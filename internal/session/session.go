package session

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/BaseMax/remote-shell-windows-xp/internal/protocol"
)

var cdPattern = regexp.MustCompile(`(?i)^(cd|chdir)(\s+(.*))?$`)

type Manager struct {
	mu   sync.Mutex
	cwds map[uint32]string
}

func NewManager() *Manager {
	return &Manager{cwds: make(map[uint32]string)}
}

func (m *Manager) cwdFor(streamID uint32) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cwd, ok := m.cwds[streamID]; ok {
		return cwd
	}
	return defaultCwd
}

func (m *Manager) setCwd(streamID uint32, cwd string) {
	m.mu.Lock()
	m.cwds[streamID] = cwd
	m.mu.Unlock()
}

func (m *Manager) Forget(streamID uint32) {
	m.mu.Lock()
	delete(m.cwds, streamID)
	m.mu.Unlock()
}

type Result struct {
	ExitCode int
	Cwd      string
	Err      error
}

func (m *Manager) StartExec(streamID uint32, req protocol.ExecStartRequest, onStdout, onStderr func([]byte)) (stdin io.WriteCloser, kill chan<- struct{}, result <-chan Result) {
	cwd := req.Cwd
	if cwd == "" {
		cwd = m.cwdFor(streamID)
	}

	if arg, ok := parseCdCommand(req.Command); ok {
		return m.runCd(streamID, cwd, arg, onStdout, onStderr)
	}
	return m.runProcess(streamID, cwd, req, onStdout, onStderr)
}

func parseCdCommand(command string) (arg string, ok bool) {
	matches := cdPattern.FindStringSubmatch(strings.TrimSpace(command))
	if matches == nil {
		return "", false
	}
	arg = strings.TrimSpace(matches[3])
	// "cd X && prog" (or &, |, <, > outside quotes) is a compound command line, not a bare cd:
	// let the shell run it instead of taking everything after "cd " as the path.
	if hasShellOperator(arg) {
		return "", false
	}
	// cmd.exe's "cd /d X:\dir" (change drive too): the switch is not part of the path.
	if l := strings.ToLower(arg); l == "/d" {
		arg = ""
	} else if strings.HasPrefix(l, "/d ") || strings.HasPrefix(l, "/d\t") {
		arg = strings.TrimSpace(arg[2:])
	}
	return arg, true
}

// hasShellOperator reports whether s contains a command separator or redirection outside double quotes.
func hasShellOperator(s string) bool {
	inQuote := false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
		case !inQuote && strings.ContainsRune("&|<>", r):
			return true
		}
	}
	return false
}

func (m *Manager) runCd(streamID uint32, cwd, arg string, onStdout, onStderr func([]byte)) (io.WriteCloser, chan<- struct{}, <-chan Result) {
	killCh := make(chan struct{})
	resultCh := make(chan Result, 1)

	newCwd, out, err := resolveCd(cwd, arg)
	if out != "" {
		onStdout([]byte(out))
	}
	exitCode := 0
	if err != nil {
		onStderr([]byte(err.Error() + newline))
		exitCode = 1
		newCwd = cwd
	}
	m.setCwd(streamID, newCwd)
	resultCh <- Result{ExitCode: exitCode, Cwd: newCwd}
	return nil, killCh, resultCh
}

func resolveCd(cwd, arg string) (newCwd, output string, err error) {
	if arg == "" {
		return cwd, cwd + newline, nil
	}
	arg = strings.Trim(arg, `"`)

	target := arg
	if looksAbsolute(arg) {
		target = filepath.Clean(arg)
	} else {
		target = filepath.Clean(filepath.Join(cwd, arg))
	}

	info, statErr := os.Stat(target)
	if statErr != nil || !info.IsDir() {
		return cwd, "", errors.New(cdNotFoundMsg)
	}
	return target, "", nil
}

func (m *Manager) runProcess(streamID uint32, cwd string, req protocol.ExecStartRequest, onStdout, onStderr func([]byte)) (io.WriteCloser, chan<- struct{}, <-chan Result) {
	killCh := make(chan struct{})
	resultCh := make(chan Result, 1)

	cmd, stdoutPipe, stderrPipe, stdinPipe, err := buildCommand(cwd, req)
	if err != nil {
		resultCh <- Result{ExitCode: -1, Cwd: cwd, Err: err}
		return nil, killCh, resultCh
	}

	if err := cmd.Start(); err != nil {
		resultCh <- Result{ExitCode: -1, Cwd: cwd, Err: err}
		return nil, killCh, resultCh
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); streamOutput(stdoutPipe, onStdout) }()
	go func() { defer wg.Done(); streamOutput(stderrPipe, onStderr) }()

	go m.awaitCompletion(streamID, cmd, cwd, killCh, resultCh, &wg)

	return stdinPipe, killCh, resultCh
}

func buildCommand(cwd string, req protocol.ExecStartRequest) (cmd *exec.Cmd, stdout, stderr io.ReadCloser, stdin io.WriteCloser, err error) {
	cmd = shellCommand(req.Command)
	cmd.Dir = cwd

	stdout, err = cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	stderr, err = cmd.StderrPipe()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if req.Interactive {
		stdin, err = cmd.StdinPipe()
		if err != nil {
			return nil, nil, nil, nil, err
		}
	}
	return cmd, stdout, stderr, stdin, nil
}

func (m *Manager) awaitCompletion(streamID uint32, cmd *exec.Cmd, cwd string, killCh <-chan struct{}, resultCh chan<- Result, wg *sync.WaitGroup) {
	// Watch killCh from the start: the output pipes only reach EOF when the process (and
	// every child holding them) exits, so waiting for them first made kill requests and
	// client disconnects impossible to honour while the command was running.
	finished := make(chan struct{})
	var killed int32
	go func() {
		select {
		case <-killCh:
			atomic.StoreInt32(&killed, 1)
			if cmd.Process != nil {
				killTree(cmd.Process)
			}
		case <-finished:
		}
	}()

	wg.Wait()
	err := cmd.Wait()
	close(finished)
	m.setCwd(streamID, cwd)
	if atomic.LoadInt32(&killed) == 1 {
		resultCh <- Result{ExitCode: -1, Cwd: cwd, Err: errors.New("killed")}
		return
	}
	resultCh <- Result{ExitCode: exitCodeOf(err), Cwd: cwd}
}

func streamOutput(r io.Reader, onChunk func([]byte)) {
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			onChunk(chunk)
		}
		if err != nil {
			return
		}
	}
}

func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok {
			return ws.ExitStatus()
		}
	}
	return -1
}
