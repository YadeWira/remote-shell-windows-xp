package certutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io/ioutil"
	"math/big"
	"net"
	"os"
	"time"
)

func Generate(hosts []string, validFor time.Duration) (certPEM, keyPEM []byte, err error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	template, err := buildTemplate(hosts, validFor)
	if err != nil {
		return nil, nil, err
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, err
	}

	return encodePEM(der, priv)
}

func buildTemplate(hosts []string, validFor time.Duration) (*x509.Certificate, error) {
	serial, err := newSerial()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "xpagent"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(validFor),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else if h != "" {
			template.DNSNames = append(template.DNSNames, h)
		}
	}
	return template, nil
}

func newSerial() (*big.Int, error) {
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, serialLimit)
}

func encodePEM(der []byte, priv *ecdsa.PrivateKey) (certPEM, keyPEM []byte, err error) {
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	return certPEM, keyPEM, nil
}

func LoadOrGenerate(certPath, keyPath string, hosts []string) (cert tls.Certificate, generated bool, err error) {
	if fileExists(certPath) && fileExists(keyPath) {
		cert, err = tls.LoadX509KeyPair(certPath, keyPath)
		return cert, false, err
	}

	certPEM, keyPEM, err := Generate(hosts, 20*365*24*time.Hour)
	if err != nil {
		return tls.Certificate{}, false, err
	}
	if err := ioutil.WriteFile(certPath, certPEM, 0600); err != nil {
		return tls.Certificate{}, false, err
	}
	if err := ioutil.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return tls.Certificate{}, false, err
	}
	cert, err = tls.X509KeyPair(certPEM, keyPEM)
	return cert, true, err
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func FingerprintOfCert(cert tls.Certificate) (string, error) {
	if len(cert.Certificate) == 0 {
		return "", errors.New("certutil: certificate has no DER bytes")
	}
	return FingerprintOfDER(cert.Certificate[0]), nil
}

func FingerprintOfDER(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}
