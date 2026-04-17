// Package env provides an EnvLoader that reads DriverConfig from environment
// variables. Missing variables fall back to sensible defaults.
//
// Variables and defaults:
//
//	HYPERSPACE_ROLE             → "driver"
//	HYPERSPACE_ENV              → "local"
//	HYPERSPACE_POOL_SIZE        → 4
//	HYPERSPACE_PROBE_INTERVAL_MS→ 20
//	HYPERSPACE_CC_ALGORITHM     → "bbrv3"
//	HYPERSPACE_LOG_LEVEL        → "info"
//	HYPERSPACE_TLS_CERT         → ""
//	HYPERSPACE_TLS_KEY          → ""
//	HYPERSPACE_TLS_CA           → ""
package env

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/cloud-jumpgate/hyperspace/pkg/config"
)

// EnvLoader reads configuration from environment variables.
type EnvLoader struct{}

// New creates a new EnvLoader.
func New() *EnvLoader { return &EnvLoader{} }

// Load reads environment variables and returns a populated DriverConfig.
// Returns an error only if a numeric variable is present but not parseable.
func (l *EnvLoader) Load(_ context.Context) (*config.DriverConfig, error) {
	poolSize, err := getIntEnv("HYPERSPACE_POOL_SIZE", 4)
	if err != nil {
		return nil, fmt.Errorf("env: HYPERSPACE_POOL_SIZE: %w", err)
	}

	probeInterval, err := getIntEnv("HYPERSPACE_PROBE_INTERVAL_MS", 20)
	if err != nil {
		return nil, fmt.Errorf("env: HYPERSPACE_PROBE_INTERVAL_MS: %w", err)
	}

	return &config.DriverConfig{
		Role:            getStringEnv("HYPERSPACE_ROLE", "driver"),
		Env:             getStringEnv("HYPERSPACE_ENV", "local"),
		PoolSize:        poolSize,
		ProbeIntervalMs: probeInterval,
		CCAlgorithm:     getStringEnv("HYPERSPACE_CC_ALGORITHM", "bbrv3"),
		LogLevel:        getStringEnv("HYPERSPACE_LOG_LEVEL", "info"),
		TLSCertPath:     getStringEnv("HYPERSPACE_TLS_CERT", ""),
		TLSKeyPath:      getStringEnv("HYPERSPACE_TLS_KEY", ""),
		TLSCAPath:       getStringEnv("HYPERSPACE_TLS_CA", ""),
	}, nil
}

func getStringEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getIntEnv(key string, defaultVal int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("expected integer, got %q: %w", v, err)
	}
	return n, nil
}
