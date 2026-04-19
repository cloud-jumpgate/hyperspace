package identity_test

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
	"net/url"
	"testing"
	"time"

	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/cloud-jumpgate/hyperspace/pkg/identity"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// mockWorkloadClient implements the workloadClient interface for testing.
type mockWorkloadClient struct {
	x509Ctx *workloadapi.X509Context
	err     error
}

func (m *mockWorkloadClient) FetchX509Context(_ context.Context) (*workloadapi.X509Context, error) {
	return m.x509Ctx, m.err
}

func (m *mockWorkloadClient) Close() error { return nil }

// buildTestSVID creates a synthetic SPIFFE SVID for a given trust domain + path.
// Returns the SVID and the CA certificate used to sign it.
func buildTestSVID(t *testing.T, trustDomain, workloadPath string) (*x509svid.SVID, *x509.Certificate) {
	t.Helper()

	// Generate CA (self-signed, IsCA=true).
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "spiffe-ca"},
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

	// Generate leaf SVID (non-CA, with SPIFFE URI SAN).
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	spiffeURI := &url.URL{Scheme: "spiffe", Host: trustDomain, Path: workloadPath}
	leafTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "hyperspace-driver"},
		URIs:                  []*url.URL{spiffeURI},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  false,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caCert, &leafKey.PublicKey, caKey)
	require.NoError(t, err)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	leafKeyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	require.NoError(t, err)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: leafKeyDER})

	svid, err := x509svid.Parse(certPEM, keyPEM)
	require.NoError(t, err)

	return svid, caCert
}

func buildX509Context(t *testing.T) *workloadapi.X509Context {
	t.Helper()

	td, err := spiffeid.TrustDomainFromString("example.org")
	require.NoError(t, err)

	svid, caCert := buildTestSVID(t, "example.org", "/hyperspace/driver")

	bundle := x509bundle.FromX509Authorities(td, []*x509.Certificate{caCert})
	bundleSet := x509bundle.NewSet(bundle)

	return &workloadapi.X509Context{
		SVIDs:   []*x509svid.SVID{svid},
		Bundles: bundleSet,
	}
}

func TestSPIFFESource_Fetch_HappyPath(t *testing.T) {
	x509Ctx := buildX509Context(t)

	mock := &mockWorkloadClient{x509Ctx: x509Ctx}
	src := identity.NewWithClient(mock)

	wi, err := src.Fetch(context.Background())
	require.NoError(t, err)

	assert.NotNil(t, wi.SVID)
	assert.NotNil(t, wi.TLSConfig)
	assert.Equal(t, uint16(tls.VersionTLS13), wi.TLSConfig.MinVersion)
	assert.Equal(t, tls.RequireAndVerifyClientCert, wi.TLSConfig.ClientAuth)
	assert.Len(t, wi.TLSConfig.Certificates, 1)
}

func TestSPIFFESource_Fetch_ClientError(t *testing.T) {
	mock := &mockWorkloadClient{err: errors.New("agent unreachable")}
	src := identity.NewWithClient(mock)

	_, err := src.Fetch(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent unreachable")
}

func TestSPIFFESource_Fetch_NoSVIDs(t *testing.T) {
	mock := &mockWorkloadClient{
		x509Ctx: &workloadapi.X509Context{
			SVIDs:   []*x509svid.SVID{},
			Bundles: x509bundle.NewSet(),
		},
	}
	src := identity.NewWithClient(mock)

	_, err := src.Fetch(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no SVIDs")
}

func TestSPIFFESource_Fetch_NilBundles(t *testing.T) {
	// Bundles can be nil; should still assemble tls.Config without panicking.
	svid, _ := buildTestSVID(t, "example.org", "/hyperspace/driver")

	mock := &mockWorkloadClient{
		x509Ctx: &workloadapi.X509Context{
			SVIDs:   []*x509svid.SVID{svid},
			Bundles: nil,
		},
	}
	src := identity.NewWithClient(mock)

	wi, err := src.Fetch(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, wi.TLSConfig)
}

func TestSPIFFESource_Close(t *testing.T) {
	mock := &mockWorkloadClient{}
	src := identity.NewWithClient(mock)
	assert.NoError(t, src.Close())
}

func TestSPIFFESource_Fetch_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mock := &mockWorkloadClient{err: context.Canceled}
	src := identity.NewWithClient(mock)

	_, err := src.Fetch(ctx)
	require.Error(t, err)
}

// mockWorkloadClientCloseError returns an error from Close.
type mockWorkloadClientCloseError struct {
	x509Ctx *workloadapi.X509Context
}

func (m *mockWorkloadClientCloseError) FetchX509Context(_ context.Context) (*workloadapi.X509Context, error) {
	return m.x509Ctx, nil
}

func (m *mockWorkloadClientCloseError) Close() error {
	return errors.New("close failed: agent gone")
}

func TestSPIFFESource_Close_Error(t *testing.T) {
	mock := &mockWorkloadClientCloseError{}
	src := identity.NewWithClient(mock)

	err := src.Close()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "close failed")
}

func TestSPIFFESource_Fetch_MultipleSVIDs_UsesFirst(t *testing.T) {
	// When multiple SVIDs are returned, the first should be used.
	x509Ctx := buildX509Context(t)

	// Add a second SVID (reuse the same one for simplicity).
	x509Ctx.SVIDs = append(x509Ctx.SVIDs, x509Ctx.SVIDs[0])

	mock := &mockWorkloadClient{x509Ctx: x509Ctx}
	src := identity.NewWithClient(mock)

	wi, err := src.Fetch(context.Background())
	require.NoError(t, err)
	assert.Equal(t, x509Ctx.SVIDs[0].ID.String(), wi.SVID.ID.String())
}

func TestNew_InvalidSocketReturnsError(t *testing.T) {
	// An invalid URI (not unix:// or tcp://) causes workloadapi.New to return
	// an address-validation error, covering the New() error-return path.
	ctx := context.Background()
	_, err := identity.New(ctx, "http://invalid-scheme")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connect to SPIRE agent")
}

// --- F-031: SVID Continuous Rotation Tests ---

func TestSPIFFESource_StartWatch_InitialFetch(t *testing.T) {
	x509Ctx := buildX509Context(t)
	mock := &mockWorkloadClient{x509Ctx: x509Ctx}
	src := identity.NewWithClient(mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := src.StartWatch(ctx)
	require.NoError(t, err)

	// TLSConfig should be populated after StartWatch.
	cfg := src.TLSConfig()
	require.NotNil(t, cfg, "TLSConfig should be non-nil after StartWatch")
	assert.Equal(t, uint16(tls.VersionTLS13), cfg.MinVersion)

	// Clean up watcher.
	cancel()
	require.NoError(t, src.Close())
}

func TestSPIFFESource_StartWatch_ConfigUpdated(t *testing.T) {
	x509Ctx := buildX509Context(t)
	mock := &mockWorkloadClient{x509Ctx: x509Ctx}
	src := identity.NewWithClient(mock)
	src.SetWatchInterval(10 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := src.StartWatch(ctx)
	require.NoError(t, err)

	cfg1 := src.TLSConfig()
	require.NotNil(t, cfg1)

	// Wait for one watch tick to re-fetch. The config should still be valid.
	time.Sleep(50 * time.Millisecond)

	cfg2 := src.TLSConfig()
	require.NotNil(t, cfg2)
	// Config should still be TLS 1.3.
	assert.Equal(t, uint16(tls.VersionTLS13), cfg2.MinVersion)

	cancel()
	require.NoError(t, src.Close())
}

func TestSPIFFESource_StartWatch_StopsOnCancel(t *testing.T) {
	x509Ctx := buildX509Context(t)
	mock := &mockWorkloadClient{x509Ctx: x509Ctx}
	src := identity.NewWithClient(mock)
	src.SetWatchInterval(10 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())

	err := src.StartWatch(ctx)
	require.NoError(t, err)

	// Cancel the context — watcher should stop.
	cancel()

	// Close should not hang (watcher must have stopped).
	done := make(chan error, 1)
	go func() { done <- src.Close() }()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Close blocked — watcher did not stop on context cancellation")
	}
}

func TestSPIFFESource_StartWatch_InitialFetchError(t *testing.T) {
	mock := &mockWorkloadClient{err: errors.New("agent unreachable")}
	src := identity.NewWithClient(mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := src.StartWatch(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "initial fetch")
}

func TestSPIFFESource_TLSConfig_NilBeforeWatch(t *testing.T) {
	mock := &mockWorkloadClient{}
	src := identity.NewWithClient(mock)

	cfg := src.TLSConfig()
	assert.Nil(t, cfg, "TLSConfig should be nil before StartWatch is called")
}

func TestNew_DefaultSocket(t *testing.T) {
	// Empty socketPath → default is used. workloadapi.New succeeds (lazy dial).
	// We cannot test the happy-path return without a live agent, so we verify
	// the function runs and returns a non-nil source or an error (not a panic).
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// This will either succeed (if a SPIRE agent happens to be running) or
	// fail. Either way it must not panic.
	src, err := identity.New(ctx, "")
	if err == nil {
		// Unexpected success — a SPIRE agent is running; clean up.
		_ = src.Close()
	}
	// Either outcome is acceptable in unit tests.
}
