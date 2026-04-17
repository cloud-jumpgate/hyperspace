package secretsmanager_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssm "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-jumpgate/hyperspace/pkg/secrets/secretsmanager"
)

// mockSMClient implements smGetClient for testing.
type mockSMClient struct {
	secrets map[string]string // secret ID → PEM string
	err     error
}

func (m *mockSMClient) GetSecretValue(
	_ context.Context,
	params *awssm.GetSecretValueInput,
	_ ...func(*awssm.Options),
) (*awssm.GetSecretValueOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	id := aws.ToString(params.SecretId)
	v, ok := m.secrets[id]
	if !ok {
		return nil, errors.New("ResourceNotFoundException: secret not found")
	}
	return &awssm.GetSecretValueOutput{SecretString: aws.String(v)}, nil
}

// generateTestCerts returns (certPEM, keyPEM, caPEM) as byte slices.
func generateTestCerts(t *testing.T) ([]byte, []byte, []byte) {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)
	caCert, err := x509.ParseCertificate(caDER)
	require.NoError(t, err)

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "test-leaf"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caCert, &leafKey.PublicKey, caKey)
	require.NoError(t, err)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})

	leafKeyDER, err := x509.MarshalECPrivateKey(leafKey)
	require.NoError(t, err)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: leafKeyDER})

	return certPEM, keyPEM, caPEM
}

func TestSecretsManagerProvider_LoadTLSConfig_HappyPath(t *testing.T) {
	certPEM, keyPEM, caPEM := generateTestCerts(t)

	mock := &mockSMClient{
		secrets: map[string]string{
			"myapp/tls/cert": string(certPEM),
			"myapp/tls/key":  string(keyPEM),
			"myapp/tls/ca":   string(caPEM),
		},
	}

	p := secretsmanager.NewWithClient(mock, "myapp/tls")
	cfg, err := p.LoadTLSConfig(context.Background())
	require.NoError(t, err)

	assert.Equal(t, uint16(tls.VersionTLS13), cfg.MinVersion)
	assert.Equal(t, tls.RequireAndVerifyClientCert, cfg.ClientAuth)
	assert.Len(t, cfg.Certificates, 1)
	assert.NotNil(t, cfg.ClientCAs)
	assert.NotNil(t, cfg.RootCAs)
}

func TestSecretsManagerProvider_LoadTLSConfig_MissingCert(t *testing.T) {
	_, keyPEM, caPEM := generateTestCerts(t)

	mock := &mockSMClient{
		secrets: map[string]string{
			// cert missing
			"myapp/tls/key": string(keyPEM),
			"myapp/tls/ca":  string(caPEM),
		},
	}

	p := secretsmanager.NewWithClient(mock, "myapp/tls")
	_, err := p.LoadTLSConfig(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cert")
}

func TestSecretsManagerProvider_LoadTLSConfig_InvalidPEM(t *testing.T) {
	certPEM, keyPEM, caPEM := generateTestCerts(t)

	mock := &mockSMClient{
		secrets: map[string]string{
			"myapp/tls/cert": "not-valid-pem",
			"myapp/tls/key":  string(keyPEM),
			"myapp/tls/ca":   string(caPEM),
		},
	}
	_ = certPEM

	p := secretsmanager.NewWithClient(mock, "myapp/tls")
	_, err := p.LoadTLSConfig(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PEM")
}

func TestSecretsManagerProvider_LoadTLSConfig_APIError(t *testing.T) {
	mock := &mockSMClient{err: errors.New("access denied")}

	p := secretsmanager.NewWithClient(mock, "myapp/tls")
	_, err := p.LoadTLSConfig(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "access denied")
}

func TestSecretsManagerProvider_LoadTLSConfig_InvalidCAContent(t *testing.T) {
	certPEM, keyPEM, _ := generateTestCerts(t)

	mock := &mockSMClient{
		secrets: map[string]string{
			"myapp/tls/cert": string(certPEM),
			"myapp/tls/key":  string(keyPEM),
			"myapp/tls/ca":   "-----BEGIN CERTIFICATE-----\nnot-real-cert\n-----END CERTIFICATE-----\n",
		},
	}

	p := secretsmanager.NewWithClient(mock, "myapp/tls")
	_, err := p.LoadTLSConfig(context.Background())
	require.Error(t, err)
	// Could fail on parse or AppendCertsFromPEM
	assert.NotNil(t, err)
}

func TestSecretsManagerProvider_New_Compiles(t *testing.T) {
	cfg := aws.Config{Region: "us-east-1"}
	p := secretsmanager.New(cfg, "myapp/tls")
	assert.NotNil(t, p)
}

// mockSMClientBinary is a mock that returns secrets as binary (SecretBinary)
// rather than SecretString, to cover the binary branch.
type mockSMClientBinary struct {
	secrets map[string][]byte
}

func (m *mockSMClientBinary) GetSecretValue(
	_ context.Context,
	params *awssm.GetSecretValueInput,
	_ ...func(*awssm.Options),
) (*awssm.GetSecretValueOutput, error) {
	id := aws.ToString(params.SecretId)
	v, ok := m.secrets[id]
	if !ok {
		return nil, errors.New("ResourceNotFoundException: secret not found")
	}
	return &awssm.GetSecretValueOutput{SecretBinary: v}, nil
}

func TestSecretsManagerProvider_LoadTLSConfig_BinarySecrets(t *testing.T) {
	certPEM, keyPEM, caPEM := generateTestCerts(t)

	mock := &mockSMClientBinary{
		secrets: map[string][]byte{
			"myapp/tls/cert": certPEM,
			"myapp/tls/key":  keyPEM,
			"myapp/tls/ca":   caPEM,
		},
	}

	p := secretsmanager.NewWithClient(mock, "myapp/tls")
	cfg, err := p.LoadTLSConfig(context.Background())
	require.NoError(t, err)

	assert.Equal(t, uint16(tls.VersionTLS13), cfg.MinVersion)
	assert.Len(t, cfg.Certificates, 1)
}

func TestSecretsManagerProvider_LoadTLSConfig_BinaryInvalidPEM(t *testing.T) {
	certPEM, keyPEM, caPEM := generateTestCerts(t)

	mock := &mockSMClientBinary{
		secrets: map[string][]byte{
			"myapp/tls/cert": []byte("not-pem-binary"),
			"myapp/tls/key":  keyPEM,
			"myapp/tls/ca":   caPEM,
		},
	}
	_ = certPEM

	p := secretsmanager.NewWithClient(mock, "myapp/tls")
	_, err := p.LoadTLSConfig(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PEM")
}

// mockSMClientEmpty returns an output with neither SecretString nor SecretBinary.
type mockSMClientEmpty struct{}

func (m *mockSMClientEmpty) GetSecretValue(
	_ context.Context,
	_ *awssm.GetSecretValueInput,
	_ ...func(*awssm.Options),
) (*awssm.GetSecretValueOutput, error) {
	return &awssm.GetSecretValueOutput{}, nil
}

func TestSecretsManagerProvider_LoadTLSConfig_EmptyOutput(t *testing.T) {
	p := secretsmanager.NewWithClient(&mockSMClientEmpty{}, "myapp/tls")
	_, err := p.LoadTLSConfig(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "neither SecretString nor SecretBinary")
}
