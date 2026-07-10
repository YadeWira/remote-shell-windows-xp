package main

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/BaseMax/remote-shell-windows-xp/internal/auditlog"
	"github.com/BaseMax/remote-shell-windows-xp/internal/certutil"
	"github.com/BaseMax/remote-shell-windows-xp/internal/transport"
)

type agent struct {
	token    string
	hostname string
	audit    *auditlog.Logger
}

type config struct {
	listenAddr      string
	dataDir         string
	insecureBindAll bool
}

func main() {
	cfg := parseFlags()

	host, err := validateListenAddr(cfg.listenAddr, cfg.insecureBindAll)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xpagent:", err)
		os.Exit(2)
	}

	if err := os.MkdirAll(cfg.dataDir, 0700); err != nil {
		log.Fatalf("xpagent: creating data dir: %v", err)
	}

	a, cert, fp, err := bootstrap(cfg.dataDir, host, cfg.listenAddr)
	if err != nil {
		log.Fatalf("xpagent: %v", err)
	}
	defer a.audit.Close()

	serve(cfg.listenAddr, cert, fp, a)
}

func parseFlags() config {
	listenAddr := flag.String("listen", "", "address:port to listen on, e.g. 192.168.56.1:5959 (required)")
	dataDir := flag.String("data", "xpagent-data", "directory for cert/key/token/audit log")
	insecureBindAll := flag.Bool("insecure-bind-all", false, "allow binding to 0.0.0.0 or [::] (dangerous: exposes the agent beyond the VM host-only network)")
	flag.Parse()

	if *listenAddr == "" {
		fmt.Fprintln(os.Stderr, "xpagent: -listen is required, e.g. -listen 192.168.56.1:5959")
		os.Exit(2)
	}
	return config{listenAddr: *listenAddr, dataDir: *dataDir, insecureBindAll: *insecureBindAll}
}

func validateListenAddr(listenAddr string, insecureBindAll bool) (host string, err error) {
	host, _, err = net.SplitHostPort(listenAddr)
	if err != nil {
		return "", fmt.Errorf("invalid -listen address %q: %w", listenAddr, err)
	}
	if !insecureBindAll && (host == "" || host == "0.0.0.0" || host == "::") {
		return "", errors.New("refusing to bind to all interfaces by default; pass the VirtualBox host-only adapter IP explicitly (e.g. -listen 192.168.56.101:5959), or pass -insecure-bind-all to override")
	}
	return host, nil
}

func bootstrap(dataDir, host, listenAddr string) (*agent, tls.Certificate, string, error) {
	certPath := filepath.Join(dataDir, "agent.crt")
	keyPath := filepath.Join(dataDir, "agent.key")
	tokenPath := filepath.Join(dataDir, "token.txt")
	auditPath := filepath.Join(dataDir, "audit.log")

	cert, certGenerated, err := certutil.LoadOrGenerate(certPath, keyPath, []string{host})
	if err != nil {
		return nil, tls.Certificate{}, "", fmt.Errorf("certificate: %w", err)
	}

	token, tokenGenerated, err := loadOrGenerateToken(tokenPath)
	if err != nil {
		return nil, tls.Certificate{}, "", fmt.Errorf("token: %w", err)
	}

	fp, err := certutil.FingerprintOfCert(cert)
	if err != nil {
		return nil, tls.Certificate{}, "", fmt.Errorf("fingerprint: %w", err)
	}

	audit, err := auditlog.Open(auditPath)
	if err != nil {
		return nil, tls.Certificate{}, "", fmt.Errorf("audit log: %w", err)
	}

	hostname, _ := os.Hostname()
	a := &agent{token: token, hostname: hostname, audit: audit}

	if certGenerated || tokenGenerated {
		printFirstRunBanner(fp, token, listenAddr)
	}

	return a, cert, fp, nil
}

func printFirstRunBanner(fingerprint, token, listenAddr string) {
	fmt.Println("==================================================================")
	fmt.Println(" xpagent: first-run setup complete")
	fmt.Println("==================================================================")
	fmt.Printf(" Certificate fingerprint (sha256): %s\n", fingerprint)
	fmt.Printf(" Auth token:                       %s\n", token)
	fmt.Println()
	fmt.Println(" On the controller machine, run:")
	fmt.Printf("   xpctl pair %s --token %s\n", listenAddr, token)
	fmt.Println("==================================================================")
}

func loadOrGenerateToken(path string) (token string, generated bool, err error) {
	if b, rerr := ioutil.ReadFile(path); rerr == nil {
		return strings.TrimSpace(string(b)), false, nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", false, err
	}
	token = hex.EncodeToString(buf)
	if err := ioutil.WriteFile(path, []byte(token), 0600); err != nil {
		return "", false, err
	}
	return token, true, nil
}

func serve(listenAddr string, cert tls.Certificate, fp string, a *agent) {
	tlsCfg := transport.ServerTLSConfig(cert)
	ln, err := tls.Listen("tcp", listenAddr, tlsCfg)
	if err != nil {
		log.Fatalf("xpagent: listen: %v", err)
	}
	log.Printf("xpagent: listening on %s (fingerprint %s)", listenAddr, fp)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("xpagent: accept: %v", err)
			continue
		}
		go a.handleConn(conn)
	}
}
