// Package file provides a FileProvider that loads TLS credentials from local
// PEM files. This is suitable for development and on-prem deployments where
// certificates are managed by external tooling (e.g. cert-manager, cfssl).
package file

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// FileProvider loads TLS credentials from local PEM files.
// All three paths (cert, key, CA) must be non-empty and readable.
type FileProvider struct {
	certPath string
	keyPath  string
	caPath   string
}

// New creates a FileProvider for the given PEM file paths.
func New(certPath, keyPath, caPath string) *FileProvider {
	return &FileProvider{
		certPath: certPath,
		keyPath:  keyPath,
		caPath:   caPath,
	}
}

// LoadTLSConfig reads the certificate, key, and CA from disk and assembles
// a *tls.Config with TLS 1.3 minimum and mutual client authentication.
func (p *FileProvider) LoadTLSConfig(_ context.Context) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(p.certPath, p.keyPath)
	if err != nil {
		return nil, fmt.Errorf("secrets/file: load key pair (%s, %s): %w", p.certPath, p.keyPath, err)
	}

	caPEM, err := os.ReadFile(p.caPath)
	if err != nil {
		return nil, fmt.Errorf("secrets/file: read CA bundle (%s): %w", p.caPath, err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("secrets/file: no valid PEM certificates found in CA bundle %s", p.caPath)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    caPool,
		RootCAs:      caPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	}, nil
}
