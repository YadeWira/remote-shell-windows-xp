//go:build !windows
// +build !windows

package main

import (
	"fmt"
	"os"
)

func install(cfg config) int {
	fmt.Fprintln(os.Stderr, "xpagent: -install is only available on Windows; on Linux/BSD use a systemd unit or your init system, running: xpagent -supervise -listen <addr:port> -data <dir>")
	return 2
}

func uninstall(cfg config) int {
	fmt.Fprintln(os.Stderr, "xpagent: -uninstall is only available on Windows")
	return 2
}
