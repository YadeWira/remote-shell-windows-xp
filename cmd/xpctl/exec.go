package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/BaseMax/remote-shell-windows-xp/internal/protocol"
)

func cmdExec(args []string) error {
	fs := flag.NewFlagSet("exec", flag.ExitOnError)
	cwd := fs.String("cwd", "", "working directory for the command")
	timeout := fs.Duration("timeout", 0, "kill the command and exit with 124 if it runs longer than this (e.g. 30s, 5m); 0 = no limit")
	desktop := fs.Bool("desktop", false, "start the command on the agent's interactive desktop (visible windows, Windows agents only); output is not captured and the call returns at once with the pid")
	wait := fs.Bool("wait", false, "with --desktop: wait for the process to exit and use its exit code")
	asJSON := fs.Bool("json", false, "print machine-readable JSON ({stdout,stderr,exit_code}) instead of streaming output")
	fs.Parse(reorderArgs(fs, args))
	rest := fs.Args()
	if len(rest) < 2 {
		return fmt.Errorf(`usage: xpctl exec <addr:port> "<command>" [--cwd DIR] [--timeout DUR] [--desktop [--wait]] [--json]`)
	}
	addr, command := rest[0], strings.Join(rest[1:], " ")
	if *wait && !*desktop {
		return fmt.Errorf("--wait only makes sense with --desktop")
	}

	var stdout, stderr bytes.Buffer
	exitCode := 0
	err := withClient(addr, func(c *Client) error {
		id := c.mux.NextStreamID()
		ch := c.mux.Open(id)
		defer c.mux.CloseStream(id)

		req := protocol.ExecStartRequest{Command: command, Cwd: *cwd, Desktop: *desktop, Wait: *wait}
		if err := c.mux.Send(protocol.Frame{Type: protocol.MsgExecStart, StreamID: id, Payload: protocol.Marshal(req)}); err != nil {
			return err
		}

		var oerr error
		stdout, stderr, exitCode, oerr = collectExecOutput(c, id, ch, !*asJSON, *timeout)
		return oerr
	})
	if err != nil {
		return err
	}

	if *asJSON {
		res := map[string]interface{}{
			"stdout":    stdout.String(),
			"stderr":    stderr.String(),
			"exit_code": exitCode,
		}
		// JSON strings must be UTF-8; cmd.exe writes the OEM code page, which json.Marshal would
		// silently replace with U+FFFD. Keep the exact bytes next to the (lossy) text.
		if !utf8.Valid(stdout.Bytes()) {
			res["stdout_b64"] = base64.StdEncoding.EncodeToString(stdout.Bytes())
			res["stdout_note"] = "stdout is not valid UTF-8 (probably the remote OEM code page); stdout_b64 has the exact bytes"
		}
		if !utf8.Valid(stderr.Bytes()) {
			res["stderr_b64"] = base64.StdEncoding.EncodeToString(stderr.Bytes())
			res["stderr_note"] = "stderr is not valid UTF-8 (probably the remote OEM code page); stderr_b64 has the exact bytes"
		}
		out, _ := json.Marshal(res)
		fmt.Println(string(out))
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return nil
}

func collectExecOutput(c *Client, id uint32, ch <-chan protocol.Frame, echo bool, timeout time.Duration) (stdout, stderr bytes.Buffer, exitCode int, err error) {
	var expired <-chan time.Time
	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		expired = timer.C
	}
	killed := false
	timedOut := func() (bytes.Buffer, bytes.Buffer, int, error) {
		stderr.WriteString(fmt.Sprintf("xpctl: timed out after %s, command killed\n", timeout))
		return stdout, stderr, 124, nil
	}
	for {
		var f protocol.Frame
		select {
		case nf, open := <-ch:
			if !open {
				return stdout, stderr, 0, nil
			}
			f = nf
		case <-expired:
			if killed {
				return timedOut() // the agent did not confirm the kill in time
			}
			// Ask the agent to kill the process tree, then wait a little for it to report the exit.
			killed = true
			c.mux.Send(protocol.Frame{Type: protocol.MsgExecKill, StreamID: id})
			expired = time.After(10 * time.Second)
			continue
		}
		switch f.Type {
		case protocol.MsgExecStdout:
			stdout.Write(f.Payload)
			if echo {
				os.Stdout.Write(f.Payload)
			}
		case protocol.MsgExecStderr:
			stderr.Write(f.Payload)
			if echo {
				os.Stderr.Write(f.Payload)
			}
		case protocol.MsgExecExit:
			var res protocol.ExecExitResult
			protocol.Unmarshal(f.Payload, &res)
			exitCode = res.ExitCode
			c.mux.Send(protocol.Frame{Type: protocol.MsgStreamEnd, StreamID: id})
			if killed {
				return timedOut()
			}
			return stdout, stderr, exitCode, nil
		case protocol.MsgError:
			return stdout, stderr, 0, errorFromFrame(f)
		}
	}
}

func cmdShell(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: xpctl shell <addr:port>")
	}
	addr := args[0]

	return withClient(addr, func(c *Client) error {
		id := c.mux.NextStreamID()
		ch := c.mux.Open(id)
		defer func() {
			c.mux.Send(protocol.Frame{Type: protocol.MsgStreamEnd, StreamID: id})
			c.mux.CloseStream(id)
		}()

		fmt.Printf("Connected to %s. Type 'exit' to quit.\n", addr)
		cwd := ""
		scanner := bufio.NewScanner(os.Stdin)
		for {
			fmt.Printf("%s> ", cwd)
			if !scanner.Scan() {
				return nil
			}
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			if line == "exit" || line == "quit" {
				return nil
			}

			newCwd, err := runShellCommand(c, id, ch, line)
			if err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				continue
			}
			cwd = newCwd
		}
	})
}

func runShellCommand(c *Client, id uint32, ch <-chan protocol.Frame, line string) (string, error) {
	req := protocol.ExecStartRequest{Command: line}
	if err := c.mux.Send(protocol.Frame{Type: protocol.MsgExecStart, StreamID: id, Payload: protocol.Marshal(req)}); err != nil {
		return "", err
	}

	for f := range ch {
		switch f.Type {
		case protocol.MsgExecStdout:
			os.Stdout.Write(f.Payload)
		case protocol.MsgExecStderr:
			os.Stderr.Write(f.Payload)
		case protocol.MsgExecExit:
			var res protocol.ExecExitResult
			protocol.Unmarshal(f.Payload, &res)
			return res.Cwd, nil
		case protocol.MsgError:
			return "", errorFromFrame(f)
		}
	}
	return "", errors.New("connection closed")
}
