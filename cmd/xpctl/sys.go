package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/BaseMax/remote-shell-windows-xp/internal/protocol"
)

func cmdSysinfo(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: xpctl sysinfo <addr:port>")
	}
	var res protocol.SysInfoResult
	if err := simpleRequest(args[0], protocol.MsgSysInfo, nil, &res); err != nil {
		return err
	}
	printSysInfo(res)
	return nil
}

func printSysInfo(res protocol.SysInfoResult) {
	fmt.Printf("Hostname:  %s\n", res.Hostname)
	fmt.Printf("OS:        %s (%s)\n", res.OSVersion, res.Arch)
	fmt.Printf("Uptime:    %s\n", (time.Duration(res.UptimeSec) * time.Second).String())
	fmt.Printf("Memory:    %d MB free / %d MB total\n", res.FreeMemMB, res.TotalMemMB)
	for _, d := range res.Drives {
		fmt.Printf("Drive %-3s  %d MB free / %d MB total\n", d.Letter, d.FreeMB, d.TotalMB)
	}
}

func cmdPs(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: xpctl ps <addr:port>")
	}
	var res protocol.ProcListResult
	if err := simpleRequest(args[0], protocol.MsgProcList, nil, &res); err != nil {
		return err
	}
	printProcesses(res)
	return nil
}

func printProcesses(res protocol.ProcListResult) {
	fmt.Printf("%8s %8s  %s\n", "PID", "PPID", "NAME")
	for _, p := range res.Processes {
		fmt.Printf("%8d %8d  %s\n", p.PID, p.PPID, p.Name)
	}
}

func cmdKill(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: xpctl kill <addr:port> <pid>")
	}
	pid, err := strconv.ParseUint(args[1], 10, 32)
	if err != nil {
		return fmt.Errorf("invalid pid %q: %w", args[1], err)
	}
	return simpleRequest(args[0], protocol.MsgProcKill, protocol.Marshal(protocol.ProcKillRequest{PID: uint32(pid)}), nil)
}
