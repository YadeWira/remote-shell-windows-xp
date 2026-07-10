//go:build !windows
// +build !windows

package procops

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/BaseMax/remote-shell-windows-xp/internal/protocol"
)

func List() ([]protocol.ProcInfo, error) {
	out, err := exec.Command("ps", "-A", "-o", "pid=,ppid=,args=").Output()
	if err != nil {
		return nil, err
	}
	return parsePS(string(out)), nil
}

func parsePS(output string) []protocol.ProcInfo {
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")

	procs := make([]protocol.ProcInfo, 0, len(lines))
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err := strconv.ParseUint(fields[0], 10, 32)
		if err != nil {
			continue
		}
		ppid, err := strconv.ParseUint(fields[1], 10, 32)
		if err != nil {
			continue
		}
		procs = append(procs, protocol.ProcInfo{
			PID:  uint32(pid),
			PPID: uint32(ppid),
			Name: strings.Join(fields[2:], " "),
		})
	}
	return procs
}

func Kill(pid uint32) error {
	return syscall.Kill(int(pid), syscall.SIGKILL)
}
