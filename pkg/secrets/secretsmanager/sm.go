// Package secretsmanager provides a SecretsManagerProvider that loads TLS
// credentials from AWS Secrets Manager. Three secrets are fetched:
//
//	{secretID}/cert — PEM certificate (+ chain)
//	{secretID}/key  — PEM private key
//	{secretID}/ca   — PEM CA bundle
//
// The assembled tls.Config enforces TLS 1.3 and mutual client authentication,
// matching the behaviour of pkg/secrets/file.FileProvider.
package secretsmanager

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// smGetClient is the subset of the Secrets Manager client that
// SecretsManagerProvider requires. Defined as an interface for testability.
type smGetClient interface {
	GetSecretValue(ctx context.Context, params *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

// SecretsManagerProvider loads TLS credentials from AWS Secrets Manager.
type SecretsManagerProvider struct {
	client   smGetClient
	secretID string
}

// New creates a SecretsManagerProvider using the supplied aws.Config and secretID.
func New(cfg aws.Config, secretID string) *SecretsManagerProvider {
	return &SecretsManagerProvider{
		client:   secretsmanager.NewFromConfig(cfg),
		secretID: secretID,
	}
}

// newWithClient creates a SecretsManagerProvider with an injected client (for testing).
func newWithClient(client smGetClient, secretID string) *SecretsManagerProvider {
	return &SecretsManagerProvider{client: client, secretID: secretID}
}

// LoadTLSConfig fetches the cert, key, and CA PEM bundles from Secrets Manager
// and assembles a *tls.Config with TLS 1.3 minimum and mutual client auth.
func (p *SecretsManagerProvider) LoadTLSConfig(ctx context.Context) (*tls.Config, error) {
	certPEM, err := p.fetchSecret(ctx, p.secretID+"/cert")
	if err != nil {
		return nil, fmt.Errorf("secrets/secretsmanager: fetch cert: %w", err)
	}

	keyPEM, err := p.fetchSecret(ctx, p.secretID+"/key")
	if err != nil {
		return nil, fmt.Errorf("secrets/secretsmanager: fetch key: %w", err)
	}

	caPEM, err := p.fetchSecret(ctx, p.secretID+"/ca")
	if err != nil {
		return nil, fmt.Errorf("secrets/secretsmanager: fetch ca: %w", err)
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("secrets/secretsmanager: parse key pair: %w", err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("secrets/secretsmanager: no valid PEM certificates in CA bundle for secret %s/ca", p.secretID)
	}

	slog.Debug("secrets/secretsmanager: TLS config loaded", "secret_id", p.secretID)

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    caPool,
		RootCAs:      caPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	}, nil
}

// fetchSecret retrieves the string value of a single secret by its full ID.
func (p *SecretsManagerProvider) fetchSecret(ctx context.Context, id string) ([]byte, error) {
	out, err := p.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(id),
	})
	if err != nil {
		return nil, fmt.Errorf("GetSecretValue(%s): %w", id, err)
	}

	if out.SecretString != nil {
		raw := []byte(aws.ToString(out.SecretString))
		// Validate it looks like PEM.
		if b, _ := pem.Decode(raw); b == nil {
			return nil, fmt.Errorf("secret %s does not contain valid PEM data", id)
		}
		return raw, nil
	}

	if out.SecretBinary != nil {
		if b, _ := pem.Decode(out.SecretBinary); b == nil {
			return nil, fmt.Errorf("secret %s (binary) does not contain valid PEM data", id)
		}
		return out.SecretBinary, nil
	}

	return nil, fmt.Errorf("secret %s has neither SecretString nor SecretBinary", id)
}
