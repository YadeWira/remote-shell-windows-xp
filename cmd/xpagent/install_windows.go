package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
)

const taskName = "xpagent"

// install registers the agent to start at boot as SYSTEM through the Task Scheduler (schtasks ships
// with Windows XP and later), running "xpagent -supervise" so it is restarted if it dies. No Windows
// service API is used: that keeps the agent free of external dependencies and works on XP.
func install(cfg config) int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "xpagent:", err)
		return 1
	}
	if _, err := validateListenAddr(cfg.listenAddr, cfg.insecureBindAll); err != nil {
		fmt.Fprintln(os.Stderr, "xpagent:", err)
		return 2
	}
	_, port, _ := net.SplitHostPort(cfg.listenAddr)

	if err := os.MkdirAll(cfg.dataDir, 0700); err != nil {
		fmt.Fprintln(os.Stderr, "xpagent: creating data dir:", err)
		return 1
	}
	runPath, _ := childPaths(exe)
	os.Remove(runPath) // a new executable should take effect; ignored if the old agent is running from it

	tr := quoteIfNeeded(exe) + " -supervise -listen " + cfg.listenAddr + " -data " + quoteIfNeeded(cfg.dataDir)
	if cfg.insecureBindAll {
		tr += " -insecure-bind-all"
	}
	// Windows XP's schtasks has no /F for /Create: remove a previous task (errors ignored) first.
	runCmd("schtasks", "/Delete", "/TN", taskName, "/F")
	if out, err := runCmd("schtasks", "/Create", "/TN", taskName, "/TR", tr, "/SC", "ONSTART", "/RU", "SYSTEM"); err != nil {
		fmt.Fprintf(os.Stderr, "xpagent: creating the scheduled task failed (run as Administrator): %v\n%s", err, out)
		return 1
	}
	fmt.Printf("scheduled task %q created: starts at boot as SYSTEM and restarts the agent if it dies\n", taskName)

	if cfg.firewallFrom != "" {
		if err := openFirewall(port, cfg.firewallFrom); err != nil {
			fmt.Fprintln(os.Stderr, "xpagent: firewall rule failed:", err)
			return 1
		}
		fmt.Printf("firewall: TCP %s allowed from %s\n", port, cfg.firewallFrom)
	}

	if !cfg.noStart {
		if out, err := runCmd("schtasks", "/Run", "/TN", taskName); err != nil {
			fmt.Fprintf(os.Stderr, "xpagent: starting the task failed: %v\n%s", err, out)
			return 1
		}
		fmt.Println("started; if another agent already holds the port the new one retries until it is free (reboot or stop the old one)")
	}
	fmt.Printf("data dir: %s (xpagent -show-pairing -data %s prints the fingerprint and token)\n", cfg.dataDir, quoteIfNeeded(cfg.dataDir))
	return 0
}

// uninstall removes the task and the firewall rule and stops the running agent. The data directory
// (certificate, token, logs) is left alone so a later install keeps the same pairing.
func uninstall(cfg config) int {
	exe, _ := os.Executable()
	runPath, _ := childPaths(exe)
	rc := 0
	if out, err := runCmd("schtasks", "/End", "/TN", taskName); err != nil {
		fmt.Printf("task not running or not found: %s", strings.TrimSpace(out)+"\n")
	}
	if out, err := runCmd("schtasks", "/Delete", "/TN", taskName, "/F"); err != nil {
		fmt.Fprintf(os.Stderr, "xpagent: deleting the task failed: %v\n%s", err, out)
		rc = 1
	} else {
		fmt.Printf("scheduled task %q removed\n", taskName)
	}
	// Stop the agent child started from this install only (by path), not other agents on the machine.
	runCmd("wmic", "process", "where", "ExecutablePath='"+strings.Replace(runPath, `\`, `\\`, -1)+"'", "call", "terminate")
	if _, port, err := net.SplitHostPort(cfg.listenAddr); err == nil {
		removeFirewall(port)
		fmt.Println("agent stopped and firewall rule removed (data dir kept)")
	} else {
		fmt.Println("agent stopped (pass -listen <addr:port> to also remove the firewall rule; data dir kept)")
	}
	return rc
}

func openFirewall(port, from string) error {
	// Windows Vista and later: advfirewall. Windows XP/2003 only has "netsh firewall".
	if _, err := runCmd("netsh", "advfirewall", "show", "currentprofile"); err == nil {
		out, err := runCmd("netsh", "advfirewall", "firewall", "add", "rule", "name="+taskName, "dir=in", "action=allow",
			"protocol=TCP", "localport="+port, "remoteip="+from)
		if err != nil {
			return fmt.Errorf("%v: %s", err, strings.TrimSpace(out))
		}
		return nil
	}
	out, err := runCmd("netsh", "firewall", "set", "portopening", "protocol=TCP", "port="+port, "name="+taskName,
		"mode=ENABLE", "scope=CUSTOM", "addresses="+from)
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(out))
	}
	return nil
}

func removeFirewall(port string) {
	if _, err := runCmd("netsh", "advfirewall", "show", "currentprofile"); err == nil {
		runCmd("netsh", "advfirewall", "firewall", "delete", "rule", "name="+taskName)
		return
	}
	// XP: a portopening rule is deleted by protocol and port.
	runCmd("netsh", "firewall", "delete", "portopening", "protocol=TCP", "port="+port)
}

func quoteIfNeeded(s string) string {
	if strings.ContainsAny(s, " \t") {
		return `"` + s + `"`
	}
	return s
}

func runCmd(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return string(out), err
}
