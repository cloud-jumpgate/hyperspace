package ssm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-jumpgate/hyperspace/pkg/config/ssm"
)

// mockSSMClient implements the ssmGetClient interface for testing.
type mockSSMClient struct {
	values map[string]string // path → value
	err    error             // non-nil to return an API error for every call
}

func (m *mockSSMClient) GetParameter(
	_ context.Context,
	params *awsssm.GetParameterInput,
	_ ...func(*awsssm.Options),
) (*awsssm.GetParameterOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	path := aws.ToString(params.Name)
	v, ok := m.values[path]
	if !ok {
		// Return the typed error so errors.As detection works correctly.
		msg := "parameter does not exist"
		return nil, &types.ParameterNotFound{Message: &msg}
	}
	return &awsssm.GetParameterOutput{
		Parameter: &types.Parameter{Value: aws.String(v)},
	}, nil
}

func TestSSMLoader_AllParametersPresent(t *testing.T) {
	clearEnv(t)

	mock := &mockSSMClient{
		values: map[string]string{
			"/hyperspace/prod/sender/pool_size":         "16",
			"/hyperspace/prod/sender/probe_interval_ms": "100",
			"/hyperspace/prod/sender/cc_algorithm":      "cubic",
			"/hyperspace/prod/sender/log_level":         "debug",
			"/hyperspace/prod/sender/tls_cert":          "/certs/cert.pem",
			"/hyperspace/prod/sender/tls_key":           "/certs/key.pem",
			"/hyperspace/prod/sender/tls_ca":            "/certs/ca.pem",
		},
	}

	l := ssm.NewWithClient(mock, "prod", "sender")
	cfg, err := l.Load(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "sender", cfg.Role)
	assert.Equal(t, "prod", cfg.Env)
	assert.Equal(t, 16, cfg.PoolSize)
	assert.Equal(t, 100, cfg.ProbeIntervalMs)
	assert.Equal(t, "cubic", cfg.CCAlgorithm)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, "/certs/cert.pem", cfg.TLSCertPath)
	assert.Equal(t, "/certs/key.pem", cfg.TLSKeyPath)
	assert.Equal(t, "/certs/ca.pem", cfg.TLSCAPath)
}

func TestSSMLoader_MissingParametersFallsBackToDefaults(t *testing.T) {
	clearEnv(t)

	// No parameters in SSM → all values come from EnvLoader defaults.
	mock := &mockSSMClient{values: map[string]string{}}

	l := ssm.NewWithClient(mock, "local", "driver")
	cfg, err := l.Load(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "driver", cfg.Role)
	assert.Equal(t, "local", cfg.Env)
	assert.Equal(t, 4, cfg.PoolSize)
	assert.Equal(t, 20, cfg.ProbeIntervalMs)
	assert.Equal(t, "bbrv3", cfg.CCAlgorithm)
	assert.Equal(t, "info", cfg.LogLevel)
}

func TestSSMLoader_PartialParametersPresent(t *testing.T) {
	clearEnv(t)

	mock := &mockSSMClient{
		values: map[string]string{
			"/hyperspace/staging/driver/pool_size": "8",
			// All other params missing → use defaults.
		},
	}

	l := ssm.NewWithClient(mock, "staging", "driver")
	cfg, err := l.Load(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 8, cfg.PoolSize)
	assert.Equal(t, 20, cfg.ProbeIntervalMs) // default
}

func TestSSMLoader_APIError(t *testing.T) {
	clearEnv(t)

	mock := &mockSSMClient{err: errors.New("connection refused")}

	l := ssm.NewWithClient(mock, "prod", "sender")
	_, err := l.Load(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

func TestSSMLoader_InvalidPoolSize(t *testing.T) {
	clearEnv(t)

	mock := &mockSSMClient{
		values: map[string]string{
			"/hyperspace/prod/sender/pool_size": "not-an-int",
		},
	}

	l := ssm.NewWithClient(mock, "prod", "sender")
	_, err := l.Load(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pool_size")
}

func TestSSMLoader_New_Compiles(t *testing.T) {
	// Verify New() compiles with a zero aws.Config (no real calls made).
	cfg := aws.Config{Region: "us-east-1"}
	l := ssm.New(cfg, "local", "driver")
	assert.NotNil(t, l)
}

func TestSSMLoader_InvalidProbeIntervalMs(t *testing.T) {
	clearEnv(t)

	mock := &mockSSMClient{
		values: map[string]string{
			"/hyperspace/prod/sender/probe_interval_ms": "not-a-number",
		},
	}

	l := ssm.NewWithClient(mock, "prod", "sender")
	_, err := l.Load(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "probe_interval_ms")
}

func TestSSMLoader_NilParameterValue(t *testing.T) {
	clearEnv(t)

	// Return a GetParameterOutput with a nil Parameter.Value — getString returns ("", false, nil).
	mock := &nilValueSSMClient{}
	l := ssm.NewWithClient(mock, "prod", "sender")
	cfg, err := l.Load(context.Background())
	require.NoError(t, err)
	// All values fall back to defaults since parameter value is nil.
	assert.Equal(t, "prod", cfg.Env)
	assert.Equal(t, "sender", cfg.Role)
}

func TestSSMLoader_APIErrorOnCCAlgorithm(t *testing.T) {
	clearEnv(t)

	// pool_size succeeds, then all others return an API error.
	mock := &partialErrSSMClient{
		okValues: map[string]string{
			"/hyperspace/prod/sender/pool_size": "4",
		},
		errAfterOK: errors.New("network timeout"),
	}

	l := ssm.NewWithClient(mock, "prod", "sender")
	_, err := l.Load(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "network timeout")
}

// nilValueSSMClient returns a non-nil output but with Parameter.Value == nil.
type nilValueSSMClient struct{}

func (n *nilValueSSMClient) GetParameter(
	_ context.Context,
	_ *awsssm.GetParameterInput,
	_ ...func(*awsssm.Options),
) (*awsssm.GetParameterOutput, error) {
	return &awsssm.GetParameterOutput{
		Parameter: &types.Parameter{Value: nil},
	}, nil
}

// partialErrSSMClient succeeds for keys in okValues; returns errAfterOK for everything else.
type partialErrSSMClient struct {
	okValues   map[string]string
	errAfterOK error
}

func (p *partialErrSSMClient) GetParameter(
	_ context.Context,
	params *awsssm.GetParameterInput,
	_ ...func(*awsssm.Options),
) (*awsssm.GetParameterOutput, error) {
	path := aws.ToString(params.Name)
	if v, ok := p.okValues[path]; ok {
		return &awsssm.GetParameterOutput{
			Parameter: &types.Parameter{Value: aws.String(v)},
		}, nil
	}
	return nil, p.errAfterOK
}

func clearEnv(t *testing.T) {
	t.Helper()
	vars := []string{
		"HYPERSPACE_ROLE", "HYPERSPACE_ENV", "HYPERSPACE_POOL_SIZE",
		"HYPERSPACE_PROBE_INTERVAL_MS", "HYPERSPACE_CC_ALGORITHM",
		"HYPERSPACE_LOG_LEVEL", "HYPERSPACE_TLS_CERT", "HYPERSPACE_TLS_KEY",
		"HYPERSPACE_TLS_CA",
	}
	for _, v := range vars {
		t.Setenv(v, "")
	}
}
