//go:build !windows
// +build !windows

package session

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/BaseMax/remote-shell-windows-xp/internal/protocol"
)

const defaultCwd = "/"
const newline = "\n"
const cdNotFoundMsg = "No such file or directory"

func shellCommand(command string) *exec.Cmd {
	return exec.Command("/bin/sh", "-c", command)
}

func looksAbsolute(p string) bool {
	return filepath.IsAbs(p)
}

// procGroup is only meaningful on Windows (Job Objects); here a kill reaches the shell only.
type procGroup struct{}

func prepareGroup(cmd *exec.Cmd) {}

func attachGroup(p *os.Process) *procGroup { return &procGroup{} }

func (g *procGroup) kill(p *os.Process) { p.Kill() }

func (g *procGroup) release() {}

func (m *Manager) runDesktop(streamID uint32, cwd string, req protocol.ExecStartRequest, onStdout func([]byte)) (io.WriteCloser, chan<- struct{}, <-chan Result) {
	resultCh := make(chan Result, 1)
	resultCh <- Result{ExitCode: -1, Cwd: cwd, Err: errors.New("desktop mode is only supported on Windows agents")}
	return nil, make(chan struct{}), resultCh
}
