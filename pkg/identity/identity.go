// Package identity provides SPIFFE/SPIRE workload identity for Hyperspace.
// It fetches X.509 SVIDs from the SPIRE Workload API and assembles a
// *tls.Config suitable for mutual TLS.
//
// This package requires a running SPIRE agent at runtime. The Workload API
// socket must be accessible at the configured path (default:
// unix:///tmp/spire-agent/public/api.sock). Without a reachable SPIRE agent,
// calls to New and Fetch will return errors.
package identity

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"

	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

const defaultSocketPath = "unix:///tmp/spire-agent/public/api.sock"

// WorkloadIdentity holds the SPIFFE identity and TLS credentials for this workload.
type WorkloadIdentity struct {
	// SVID is the X.509 SPIFFE Verifiable Identity Document for this workload.
	SVID *x509svid.SVID
	// TLSConfig is a *tls.Config assembled from the SVID and trust bundle.
	// It enforces TLS 1.3 and mutual client authentication.
	TLSConfig *tls.Config
}

// workloadClient is the subset of workloadapi.Client used by SPIFFESource.
// Defined as an interface so tests can inject mocks without a running agent.
type workloadClient interface {
	FetchX509Context(ctx context.Context) (*workloadapi.X509Context, error)
	Close() error
}

// SPIFFESource fetches workload identity from the SPIRE Workload API.
type SPIFFESource struct {
	client workloadClient
}

// New connects to the SPIRE Workload API socket.
// If socketPath is empty, the default path is used:
//
//	unix:///tmp/spire-agent/public/api.sock
//
// Returns an error if the connection cannot be established.
func New(ctx context.Context, socketPath string) (*SPIFFESource, error) {
	if socketPath == "" {
		socketPath = defaultSocketPath
	}

	client, err := workloadapi.New(ctx, workloadapi.WithAddr(socketPath))
	if err != nil {
		return nil, fmt.Errorf("identity: connect to SPIRE agent at %s: %w", socketPath, err)
	}

	slog.Info("identity: connected to SPIRE Workload API", "socket", socketPath)
	return &SPIFFESource{client: client}, nil
}

// newWithClient creates a SPIFFESource with an injected client (for testing).
func newWithClient(client workloadClient) *SPIFFESource {
	return &SPIFFESource{client: client}
}

// Fetch retrieves the current workload identity from the SPIRE agent.
// It returns a WorkloadIdentity with the SVID and an assembled *tls.Config.
func (s *SPIFFESource) Fetch(ctx context.Context) (*WorkloadIdentity, error) {
	x509Ctx, err := s.client.FetchX509Context(ctx)
	if err != nil {
		return nil, fmt.Errorf("identity: FetchX509Context: %w", err)
	}

	if len(x509Ctx.SVIDs) == 0 {
		return nil, fmt.Errorf("identity: no SVIDs returned from SPIRE agent")
	}

	svid := x509Ctx.SVIDs[0]

	tlsCfg, err := buildTLSConfig(svid, x509Ctx)
	if err != nil {
		return nil, fmt.Errorf("identity: build TLS config: %w", err)
	}

	slog.Info("identity: fetched SPIFFE SVID",
		"spiffe_id", svid.ID.String(),
		"trust_domain", svid.ID.TrustDomain().String(),
	)

	return &WorkloadIdentity{
		SVID:      svid,
		TLSConfig: tlsCfg,
	}, nil
}

// Close releases the Workload API connection.
func (s *SPIFFESource) Close() error {
	if err := s.client.Close(); err != nil {
		return fmt.Errorf("identity: close Workload API client: %w", err)
	}
	return nil
}

// buildTLSConfig assembles a *tls.Config from the SVID and trust bundle context.
// The config uses TLS 1.3 minimum and mutual client authentication.
func buildTLSConfig(svid *x509svid.SVID, x509Ctx *workloadapi.X509Context) (*tls.Config, error) {
	// Marshal the SVID certificate chain and private key to PEM.
	certPEM, keyPEM, err := svid.Marshal()
	if err != nil {
		return nil, fmt.Errorf("marshal SVID: %w", err)
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("build X509 key pair: %w", err)
	}

	// Build CA pool from the trust bundles in the X509Context.
	caPool := x509.NewCertPool()
	bundles := x509Ctx.Bundles
	if bundles != nil {
		for _, bundle := range bundles.Bundles() {
			for _, caCert := range bundle.X509Authorities() {
				caDER := caCert.Raw
				caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
				caPool.AppendCertsFromPEM(caPEM)
			}
		}
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    caPool,
		RootCAs:      caPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	}, nil
}
