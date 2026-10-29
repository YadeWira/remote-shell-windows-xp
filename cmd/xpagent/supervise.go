package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// supervise keeps an agent running: it starts a child agent, restarts it when it exits and swaps in
// a newer build when one is waiting. It is what the scheduled task created by -install runs.
//
// The child runs from a copy ("<name>.run.exe") of the executable, so the file the supervisor itself
// is running from is never replaced. To update the agent from a controller: put the new build next
// to the executable as "<name>.exe.new" (xpctl put) and kill the agent process (xpctl kill <pid>);
// the supervisor moves the file over the copy and starts it. The supervisor process itself keeps
// the version it was installed with, which is fine: it only restarts the child.
func supervise(cfg config) int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "xpagent:", err)
		return 1
	}
	runPath, newPath := childPaths(exe)

	if err := os.MkdirAll(cfg.dataDir, 0700); err != nil {
		fmt.Fprintln(os.Stderr, "xpagent: creating data dir:", err)
		return 1
	}
	logFile, err := os.OpenFile(filepath.Join(cfg.dataDir, "agent.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xpagent: opening agent.log:", err)
		return 1
	}
	defer logFile.Close()
	logger := log.New(io.MultiWriter(logFile, os.Stderr), "", log.LstdFlags)

	args := []string{"-listen", cfg.listenAddr, "-data", cfg.dataDir}
	if cfg.insecureBindAll {
		args = append(args, "-insecure-bind-all")
	}

	delay := 5 * time.Second
	for {
		if err := prepareChild(exe, runPath, newPath, logger); err != nil {
			logger.Printf("xpagent supervisor: %v", err)
		} else {
			start := time.Now()
			cmd := exec.Command(runPath, args...)
			cmd.Stdout = logFile
			cmd.Stderr = logFile
			logger.Printf("xpagent supervisor: starting %s", runPath)
			err := cmd.Run()
			logger.Printf("xpagent supervisor: agent exited: %v", err)
			if time.Since(start) > 30*time.Second {
				delay = 5 * time.Second
			}
		}
		time.Sleep(delay)
		// Back off when the agent keeps dying right away (port taken, bad address...).
		if delay < time.Minute {
			delay *= 2
		}
	}
}

// childPaths returns the copy the child runs from and the file a pending update is put in.
func childPaths(exe string) (runPath, newPath string) {
	ext := filepath.Ext(exe)
	base := strings.TrimSuffix(exe, ext)
	return base + ".run" + ext, exe + ".new"
}

// prepareChild makes sure the child binary exists: a pending ".new" replaces it, otherwise it is
// copied from the executable once. A copy that exists is kept, so an update survives a reboot.
func prepareChild(exe, runPath, newPath string, logger *log.Logger) error {
	if _, err := os.Stat(newPath); err == nil {
		os.Remove(runPath)
		if err := os.Rename(newPath, runPath); err != nil {
			return fmt.Errorf("applying update %s: %v", newPath, err)
		}
		logger.Printf("xpagent supervisor: applied update from %s", newPath)
		return nil
	}
	if _, err := os.Stat(runPath); err == nil {
		return nil
	}
	return copyFile(exe, runPath)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
