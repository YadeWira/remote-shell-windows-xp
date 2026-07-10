package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/BaseMax/remote-shell-windows-xp/internal/protocol"
)

func cmdExec(args []string) error {
	fs := flag.NewFlagSet("exec", flag.ExitOnError)
	cwd := fs.String("cwd", "", "working directory for the command")
	asJSON := fs.Bool("json", false, "print machine-readable JSON ({stdout,stderr,exit_code}) instead of streaming output")
	fs.Parse(reorderArgs(fs, args))
	rest := fs.Args()
	if len(rest) < 2 {
		return fmt.Errorf(`usage: xpctl exec <addr:port> "<command>" [--cwd DIR] [--json]`)
	}
	addr, command := rest[0], strings.Join(rest[1:], " ")

	var stdout, stderr bytes.Buffer
	exitCode := 0
	err := withClient(addr, func(c *Client) error {
		id := c.mux.NextStreamID()
		ch := c.mux.Open(id)
		defer c.mux.CloseStream(id)

		req := protocol.ExecStartRequest{Command: command, Cwd: *cwd}
		if err := c.mux.Send(protocol.Frame{Type: protocol.MsgExecStart, StreamID: id, Payload: protocol.Marshal(req)}); err != nil {
			return err
		}

		var oerr error
		stdout, stderr, exitCode, oerr = collectExecOutput(c, id, ch, !*asJSON)
		return oerr
	})
	if err != nil {
		return err
	}

	if *asJSON {
		out, _ := json.Marshal(map[string]interface{}{
			"stdout":    stdout.String(),
			"stderr":    stderr.String(),
			"exit_code": exitCode,
		})
		fmt.Println(string(out))
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return nil
}

func collectExecOutput(c *Client, id uint32, ch <-chan protocol.Frame, echo bool) (stdout, stderr bytes.Buffer, exitCode int, err error) {
	for f := range ch {
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
			return stdout, stderr, exitCode, nil
		case protocol.MsgError:
			return stdout, stderr, 0, errorFromFrame(f)
		}
	}
	return stdout, stderr, 0, nil
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
