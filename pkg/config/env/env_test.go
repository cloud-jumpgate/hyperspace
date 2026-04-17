package env_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-jumpgate/hyperspace/pkg/config/env"
)

func TestEnvLoader_Defaults(t *testing.T) {
	// Ensure no env vars interfere.
	clearEnv(t)

	l := env.New()
	cfg, err := l.Load(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "driver", cfg.Role)
	assert.Equal(t, "local", cfg.Env)
	assert.Equal(t, 4, cfg.PoolSize)
	assert.Equal(t, 20, cfg.ProbeIntervalMs)
	assert.Equal(t, "bbrv3", cfg.CCAlgorithm)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "", cfg.TLSCertPath)
	assert.Equal(t, "", cfg.TLSKeyPath)
	assert.Equal(t, "", cfg.TLSCAPath)
}

func TestEnvLoader_AllVarsSet(t *testing.T) {
	clearEnv(t)
	t.Setenv("HYPERSPACE_ROLE", "sender")
	t.Setenv("HYPERSPACE_ENV", "prod")
	t.Setenv("HYPERSPACE_POOL_SIZE", "8")
	t.Setenv("HYPERSPACE_PROBE_INTERVAL_MS", "50")
	t.Setenv("HYPERSPACE_CC_ALGORITHM", "cubic")
	t.Setenv("HYPERSPACE_LOG_LEVEL", "debug")
	t.Setenv("HYPERSPACE_TLS_CERT", "/etc/certs/cert.pem")
	t.Setenv("HYPERSPACE_TLS_KEY", "/etc/certs/key.pem")
	t.Setenv("HYPERSPACE_TLS_CA", "/etc/certs/ca.pem")

	l := env.New()
	cfg, err := l.Load(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "sender", cfg.Role)
	assert.Equal(t, "prod", cfg.Env)
	assert.Equal(t, 8, cfg.PoolSize)
	assert.Equal(t, 50, cfg.ProbeIntervalMs)
	assert.Equal(t, "cubic", cfg.CCAlgorithm)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, "/etc/certs/cert.pem", cfg.TLSCertPath)
	assert.Equal(t, "/etc/certs/key.pem", cfg.TLSKeyPath)
	assert.Equal(t, "/etc/certs/ca.pem", cfg.TLSCAPath)
}

func TestEnvLoader_InvalidPoolSize(t *testing.T) {
	clearEnv(t)
	t.Setenv("HYPERSPACE_POOL_SIZE", "not-a-number")

	l := env.New()
	_, err := l.Load(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HYPERSPACE_POOL_SIZE")
}

func TestEnvLoader_InvalidProbeInterval(t *testing.T) {
	clearEnv(t)
	t.Setenv("HYPERSPACE_PROBE_INTERVAL_MS", "abc")

	l := env.New()
	_, err := l.Load(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HYPERSPACE_PROBE_INTERVAL_MS")
}

func TestEnvLoader_PoolSizeZero(t *testing.T) {
	clearEnv(t)
	t.Setenv("HYPERSPACE_POOL_SIZE", "0")

	l := env.New()
	cfg, err := l.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, cfg.PoolSize)
}

func TestEnvLoader_ContextCancelled(t *testing.T) {
	// EnvLoader does not block; a cancelled context must not prevent loading.
	clearEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	l := env.New()
	cfg, err := l.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, "driver", cfg.Role)
}

// clearEnv unsets all HYPERSPACE_ variables for the duration of the test.
func clearEnv(t *testing.T) {
	t.Helper()
	vars := []string{
		"HYPERSPACE_ROLE",
		"HYPERSPACE_ENV",
		"HYPERSPACE_POOL_SIZE",
		"HYPERSPACE_PROBE_INTERVAL_MS",
		"HYPERSPACE_CC_ALGORITHM",
		"HYPERSPACE_LOG_LEVEL",
		"HYPERSPACE_TLS_CERT",
		"HYPERSPACE_TLS_KEY",
		"HYPERSPACE_TLS_CA",
	}
	for _, v := range vars {
		t.Setenv(v, "")
	}
}
