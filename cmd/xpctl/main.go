package main

import (
	"fmt"
	"os"
)

var commands = map[string]func([]string) error{
	"pair":    cmdPair,
	"exec":    cmdExec,
	"shell":   cmdShell,
	"ls":      cmdLs,
	"stat":    cmdStat,
	"mkdir":   cmdMkdir,
	"rm":      cmdRm,
	"mv":      cmdMv,
	"cp":      cmdCp,
	"cat":     cmdCat,
	"tree":    cmdTree,
	"get":     cmdGet,
	"put":     cmdPut,
	"ps":      cmdPs,
	"kill":    cmdKill,
	"sysinfo": cmdSysinfo,
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}

	name := os.Args[1]
	args := os.Args[2:]

	if name == "-h" || name == "--help" || name == "help" {
		printUsage()
		return
	}

	cmd, ok := commands[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "xpctl: unknown command %q\n\n", name)
		printUsage()
		os.Exit(2)
	}

	if err := cmd(args); err != nil {
		fmt.Fprintf(os.Stderr, "xpctl: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `xpctl - control an xpagent instance on a remote machine

Usage:
  xpctl pair <addr:port> --token <token> [--yes]
  xpctl exec <addr:port> "<command>" [--cwd DIR] [--timeout DUR] [--desktop [--wait]] [--json]
  xpctl shell <addr:port>
  xpctl ls <addr:port> <path>
  xpctl tree <addr:port> <path> [--depth N] [--json]
  xpctl stat <addr:port> <path>
  xpctl cat <addr:port> <path> [<path>...]
  xpctl mkdir <addr:port> <path>
  xpctl rm <addr:port> <path> [--recursive]
  xpctl mv <addr:port> <src> <dst>
  xpctl cp <addr:port> <src> <dst>
  xpctl get <addr:port> <remote-path> <local-path>
  xpctl put <addr:port> <local-path> <remote-path> [--overwrite] [--resume]
  xpctl ps <addr:port>
  xpctl kill <addr:port> <pid>
  xpctl sysinfo <addr:port>

First-time setup: start xpagent on the XP guest, then run the
`+"`xpctl pair`"+` command it prints (bind address + token). Pairing records the
agent's certificate fingerprint locally (like SSH's known_hosts) so future
connections are authenticated without a CA.`)
}
