package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/BaseMax/remote-shell-windows-xp/internal/transport"
	"github.com/BaseMax/remote-shell-windows-xp/internal/trust"
)

func cmdPair(args []string) error {
	fs := flag.NewFlagSet("pair", flag.ExitOnError)
	token := fs.String("token", "", "auth token printed by xpagent on first run (required)")
	yes := fs.Bool("yes", false, "skip the interactive fingerprint confirmation prompt")
	fs.Parse(reorderArgs(fs, args))
	rest := fs.Args()
	if len(rest) < 1 || *token == "" {
		return fmt.Errorf("usage: xpctl pair <addr:port> --token <token> [--yes]")
	}
	addr := rest[0]

	conn, fingerprint, err := transport.DialTLS(addr, "", 10*time.Second)
	if err != nil {
		return fmt.Errorf("connecting to %s: %w", addr, err)
	}
	defer conn.Close()

	resp, err := transport.ClientHandshake(conn, *token)
	if err != nil {
		return fmt.Errorf("authenticating: %w", err)
	}

	fmt.Printf("Agent at %s (hostname %q) presented certificate fingerprint:\n  %s\n", addr, resp.Hostname, fingerprint)
	if !*yes && !confirmTrust() {
		return errors.New("pairing aborted")
	}

	return savePairing(addr, fingerprint, *token)
}

func confirmTrust() bool {
	fmt.Print("Trust this fingerprint and save it for future connections? [y/N] ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	return strings.ToLower(strings.TrimSpace(line)) == "y"
}

func savePairing(addr, fingerprint, token string) error {
	path, err := trust.DefaultPath()
	if err != nil {
		return err
	}
	store, err := trust.Load(path)
	if err != nil {
		return err
	}
	store.Set(trust.Entry{Addr: addr, Fingerprint: fingerprint, Token: token})
	if err := store.Save(); err != nil {
		return err
	}

	fmt.Printf("Paired with %s. Trust info saved to %s\n", addr, path)
	return nil
}
