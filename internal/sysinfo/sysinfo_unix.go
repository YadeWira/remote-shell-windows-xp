// +build !windows

package sysinfo

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/BaseMax/remote-shell-windows-xp/internal/protocol"
)

func Collect() (protocol.SysInfoResult, error) {
	host, _ := os.Hostname()
	totalMem, freeMem := memoryInfoMB()

	return protocol.SysInfoResult{
		Hostname:   host,
		OSVersion:  unameVersion(),
		Arch:       runtime.GOARCH,
		UptimeSec:  uptimeSeconds(),
		TotalMemMB: totalMem,
		FreeMemMB:  freeMem,
		Drives:     rootDrive(),
	}, nil
}

func unameVersion() string {
	out, err := exec.Command("uname", "-srm").Output()
	if err != nil {
		return runtime.GOOS
	}
	return strings.TrimSpace(string(out))
}

func uptimeSeconds() int64 {
	f, err := os.Open("/proc/uptime")
	if err != nil {
		return 0
	}
	defer f.Close()

	var seconds float64
	if _, err := fmt.Fscanf(f, "%f", &seconds); err != nil {
		return 0
	}
	return int64(seconds)
}

func memoryInfoMB() (totalMB, freeMB int64) {
	if total, free, ok := memoryFromProc(); ok {
		return total, free
	}
	return memoryFromSysctl(), 0
}

func memoryFromProc() (totalMB, freeMB int64, ok bool) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()

	values := map[string]int64{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		n, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		values[strings.TrimSuffix(fields[0], ":")] = n
	}

	total, hasTotal := values["MemTotal"]
	if !hasTotal {
		return 0, 0, false
	}
	avail, hasAvail := values["MemAvailable"]
	if !hasAvail {
		avail = values["MemFree"]
	}
	return total / 1024, avail / 1024, true
}

func memoryFromSysctl() int64 {
	for _, key := range []string{"hw.memsize", "hw.physmem64", "hw.physmem"} {
		out, err := exec.Command("sysctl", "-n", key).Output()
		if err != nil {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
		if err != nil {
			continue
		}
		return n / 1024 / 1024
	}
	return 0
}

func rootDrive() []protocol.DriveInfo {
	out, err := exec.Command("df", "-Pk", "/").Output()
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) < 2 {
		return nil
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 4 {
		return nil
	}
	totalKB, err1 := strconv.ParseInt(fields[1], 10, 64)
	availKB, err2 := strconv.ParseInt(fields[3], 10, 64)
	if err1 != nil || err2 != nil {
		return nil
	}
	return []protocol.DriveInfo{{Letter: "/", TotalMB: totalKB / 1024, FreeMB: availKB / 1024}}
}
