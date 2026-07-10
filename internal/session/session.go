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
	return strings.TrimSpace(matches[3]), true
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
		onStderr([]byte(err.Error() + "\r\n"))
		exitCode = 1
		newCwd = cwd
	}
	m.setCwd(streamID, newCwd)
	resultCh <- Result{ExitCode: exitCode, Cwd: newCwd}
	return nil, killCh, resultCh
}

func resolveCd(cwd, arg string) (newCwd, output string, err error) {
	if arg == "" {
		return cwd, cwd + "\r\n", nil
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
		return cwd, "", errors.New("The system cannot find the path specified.")
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
	wg.Wait()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		m.setCwd(streamID, cwd)
		resultCh <- Result{ExitCode: exitCodeOf(err), Cwd: cwd}
	case <-killCh:
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		<-done
		m.setCwd(streamID, cwd)
		resultCh <- Result{ExitCode: -1, Cwd: cwd, Err: errors.New("killed")}
	}
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
