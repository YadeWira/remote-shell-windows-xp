//go:build !windows
// +build !windows

package session

import (
	"os"
	"os/exec"
	"path/filepath"
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

func killTree(p *os.Process) {
	p.Kill()
}
