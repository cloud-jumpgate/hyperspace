// Package secrets defines the Provider interface for loading TLS credentials.
// Implementations include a file-based provider and an AWS Secrets Manager provider.
package secrets

import (
	"context"
	"crypto/tls"
)

// Provider loads TLS credentials for mutual TLS.
type Provider interface {
	// LoadTLSConfig returns a *tls.Config populated with the workload's
	// certificate, private key, and CA pool. The config requires TLS 1.3
	// and client certificate verification.
	LoadTLSConfig(ctx context.Context) (*tls.Config, error)
}
