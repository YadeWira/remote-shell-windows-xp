package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

const defaultCwd = `C:\`
const newline = "\r\n"
const cdNotFoundMsg = "The system cannot find the path specified."

// shellCommand builds the cmd.exe command line by hand. Letting os/exec escape the
// argument turns every inner quote into \" which cmd.exe does not understand, so a
// command such as dir "C:\Program Files" fails. /S makes cmd strip exactly the outer
// pair of quotes added here and keep the rest of the command as typed.
func shellCommand(command string) *exec.Cmd {
	path := "cmd.exe"
	if root := os.Getenv("SystemRoot"); root != "" {
		path = filepath.Join(root, "system32", "cmd.exe")
	}
	cmd := exec.Command(path)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /S /C "` + command + `"`}
	return cmd
}

func looksAbsolute(p string) bool {
	return filepath.IsAbs(p) || (len(p) >= 2 && p[1] == ':')
}

// killTree kills the process and everything it started (taskkill ships with XP Professional).
func killTree(p *os.Process) {
	exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(p.Pid)).Run()
	p.Kill()
}
