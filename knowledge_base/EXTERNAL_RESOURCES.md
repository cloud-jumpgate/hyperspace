# External Resources — Hyperspace

**Status:** Active
**Owner:** Harness Architect
**Last Updated:** 2026-04-18
**Project:** github.com/cloud-jumpgate/hyperspace

---

## Go Libraries (Direct Dependencies)

### quic-go — QUIC Transport

- **Import path:** `github.com/quic-go/quic-go`
- **Pinned version:** `v0.59.0` (see `go.mod`)
- **Purpose:** Pure-Go implementation of IETF RFC 9000 (QUIC) and RFC 9001 (TLS 1.3 for QUIC). Used as the sole transport layer in `pkg/transport/quic`. quic-go handles retransmission, flow control, connection migration, 0-RTT handshake, and stream multiplexing natively.
- **Key API surfaces used:**
  - `quic.DialAddr` / `quic.Listen` — client and server entry points
  - `quic.Connection.OpenStreamSync` — opens a new QUIC stream per frame batch
  - `quic.Config{MaxIncomingStreams: 4096, KeepAlivePeriod: 5s}` — connection parameters
  - `tls.Config{MinVersion: tls.VersionTLS13}` — mandatory TLS configuration
- **Known issue:** The package name `quic` conflicts with Hyperspace's own `pkg/transport/quic` package when both are imported in the same file. Use the alias `quictr "github.com/quic-go/quic-go"` to avoid shadowing. See `shared_knowledge.md` entry 3.
- **Documentation:** https://pkg.go.dev/github.com/quic-go/quic-go
- **Repository:** https://github.com/quic-go/quic-go
- **Monitoring:** Subscribe to quic-go GitHub releases for security advisories; run `govulncheck ./...` in CI

### go-spiffe — SPIFFE Workload API Client

- **Import path:** `github.com/spiffe/go-spiffe/v2`
- **Pinned version:** `v2.6.0` (see `go.mod`)
- **Purpose:** Official Go client library for the SPIFFE Workload API. Used in `pkg/identity` to fetch X.509 SVIDs from the SPIRE Agent Unix socket and watch for SVID rotation events.
- **Key API surfaces used:**
  - `workloadapi.NewClient(ctx, workloadapi.WithAddr("unix:///run/spire/agent.sock"))` — connect to SPIRE Agent
  - `client.FetchX509SVID(ctx)` — fetch the current X.509 SVID
  - `client.WatchX509Context(ctx, watcher)` — watch for SVID rotation (calls `OnX509ContextUpdate` before expiry)
  - `svid.Marshal()` — serialise the private key to PKCS#8 format (required for `tls.X509KeyPair`)
- **Critical gotcha:** `svid.Marshal()` produces PKCS#8 DER-encoded private key. Do NOT use `x509.MarshalECPrivateKey()` — it produces SEC1/PKCS#1 format which is incompatible with `tls.X509KeyPair` on the PKCS#8 PEM block. See `shared_knowledge.md` entry 9.
- **Test utilities:** `go-spiffe` includes test helpers in `spiffetesting` package for creating mock SPIRE Agents in unit tests — no real SPIRE installation required.
- **Documentation:** https://pkg.go.dev/github.com/spiffe/go-spiffe/v2
- **Repository:** https://github.com/spiffe/go-spiffe
- **SPIFFE specification:** https://spiffe.io/docs/latest/spiffe-about/spiffe-concepts/

### ONNX Runtime Go Binding — DRL Congestion Control Inference

- **Import path:** `github.com/yalue/onnxruntime_go`
- **Pinned version:** `v1.27.0` (see `go.mod`)
- **Purpose:** CGO binding for ONNX Runtime C library. Used exclusively in `pkg/cc/drl` (gated by `//go:build onnx`) to run inference on the trained DRL congestion control policy network.
- **Build requirements:** Requires ONNX Runtime shared library (`libonnxruntime.so`) present on the build host and on the EC2 AMI. Cross-compilation for arm64 requires the arm64 ONNX Runtime shared library. The ONNX Runtime version on the AMI must match the version the binding was built against.
- **Key API surfaces used:**
  - `onnxruntime_go.NewSession(modelPath)` — load the ONNX model
  - `session.Run(inputs)` — run inference with the state vector `[rtt_ms, loss_rate, cwnd, outstanding]`
  - `session.Destroy()` — release ONNX Runtime resources on shutdown
- **Fallback:** `DRLController` automatically delegates to BBRv3 if the model fails to load or inference errors exceed 5 in a 60-second window. The `//go:build onnx` tag means the fallback-only stub compiles without any CGO dependency.
- **Documentation:** https://pkg.go.dev/github.com/yalue/onnxruntime_go
- **ONNX Runtime:** https://onnxruntime.ai/docs/

### AWS SDK Go v2 — Cloud Integration

- **Import path:** `github.com/aws/aws-sdk-go-v2`
- **Pinned version:** `v1.41.6` (see `go.mod`)
- **Purpose:** Official AWS SDK for Go v2. Used in three packages:
  - `pkg/discovery` — `servicediscovery` (AWS Cloud Map) for peer endpoint resolution
  - `pkg/config` — `ssm` (AWS Systems Manager Parameter Store) for runtime configuration
  - `pkg/identity` — `secretsmanager` (AWS Secrets Manager) for TLS certificate fetch
- **Credential pattern:** All calls use `config.LoadDefaultConfig(ctx)` which resolves credentials in order: environment variables, `~/.aws/credentials`, EC2 instance metadata (IAM role). No hardcoded credentials anywhere. In CI, `AWS_ENDPOINT_URL` overrides the endpoint for Localstack.
- **Localstack compatibility:** AWS SDK v2 honours `AWS_ENDPOINT_URL` for endpoint override. Set `AWS_ENDPOINT_URL=http://localhost:4566` to route all calls to Localstack in integration tests.
- **Service-specific imports used:**
  - `github.com/aws/aws-sdk-go-v2/service/servicediscovery` v1.39.27
  - `github.com/aws/aws-sdk-go-v2/service/ssm` v1.68.5
  - `github.com/aws/aws-sdk-go-v2/service/secretsmanager` v1.41.6
- **Documentation:** https://aws.github.io/aws-sdk-go-v2/docs/
- **API reference:** https://pkg.go.dev/github.com/aws/aws-sdk-go-v2

### goleak — Goroutine Leak Detector

- **Import path:** `go.uber.org/goleak`
- **Pinned version:** `v1.3.0` (see `go.mod`)
- **Purpose:** Test helper that verifies no unexpected goroutines are running at the end of a test. Used in all driver agent tests (`pkg/driver/conductor`, `pkg/driver/sender`, `pkg/driver/receiver`, `pkg/driver/pathmgr`, `pkg/driver/poolmgr`) to enforce the goroutine lifecycle invariant.
- **Usage pattern:**
  ```go
  func TestMyAgent_GracefulShutdown(t *testing.T) {
      defer goleak.VerifyNone(t)
      // ... start agent, cancel ctx, wait ...
  }
  ```
- **What it detects:** goroutines started by the agent under test that do not terminate when `ctx` is cancelled. A goroutine leak detected by `goleak` is a P0 defect.
- **False positives:** Some background goroutines started by the Go runtime or test framework may appear as leaks. Use `goleak.IgnoreTopFunction` to suppress known-safe goroutines.
- **Documentation:** https://pkg.go.dev/go.uber.org/goleak
- **Repository:** https://github.com/uber-go/goleak

### OpenTelemetry Go SDK — Observability

- **Import path:** `go.opentelemetry.io/otel`
- **Pinned version:** `v1.43.0` (see `go.mod`)
- **Purpose:** OpenTelemetry metrics and tracing SDK. Used in `pkg/obs` to export `hyperspace.*` gauges and distributed traces to an OTLP/gRPC collector.
- **Metrics exported:**
  - `hyperspace.bytes_sent` — cumulative bytes sent
  - `hyperspace.bytes_received` — cumulative bytes received
  - `hyperspace.pool_size` — current connection pool size
  - `hyperspace.probe_rtt_us.<conn_id>` — per-connection EWMA RTT in microseconds
- **Initialization:** `obs.InitTracer(serviceName, otlpEndpoint)` configures the SDK. When `otlpEndpoint` is unreachable, the SDK logs a warning and continues — hsd does not exit.
- **Documentation:** https://opentelemetry.io/docs/languages/go/
- **API reference:** https://pkg.go.dev/go.opentelemetry.io/otel

---

## Architecture Reference Documents

### Aeron System Design (Reference Architecture)

- **Location:** `/Users/waynehamilton/agents/src/agent3/AERON_QUIC_SYSTEM_DESIGN.md` (parent repo)
- **Relevance:** Hyperspace's shared-memory log buffer architecture, three-term rotation, and client/driver separation are derived from Aeron's design. This document explains the Aeron DNA that Hyperspace inherits.
- **Key patterns borrowed:** Log buffer three-term rotation, AtomicBuffer, MPSC ring buffer for client-to-driver IPC, separate Conductor/Sender/Receiver agent model.
- **Key differences:** Aeron uses custom reliable UDP; Hyperspace uses Multi-QUIC. Aeron is JVM-based; Hyperspace is pure Go.

---

## Standards and RFCs

### RFC 9000 — QUIC Transport Protocol

- **URL:** https://www.rfc-editor.org/rfc/rfc9000
- **Relevance:** The foundational QUIC protocol specification implemented by quic-go. Relevant sections:
  - §7 — Cryptographic and Transport Handshake (TLS 1.3 integration)
  - §9 — Connection Migration (used for cert rotation)
  - §17 — Packet Formats
  - §19 — Flow Control

### RFC 9001 — Using TLS to Secure QUIC

- **URL:** https://www.rfc-editor.org/rfc/rfc9001
- **Relevance:** Defines how TLS 1.3 is integrated into QUIC. ALPN negotiation (`hyperspace/1`) and the `ClientHello` extension requirements are specified here.

### RFC 6298 — Computing TCP's Retransmission Timer (EWMA RTT Formula)

- **URL:** https://www.rfc-editor.org/rfc/rfc6298
- **Relevance:** PathManager's EWMA RTT formula is taken directly from RFC 6298 §2:
  - `sRTT = (1 - α) * sRTT + α * rttSample` where α = 0.125 (1/8)
  - `rttVar = (1 - β) * rttVar + β * |sRTT - rttSample|` where β = 0.25 (1/4)
- The formula is reproduced in `pkg/driver/pathmgr/pathmgr.go` with a comment citing this RFC.

### RFC 8312 — CUBIC for Fast Long-Distance Networks

- **URL:** https://www.rfc-editor.org/rfc/rfc8312
- **Relevance:** The CUBIC congestion control algorithm implemented in `pkg/cc/cubic`. Key sections:
  - §5 — CUBIC Increase Function `W_cubic(t) = C*(t - K)^3 + W_max`
  - §5.8 — Multiplicative Decrease: `W_max = cwnd * beta_cubic` where `beta_cubic = 0.7`

### SPIFFE Specification

- **URL:** https://spiffe.io/docs/latest/spiffe-about/spiffe-concepts/
- **Relevance:** Core concepts for SPIFFE/SPIRE identity used in `pkg/identity`:
  - SPIFFE ID format: `spiffe://trust-domain/path`
  - SVID: the X.509 certificate format with SPIFFE URI SAN
  - Workload API: the Unix socket protocol for SVID delivery
  - Trust Bundle: the CA certificate set for a trust domain

---

## Research and Case Studies

### BBR Congestion Control (Google, 2016)

- **Paper:** "BBR: Congestion-Based Congestion Control" — Cardwell et al., ACM Queue 2016
- **URL:** https://queue.acm.org/detail.cfm?id=3022184
- **Relevance:** Foundation for `pkg/cc/bbr` (BBRv1) implementation. BBR models network as a pipe (bandwidth × RTT) and controls cwnd to fill the pipe without creating a queue. Contrasts with CUBIC which is loss-based.

### BBRv3 Design

- **Document:** Google's BBRv3 IETF draft and implementation notes
- **URL:** https://datatracker.ietf.org/doc/html/draft-cardwell-iccrg-bbr-congestion-control
- **Relevance:** Foundation for `pkg/cc/bbrv3`. Key improvement over BBRv1: loss events above a threshold (not every loss) trigger cwnd reduction, improving stability in wireless/lossy environments.

### Aeron Protocol (Reference, Not Used)

- **URL:** https://github.com/real-logic/aeron/wiki/Protocol-Specification
- **Relevance:** Aeron's custom reliable UDP protocol is the architecture that Hyperspace consciously chose NOT to implement. Understanding Aeron's protocol helps justify ADR-002 (QUIC over Aeron Protocol).
