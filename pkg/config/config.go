// Package config defines the DriverConfig type and the Loader interface for
// resolving driver configuration from various sources (environment variables,
// AWS SSM Parameter Store, etc.).
package config

import "context"

// DriverConfig is the resolved configuration for an hsd driver instance.
type DriverConfig struct {
	Role            string // e.g. "driver", "sender", "receiver"
	Env             string // e.g. "local", "staging", "prod"
	PoolSize        int    // number of QUIC connections in the pool
	ProbeIntervalMs int    // path-probe interval in milliseconds
	CCAlgorithm     string // congestion control: "cubic", "bbrv1", "bbrv3", "drl"
	LogLevel        string // "debug", "info", "warn", "error"
	TLSCertPath     string // path to PEM certificate file (or empty)
	TLSKeyPath      string // path to PEM private key file (or empty)
	TLSCAPath       string // path to PEM CA bundle file (or empty)
}

// Loader loads configuration from a source.
type Loader interface {
	Load(ctx context.Context) (*DriverConfig, error)
}
