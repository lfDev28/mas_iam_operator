// Package tlsutil builds the TLS configuration shared by the bridge's HTTP
// clients.
package tlsutil

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// systemCertPool is swapped in tests.
var systemCertPool = x509.SystemCertPool

// RootPool returns the system trust store plus the PEM certificates in
// extraCAFile (when set). An extra CA adds to the system roots rather than
// replacing them, so a private Keycloak/MAS CA does not break calls to
// publicly-signed endpoints. Only if the system pool cannot be loaded does
// the pool fall back to the extra CA alone. Returns nil (use Go's default)
// when there is nothing extra to add.
func RootPool(extraCAFile string) (*x509.CertPool, error) {
	if extraCAFile == "" {
		return nil, nil
	}
	data, err := os.ReadFile(extraCAFile)
	if err != nil {
		return nil, err
	}
	pool, sysErr := systemCertPool()
	if sysErr != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("no PEM certificates found in %s", extraCAFile)
	}
	return pool, nil
}

// ClientConfig returns the TLS client config for the given CA file and
// skip-verify flag, or nil when Go's defaults apply unchanged.
func ClientConfig(extraCAFile string, insecureSkipVerify bool) (*tls.Config, error) {
	pool, err := RootPool(extraCAFile)
	if err != nil {
		return nil, err
	}
	if pool == nil && !insecureSkipVerify {
		return nil, nil
	}
	return &tls.Config{
		RootCAs:            pool,
		InsecureSkipVerify: insecureSkipVerify, //nolint:gosec // dev/self-signed only, opt-in
		MinVersion:         tls.VersionTLS12,
	}, nil
}
