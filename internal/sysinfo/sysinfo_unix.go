//go:build !windows
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
	"time"

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
	if seconds, ok := uptimeFromProc(); ok {
		return seconds
	}
	return uptimeFromBoottime()
}

func uptimeFromProc() (int64, bool) {
	f, err := os.Open("/proc/uptime")
	if err != nil {
		return 0, false
	}
	defer f.Close()

	var seconds float64
	if _, err := fmt.Fscanf(f, "%f", &seconds); err != nil {
		return 0, false
	}
	return int64(seconds), true
}

func uptimeFromBoottime() int64 {
	out, err := exec.Command("sysctl", "-n", "kern.boottime").Output()
	if err != nil {
		return 0
	}
	boot, ok := parseBoottimeSeconds(string(out))
	if !ok || boot <= 0 {
		return 0
	}
	uptime := time.Now().Unix() - boot
	if uptime < 0 {
		return 0
	}
	return uptime
}

func parseBoottimeSeconds(s string) (int64, bool) {
	idx := strings.Index(s, "sec")
	if idx < 0 {
		return 0, false
	}
	rest := s[idx:]
	eq := strings.IndexByte(rest, '=')
	if eq < 0 {
		return 0, false
	}
	rest = strings.TrimSpace(rest[eq+1:])

	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	n, err := strconv.ParseInt(rest[:end], 10, 64)
	return n, err == nil
}

func memoryInfoMB() (totalMB, freeMB int64) {
	if total, free, ok := memoryFromProc(); ok {
		return total, free
	}

	total := memoryFromSysctl()
	if free, ok := freeMemoryFromVMStat(); ok {
		return total, free
	}
	if free, ok := freeMemoryFromFreeBSDSysctl(); ok {
		return total, free
	}
	return total, 0
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

func freeMemoryFromVMStat() (freeMB int64, ok bool) {
	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0, false
	}
	lines := strings.Split(string(out), "\n")

	pageSize := int64(4096)
	if len(lines) > 0 {
		if idx := strings.Index(lines[0], "page size of "); idx >= 0 {
			rest := lines[0][idx+len("page size of "):]
			if n, ok := leadingInt(rest); ok {
				pageSize = n
			}
		}
	}

	var freePages, inactivePages int64
	var sawFree, sawInactive bool
	for _, line := range lines {
		if n, ok := vmStatValue(line, "Pages free"); ok {
			freePages, sawFree = n, true
		}
		if n, ok := vmStatValue(line, "Pages inactive"); ok {
			inactivePages, sawInactive = n, true
		}
	}
	if !sawFree && !sawInactive {
		return 0, false
	}
	return (freePages + inactivePages) * pageSize / 1024 / 1024, true
}

func vmStatValue(line, prefix string) (int64, bool) {
	if !strings.HasPrefix(line, prefix) {
		return 0, false
	}
	rest := strings.TrimPrefix(line, prefix)
	rest = strings.TrimPrefix(strings.TrimSpace(rest), ":")
	rest = strings.TrimSuffix(strings.TrimSpace(rest), ".")
	n, err := strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func leadingInt(s string) (int64, bool) {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	n, err := strconv.ParseInt(s[:end], 10, 64)
	return n, err == nil
}

func freeMemoryFromFreeBSDSysctl() (freeMB int64, ok bool) {
	out, err := exec.Command("sysctl", "-n", "vm.stats.vm.v_free_count").Output()
	if err != nil {
		return 0, false
	}
	freePages, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0, false
	}

	pageSize := int64(4096)
	if psOut, err := exec.Command("sysctl", "-n", "hw.pagesize").Output(); err == nil {
		if n, err := strconv.ParseInt(strings.TrimSpace(string(psOut)), 10, 64); err == nil && n > 0 {
			pageSize = n
		}
	}
	return freePages * pageSize / 1024 / 1024, true
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
