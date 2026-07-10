// +build !windows

package session

import (
	"os/exec"
	"path/filepath"
)

const defaultCwd = "/"

func shellCommand(command string) *exec.Cmd {
	return exec.Command("/bin/sh", "-c", command)
}

func looksAbsolute(p string) bool {
	return filepath.IsAbs(p)
}
