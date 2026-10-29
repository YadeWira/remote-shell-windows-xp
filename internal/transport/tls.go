package transport

import (
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/BaseMax/remote-shell-windows-xp/internal/protocol"
)

var CipherSuites = []uint16{
	tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
}

func ServerTLSConfig(cert tls.Certificate) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		CipherSuites: CipherSuites,
	}
}

func ClientTLSConfig(wantFingerprint string, gotFingerprint *string) *tls.Config {
	cfg := &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
		CipherSuites:       CipherSuites,
	}
	cfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		return verifyFingerprint(rawCerts, wantFingerprint, gotFingerprint)
	}
	return cfg
}

func verifyFingerprint(rawCerts [][]byte, wantFingerprint string, gotFingerprint *string) error {
	if len(rawCerts) == 0 {
		return errors.New("transport: server presented no certificate")
	}
	sum := sha256.Sum256(rawCerts[0])
	fp := hex.EncodeToString(sum[:])
	if gotFingerprint != nil {
		*gotFingerprint = fp
	}
	if wantFingerprint == "" {
		return nil
	}
	if subtle.ConstantTimeCompare([]byte(fp), []byte(wantFingerprint)) != 1 {
		return fmt.Errorf("transport: certificate fingerprint mismatch: expected %s, got %s", wantFingerprint, fp)
	}
	return nil
}

func DialTLS(addr, wantFingerprint string, timeout time.Duration) (*Conn, string, error) {
	var gotFP string
	cfg := ClientTLSConfig(wantFingerprint, &gotFP)
	d := &net.Dialer{Timeout: timeout}
	raw, err := tls.DialWithDialer(d, "tcp", addr, cfg)
	if err != nil {
		return nil, gotFP, err
	}
	return New(raw), gotFP, nil
}

func ClientHandshake(conn *Conn, token string) (*protocol.HelloResponse, error) {
	return ClientHandshakeAs(conn, token, "")
}

// ClientHandshakeAs is ClientHandshake that also tells the agent who is connecting.
func ClientHandshakeAs(conn *Conn, token, name string) (*protocol.HelloResponse, error) {
	req := protocol.HelloRequest{Version: protocol.Version, Token: token, Name: name}
	if err := conn.SendFrame(protocol.Frame{Type: protocol.MsgHello, Payload: protocol.Marshal(req)}); err != nil {
		return nil, err
	}
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetDeadline(time.Time{})

	f, err := conn.RecvFrame()
	if err != nil {
		return nil, err
	}
	var resp protocol.HelloResponse
	if err := protocol.Unmarshal(f.Payload, &resp); err != nil {
		return nil, err
	}
	return &resp, checkHelloReply(f.Type, resp)
}

func checkHelloReply(msgType byte, resp protocol.HelloResponse) error {
	switch msgType {
	case protocol.MsgHelloOK:
		return nil
	case protocol.MsgHelloReject:
		return fmt.Errorf("transport: auth rejected: %s", resp.Message)
	default:
		return fmt.Errorf("transport: unexpected handshake message 0x%02x", msgType)
	}
}

// ServerHandshake checks the HELLO frame and answers it. It returns the (sanitized) name the
// controller announced, which may be empty.
func ServerHandshake(conn *Conn, first protocol.Frame, expectedToken, hostname string) (string, error) {
	if first.Type != protocol.MsgHello {
		conn.SendFrame(protocol.Frame{Type: protocol.MsgHelloReject, Payload: protocol.Marshal(protocol.HelloResponse{Message: "expected HELLO"})})
		return "", fmt.Errorf("transport: expected HELLO, got 0x%02x", first.Type)
	}

	var req protocol.HelloRequest
	if err := protocol.Unmarshal(first.Payload, &req); err != nil {
		conn.SendFrame(protocol.Frame{Type: protocol.MsgHelloReject, Payload: protocol.Marshal(protocol.HelloResponse{Message: "malformed hello"})})
		return "", err
	}

	if subtle.ConstantTimeCompare([]byte(req.Token), []byte(expectedToken)) != 1 {
		conn.SendFrame(protocol.Frame{Type: protocol.MsgHelloReject, Payload: protocol.Marshal(protocol.HelloResponse{Message: "invalid token"})})
		return "", errors.New("transport: invalid token")
	}

	return SanitizeName(req.Name), conn.SendFrame(protocol.Frame{Type: protocol.MsgHelloOK, Payload: protocol.Marshal(protocol.HelloResponse{OK: true, Hostname: hostname})})
}

// SanitizeName keeps a client-supplied name safe for logs: printable ASCII only, at most 64 characters.
func SanitizeName(name string) string {
	var b []byte
	for i := 0; i < len(name) && len(b) < 64; i++ {
		c := name[i]
		if c < 0x20 || c > 0x7e {
			c = '?'
		}
		b = append(b, c)
	}
	return string(b)
}
