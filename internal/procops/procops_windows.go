package procops

import (
	"github.com/BaseMax/remote-shell-windows-xp/internal/protocol"
	"github.com/BaseMax/remote-shell-windows-xp/internal/winapi"
)

func List() ([]protocol.ProcInfo, error) {
	ps, err := winapi.ListProcesses()
	if err != nil {
		return nil, err
	}
	out := make([]protocol.ProcInfo, 0, len(ps))
	for _, p := range ps {
		out = append(out, protocol.ProcInfo{PID: p.PID, PPID: p.PPID, Name: p.Name})
	}
	return out, nil
}

func Kill(pid uint32) error {
	return winapi.KillProcess(pid)
}
