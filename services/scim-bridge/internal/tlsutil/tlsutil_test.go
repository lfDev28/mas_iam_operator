package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func selfSigned(t *testing.T, cn string) (*x509.Certificate, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              []string{cn},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	return cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func verifies(pool *x509.CertPool, cert *x509.Certificate) bool {
	_, err := cert.Verify(x509.VerifyOptions{Roots: pool, DNSName: cert.Subject.CommonName})
	return err == nil
}

func TestRootPoolAddsExtraCAToSystemRoots(t *testing.T) {
	sysCert, _ := selfSigned(t, "public.example")
	extraCert, extraPEM := selfSigned(t, "keycloak.internal")
	orig := systemCertPool
	defer func() { systemCertPool = orig }()
	systemCertPool = func() (*x509.CertPool, error) {
		p := x509.NewCertPool()
		p.AddCert(sysCert)
		return p, nil
	}
	path := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(path, extraPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	pool, err := RootPool(path)
	if err != nil {
		t.Fatal(err)
	}
	if !verifies(pool, sysCert) {
		t.Fatalf("system roots were replaced by the extra CA")
	}
	if !verifies(pool, extraCert) {
		t.Fatalf("extra CA not trusted")
	}
}

func TestRootPoolFallsBackToExtraCAWhenSystemPoolUnavailable(t *testing.T) {
	extraCert, extraPEM := selfSigned(t, "mas.internal")
	orig := systemCertPool
	defer func() { systemCertPool = orig }()
	systemCertPool = func() (*x509.CertPool, error) { return nil, errors.New("no system roots") }
	path := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(path, extraPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	pool, err := RootPool(path)
	if err != nil || !verifies(pool, extraCert) {
		t.Fatalf("expected extra-CA-only pool, err=%v", err)
	}
}

func TestClientConfigDefaultsAndErrors(t *testing.T) {
	if cfg, err := ClientConfig("", false); err != nil || cfg != nil {
		t.Fatalf("expected nil config with no CA and verification on, got %v %v", cfg, err)
	}
	if cfg, err := ClientConfig("", true); err != nil || cfg == nil || !cfg.InsecureSkipVerify {
		t.Fatalf("expected insecure config, got %v %v", cfg, err)
	}
	if _, err := ClientConfig(filepath.Join(t.TempDir(), "missing.crt"), false); err == nil {
		t.Fatalf("expected error for missing CA file")
	}
	bad := filepath.Join(t.TempDir(), "bad.crt")
	_ = os.WriteFile(bad, []byte("not a cert"), 0o644)
	if _, err := ClientConfig(bad, false); err == nil {
		t.Fatalf("expected error for CA file without PEM certs")
	}
}
