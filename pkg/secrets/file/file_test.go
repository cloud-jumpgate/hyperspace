package file_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloud-jumpgate/hyperspace/pkg/secrets/file"
)

// generateTestCerts creates a self-signed CA and a leaf cert signed by that CA.
// Returns (certPEM, keyPEM, caPEM).
func generateTestCerts(t *testing.T) ([]byte, []byte, []byte) {
	t.Helper()

	// Generate CA key + self-signed cert.
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

	// Generate leaf key + cert signed by CA.
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

func writeTempFiles(t *testing.T, certPEM, keyPEM, caPEM []byte) (certPath, keyPath, caPath string) {
	t.Helper()
	dir := t.TempDir()

	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	caPath = filepath.Join(dir, "ca.pem")

	require.NoError(t, os.WriteFile(certPath, certPEM, 0600))
	require.NoError(t, os.WriteFile(keyPath, keyPEM, 0600))
	require.NoError(t, os.WriteFile(caPath, caPEM, 0600))
	return
}

func TestFileProvider_LoadTLSConfig_HappyPath(t *testing.T) {
	certPEM, keyPEM, caPEM := generateTestCerts(t)
	certPath, keyPath, caPath := writeTempFiles(t, certPEM, keyPEM, caPEM)

	p := file.New(certPath, keyPath, caPath)
	cfg, err := p.LoadTLSConfig(context.Background())
	require.NoError(t, err)

	assert.Equal(t, uint16(tls.VersionTLS13), cfg.MinVersion)
	assert.Equal(t, tls.RequireAndVerifyClientCert, cfg.ClientAuth)
	assert.Len(t, cfg.Certificates, 1)
	assert.NotNil(t, cfg.ClientCAs)
	assert.NotNil(t, cfg.RootCAs)
}

func TestFileProvider_LoadTLSConfig_MissingCertFile(t *testing.T) {
	_, keyPEM, caPEM := generateTestCerts(t)
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.pem")
	caPath := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(keyPath, keyPEM, 0600))
	require.NoError(t, os.WriteFile(caPath, caPEM, 0600))

	p := file.New("/nonexistent/cert.pem", keyPath, caPath)
	_, err := p.LoadTLSConfig(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load key pair")
}

func TestFileProvider_LoadTLSConfig_MissingKeyFile(t *testing.T) {
	certPEM, _, caPEM := generateTestCerts(t)
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	caPath := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(certPath, certPEM, 0600))
	require.NoError(t, os.WriteFile(caPath, caPEM, 0600))

	p := file.New(certPath, "/nonexistent/key.pem", caPath)
	_, err := p.LoadTLSConfig(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load key pair")
}

func TestFileProvider_LoadTLSConfig_MissingCAFile(t *testing.T) {
	certPEM, keyPEM, _ := generateTestCerts(t)
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	require.NoError(t, os.WriteFile(certPath, certPEM, 0600))
	require.NoError(t, os.WriteFile(keyPath, keyPEM, 0600))

	p := file.New(certPath, keyPath, "/nonexistent/ca.pem")
	_, err := p.LoadTLSConfig(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read CA bundle")
}

func TestFileProvider_LoadTLSConfig_InvalidCAContent(t *testing.T) {
	certPEM, keyPEM, _ := generateTestCerts(t)
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	caPath := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(certPath, certPEM, 0600))
	require.NoError(t, os.WriteFile(keyPath, keyPEM, 0600))
	require.NoError(t, os.WriteFile(caPath, []byte("not a valid PEM cert"), 0600))

	p := file.New(certPath, keyPath, caPath)
	_, err := p.LoadTLSConfig(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no valid PEM certificates")
}

func TestFileProvider_LoadTLSConfig_ContextIgnored(t *testing.T) {
	// FileProvider doesn't block on context; cancelled context must not cause errors.
	certPEM, keyPEM, caPEM := generateTestCerts(t)
	certPath, keyPath, caPath := writeTempFiles(t, certPEM, keyPEM, caPEM)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := file.New(certPath, keyPath, caPath)
	cfg, err := p.LoadTLSConfig(ctx)
	require.NoError(t, err)
	assert.NotNil(t, cfg)
}
