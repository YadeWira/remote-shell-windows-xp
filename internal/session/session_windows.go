package session

import (
	"os/exec"
	"path/filepath"
)

const defaultCwd = `C:\`

func shellCommand(command string) *exec.Cmd {
	return exec.Command("cmd.exe", "/C", command)
}

func looksAbsolute(p string) bool {
	return filepath.IsAbs(p) || (len(p) >= 2 && p[1] == ':')
}
