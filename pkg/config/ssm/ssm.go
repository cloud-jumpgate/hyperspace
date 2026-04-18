// Package ssm provides an SSMLoader that reads DriverConfig from AWS SSM
// Parameter Store. Parameters are fetched from /hyperspace/{env}/{role}/{param}.
// Any parameter absent from SSM falls back to the EnvLoader defaults.
package ssm

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/cloud-jumpgate/hyperspace/pkg/config"
	envloader "github.com/cloud-jumpgate/hyperspace/pkg/config/env"
)

// ssmGetClient is the subset of the SSM client used by SSMLoader.
// Defined as an interface so tests can inject mocks.
type ssmGetClient interface {
	GetParameter(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error)
}

// SSMLoader reads configuration from AWS SSM Parameter Store.
// Path pattern: /hyperspace/{env}/{role}/{param}
//
//nolint:revive // stutter is intentional: ssm.SSMLoader is the established public API name
type SSMLoader struct {
	client ssmGetClient
	env    string
	role   string
}

// New creates an SSMLoader using the supplied aws.Config, env, and role strings.
func New(cfg aws.Config, env, role string) *SSMLoader {
	return &SSMLoader{
		client: ssm.NewFromConfig(cfg),
		env:    env,
		role:   role,
	}
}

// newWithClient creates an SSMLoader with an injected client (for testing).
func newWithClient(client ssmGetClient, env, role string) *SSMLoader {
	return &SSMLoader{client: client, env: env, role: role}
}

// Load fetches parameters from SSM and returns a populated DriverConfig.
// Parameters absent from SSM fall back to EnvLoader defaults.
func (l *SSMLoader) Load(ctx context.Context) (*config.DriverConfig, error) {
	// Start with env-var defaults as the baseline.
	base, err := envloader.New().Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("ssm: baseline EnvLoader: %w", err)
	}

	// Override with SSM values where present.
	base.Role = l.role
	base.Env = l.env

	if v, ok, err := l.getString(ctx, "pool_size"); err != nil {
		return nil, err
	} else if ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("ssm: pool_size: %w", err)
		}
		base.PoolSize = n
	}

	if v, ok, err := l.getString(ctx, "probe_interval_ms"); err != nil {
		return nil, err
	} else if ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("ssm: probe_interval_ms: %w", err)
		}
		base.ProbeIntervalMs = n
	}

	if v, ok, err := l.getString(ctx, "cc_algorithm"); err != nil {
		return nil, err
	} else if ok {
		base.CCAlgorithm = v
	}

	if v, ok, err := l.getString(ctx, "log_level"); err != nil {
		return nil, err
	} else if ok {
		base.LogLevel = v
	}

	if v, ok, err := l.getString(ctx, "tls_cert"); err != nil {
		return nil, err
	} else if ok {
		base.TLSCertPath = v
	}

	if v, ok, err := l.getString(ctx, "tls_key"); err != nil {
		return nil, err
	} else if ok {
		base.TLSKeyPath = v
	}

	if v, ok, err := l.getString(ctx, "tls_ca"); err != nil {
		return nil, err
	} else if ok {
		base.TLSCAPath = v
	}

	return base, nil
}

// getString fetches a single SSM parameter and returns (value, found, error).
// A ParameterNotFound response returns (empty, false, nil) — not an error.
func (l *SSMLoader) getString(ctx context.Context, param string) (string, bool, error) {
	path := fmt.Sprintf("/hyperspace/%s/%s/%s", l.env, l.role, param)

	out, err := l.client.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(path),
		WithDecryption: aws.Bool(true),
	})
	if err != nil {
		// AWS returns a ParameterNotFound error for missing parameters.
		// We treat this as a missing value, not a fatal error.
		if strings.Contains(err.Error(), "ParameterNotFound") {
			slog.Debug("ssm: parameter not found, using default",
				"path", path,
			)
			return "", false, nil
		}
		return "", false, fmt.Errorf("ssm: GetParameter(%s): %w", path, err)
	}

	if out.Parameter == nil || out.Parameter.Value == nil {
		return "", false, nil
	}

	return aws.ToString(out.Parameter.Value), true, nil
}
