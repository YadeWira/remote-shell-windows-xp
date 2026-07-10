package sysinfo

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/BaseMax/remote-shell-windows-xp/internal/protocol"
	"github.com/BaseMax/remote-shell-windows-xp/internal/winapi"
)

func Collect() (protocol.SysInfoResult, error) {
	host, _ := os.Hostname()
	totalMem, freeMem, _ := winapi.MemoryStatusMB()

	return protocol.SysInfoResult{
		Hostname:   host,
		OSVersion:  collectOSVersion(),
		Arch:       runtime.GOARCH,
		UptimeSec:  winapi.UptimeSeconds(),
		TotalMemMB: totalMem,
		FreeMemMB:  freeMem,
		Drives:     collectDrives(),
	}, nil
}

func collectOSVersion() string {
	major, minor, build, sp, err := winapi.OSVersion()
	if err != nil {
		return "unknown"
	}
	return fmt.Sprintf("Windows NT %d.%d build %d %s", major, minor, build, strings.TrimSpace(sp))
}

func collectDrives() []protocol.DriveInfo {
	var drives []protocol.DriveInfo
	for _, letter := range winapi.LogicalDriveLetters() {
		total, free, err := winapi.DiskFreeSpaceMB(letter)
		if err != nil {
			continue
		}
		drives = append(drives, protocol.DriveInfo{Letter: letter, TotalMB: total, FreeMB: free})
	}
	return drives
}
