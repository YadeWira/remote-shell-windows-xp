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
	"github.com/BaseMax/remote-shell-windows-xp/internal/fileacl"
	"github.com/BaseMax/remote-shell-windows-xp/internal/transport"
)

type agent struct {
	token    string
	hostname string
	audit    *auditlog.Logger

	maxConns   int32
	maxPreAuth int32
	maxStreams int32
	conns      int32 // open connections, authenticated or not
	preAuth    int32 // connections that have not authenticated yet
	lastReject int64 // unix time of the last "limit reached" log line, to avoid flooding the log
}

type config struct {
	listenAddr      string
	dataDir         string
	insecureBindAll bool
	showPairing     bool
	install         bool
	uninstall       bool
	supervise       bool
	firewallFrom    string
	noStart         bool
	maxConns        int
	maxPreAuth      int
	maxStreams      int
	auditFull       bool
	auditMaxMB      int
}

func main() {
	cfg := parseFlags()
	switch {
	case cfg.showPairing:
		os.Exit(showPairing(cfg.dataDir))
	case cfg.install:
		os.Exit(install(cfg))
	case cfg.uninstall:
		os.Exit(uninstall(cfg))
	case cfg.supervise:
		os.Exit(supervise(cfg))
	}

	host, err := validateListenAddr(cfg.listenAddr, cfg.insecureBindAll)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xpagent:", err)
		os.Exit(2)
	}

	if err := os.MkdirAll(cfg.dataDir, 0700); err != nil {
		log.Fatalf("xpagent: creating data dir: %v", err)
	}

	if err := fileacl.RestrictDir(cfg.dataDir); err != nil {
		log.Printf("xpagent: warning: could not restrict the data dir permissions: %v", err)
	}
	log.Printf("xpagent: data dir %s", cfg.dataDir)
	a, cert, fp, err := bootstrap(cfg, host)
	if err != nil {
		log.Fatalf("xpagent: %v", err)
	}
	defer a.audit.Close()

	serve(cfg.listenAddr, cert, fp, a)
}

func parseFlags() config {
	listenAddr := flag.String("listen", "", "address:port to listen on, e.g. 192.168.56.1:5959 (required)")
	dataDir := flag.String("data", "", "directory for cert/key/token/audit log (default: xpagent-data next to the executable)")
	showPair := flag.Bool("show-pairing", false, "print the certificate fingerprint and token stored in -data, then exit")
	install := flag.Bool("install", false, "Windows: register the agent as a boot-time SYSTEM scheduled task (with -listen, optional -data, -firewall-from), then start it")
	uninstall := flag.Bool("uninstall", false, "Windows: remove the scheduled task, stop the agent and remove the firewall rule (pass -listen to name the port); the data dir is kept")
	supervise := flag.Bool("supervise", false, "run the agent and restart it when it exits; applies a pending <exe>.new update (what the installed task runs)")
	firewallFrom := flag.String("firewall-from", "", "with -install: also open the firewall for the agent's port from these addresses, e.g. 172.17.0.0/16,172.30.0.0/24")
	noStart := flag.Bool("no-start", false, "with -install: create the task but do not start it now")
	maxConns := flag.Int("max-conns", 32, "maximum simultaneous connections (0 = unlimited)")
	maxPreAuth := flag.Int("max-preauth", 8, "maximum connections that have not authenticated yet (0 = unlimited)")
	maxStreams := flag.Int("max-streams", 64, "maximum concurrent requests per connection (0 = unlimited)")
	auditFull := flag.Bool("audit-full", false, "log exec command lines as typed; by default password-like values are hidden")
	auditMaxMB := flag.Int("audit-max-mb", 10, "rotate audit.log when it exceeds this many MB, keeping 3 old files (0 = never rotate)")
	insecureBindAll := flag.Bool("insecure-bind-all", false, "allow binding to 0.0.0.0 or [::] (dangerous: exposes the agent beyond the VM host-only network)")
	flag.Parse()

	if *dataDir == "" {
		*dataDir = defaultDataDir()
	}
	if *listenAddr == "" && !*showPair && !*uninstall {
		fmt.Fprintln(os.Stderr, "xpagent: -listen is required, e.g. -listen 192.168.56.1:5959")
		os.Exit(2)
	}
	return config{listenAddr: *listenAddr, dataDir: *dataDir, insecureBindAll: *insecureBindAll, showPairing: *showPair,
		install: *install, uninstall: *uninstall, supervise: *supervise, firewallFrom: *firewallFrom, noStart: *noStart,
		maxConns: *maxConns, maxPreAuth: *maxPreAuth, maxStreams: *maxStreams, auditFull: *auditFull, auditMaxMB: *auditMaxMB}
}

// defaultDataDir is next to the executable, not relative to the working directory: an agent
// started from the Startup folder, a scheduled task or a service has an unpredictable cwd and
// would silently generate a new certificate and token (breaking every paired controller).
func defaultDataDir() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), "xpagent-data")
	}
	return "xpagent-data"
}

func showPairing(dataDir string) int {
	cert, err := tls.LoadX509KeyPair(filepath.Join(dataDir, "agent.crt"), filepath.Join(dataDir, "agent.key"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "xpagent: no certificate in", dataDir, "(run the agent once first):", err)
		return 1
	}
	fp, err := certutil.FingerprintOfCert(cert)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xpagent:", err)
		return 1
	}
	token, err := ioutil.ReadFile(filepath.Join(dataDir, "token.txt"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "xpagent: no token in", dataDir, ":", err)
		return 1
	}
	fmt.Printf("data dir:    %s\nfingerprint: %s\ntoken:       %s\n", dataDir, fp, strings.TrimSpace(string(token)))
	fmt.Printf("pair with:   xpctl pair <addr:port> --fingerprint %s --token %s\n", fp, strings.TrimSpace(string(token)))
	return 0
}

func validateListenAddr(listenAddr string, insecureBindAll bool) (host string, err error) {
	host, _, err = net.SplitHostPort(listenAddr)
	if err != nil {
		return "", fmt.Errorf("invalid -listen address %q: %v", listenAddr, err)
	}
	if !insecureBindAll && (host == "" || host == "0.0.0.0" || host == "::") {
		return "", errors.New("refusing to bind to all interfaces by default; pass the VirtualBox host-only adapter IP explicitly (e.g. -listen 192.168.56.101:5959), or pass -insecure-bind-all to override")
	}
	return host, nil
}

func bootstrap(cfg config, host string) (*agent, tls.Certificate, string, error) {
	dataDir, listenAddr := cfg.dataDir, cfg.listenAddr
	certPath := filepath.Join(dataDir, "agent.crt")
	keyPath := filepath.Join(dataDir, "agent.key")
	tokenPath := filepath.Join(dataDir, "token.txt")
	auditPath := filepath.Join(dataDir, "audit.log")

	cert, certGenerated, err := certutil.LoadOrGenerate(certPath, keyPath, []string{host})
	if err != nil {
		return nil, tls.Certificate{}, "", fmt.Errorf("certificate: %v", err)
	}

	token, tokenGenerated, err := loadOrGenerateToken(tokenPath)
	if err != nil {
		return nil, tls.Certificate{}, "", fmt.Errorf("token: %v", err)
	}

	fp, err := certutil.FingerprintOfCert(cert)
	if err != nil {
		return nil, tls.Certificate{}, "", fmt.Errorf("fingerprint: %v", err)
	}

	audit, err := auditlog.OpenWith(auditPath, auditlog.Options{
		MaxSize: int64(cfg.auditMaxMB) << 20,
		Redact:  !cfg.auditFull,
		OnError: func(err error) { log.Printf("xpagent: audit log: %v", err) },
	})
	if err != nil {
		return nil, tls.Certificate{}, "", fmt.Errorf("audit log: %v", err)
	}

	// 0600 is ignored on Windows: make sure the secrets are not readable by other local users.
	for _, p := range []string{certPath, keyPath, tokenPath, auditPath} {
		if err := fileacl.RestrictFile(p); err != nil {
			log.Printf("xpagent: warning: could not restrict permissions on %s: %v", p, err)
		}
	}

	hostname, _ := os.Hostname()
	a := &agent{token: token, hostname: hostname, audit: audit,
		maxConns: int32(cfg.maxConns), maxPreAuth: int32(cfg.maxPreAuth), maxStreams: int32(cfg.maxStreams)}

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
