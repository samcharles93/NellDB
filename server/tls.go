package server

import (
	"crypto/tls"
	"fmt"
	"os"
)

// LoadTLSConfig reads a cert/key PEM pair and returns a *tls.Config suitable
// for http.Server.  Adapted from /work/infra/ca internal/proxy/proxy.go.
func LoadTLSConfig(certFile, keyFile string) (*tls.Config, error) {
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return nil, fmt.Errorf("read cert %q: %w", certFile, err)
	}
	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("read key %q: %w", keyFile, err)
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("load key pair: %w", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}, nil
}
