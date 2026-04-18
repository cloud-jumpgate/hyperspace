# Security Knowledge — Hyperspace

**Status:** Active
**Owner:** Harness Architect
**Last Updated:** 2026-04-18
**Project:** github.com/cloud-jumpgate/hyperspace

---

## Security Checklist

All items below must pass before any sprint is marked as fully passing. The Security Evaluator audits against this checklist at every S9-or-later sprint evaluation and at every production deploy gate.

| Check | Tool | Gate |
|---|---|---|
| Zero high-severity `gosec` findings | `gosec ./...` | PR merge |
| TLS 1.3 minimum on all QUIC connections | `gosec` + TLS config audit | PR merge |
| No hardcoded credentials | `gosec` G101, G304 | PR merge |
| mmap files created at 0600 | `TestMmap_FilePermissions` | Sprint acceptance |
| Frame length validated before read | Code review + `gosec` G115 | Code Evaluator PASS |
| No goroutine leaks | `goleak.VerifyNone(t)` in all driver tests | Sprint acceptance |
| Zero known vulnerabilities | `govulncheck ./...` | Deploy gate |
| IAM role credentials only | `gosec` G101 + code review | Sprint acceptance (S9) |

---

## TLS 1.3 Minimum Enforcement

**Where it is enforced:** `pkg/transport/quic/config.go` — the `NewTLSConfig` function sets `MinVersion: tls.VersionTLS13` on every `*tls.Config` it returns. This is the only function permitted to create a `tls.Config` for QUIC connections.

**How it is enforced in CI:** `.github/workflows/ci.yml` runs `gosec ./...` which flags any `tls.Config` without an explicit `MinVersion` or with `MinVersion` below `tls.VersionTLS13`. The `golangci-lint` configuration also enables the `gosec` linter integration.

**How it is tested:** `TestTLSConfig_MinVersion` in `pkg/transport/quic/config_test.go` asserts `cfg.MinVersion == tls.VersionTLS13`. Any `tls.Config` constructed outside `NewTLSConfig` (e.g., in a test helper) is also inspected by this test via a registry pattern.

**Failure mode:** A PR that introduces a `tls.Config` without `MinVersion: tls.VersionTLS13` will fail `gosec` in CI. The Security Evaluator will issue a FAIL verdict. This is a non-waivable defect.

---

## SPIFFE/SPIRE Identity Model

Hyperspace uses SPIFFE (Secure Production Identity Framework for Everyone) via the SPIRE (SPIFFE Runtime Environment) implementation for workload identity. This is the authoritative identity mechanism for all production QUIC connections.

**Components:**

- **SPIRE Server:** Runs per-cluster (not per-node). Issues short-lived X.509 SVIDs to SPIRE Agents based on node attestation and workload attestation policies. Not managed by Hyperspace — operational concern.
- **SPIRE Agent:** Runs on each EC2 node as a DaemonSet or systemd service. Exposes the SPIFFE Workload API on a Unix socket at `/run/spire/agent.sock` (configurable via SSM `hyperspace/spire/socket_path`).
- **SVID (SPIFFE Verifiable Identity Document):** A short-lived X.509 certificate with a SPIFFE URI SAN (`spiffe://trust-domain/workload-id`) in the Subject Alternative Names. Default TTL is 1 hour; `pkg/identity` watches for rotation and delivers new SVIDs before expiry.
- **Trust Bundle:** A set of CA certificates defining the trust domain. Both client and server verify peer certificates against the trust bundle. Peers outside the trust domain are rejected at TLS handshake.

**Workload API socket path:** `unix:///run/spire/agent.sock` is the default. Override via SSM parameter `hyperspace/spire/socket_path` at runtime. In tests, go-spiffe test utilities provide a mock SPIRE Agent — no real socket required.

**Watch API:** `SPIFFEIdentity.Watch(ctx, onChange)` uses the go-spiffe `x509bundle.Watch` API. The SPIRE Agent pushes new SVIDs before they expire. When `onChange` fires, PoolManager opens new QUIC connections authenticated with the new cert, drains in-flight messages on old connections, then closes old connections. This is the zero-message-loss cert rotation path.

---

## mTLS Configuration

All QUIC connections in Hyperspace are mutually authenticated. Anonymous connections are rejected.

**Server-side TLS config:**
```go
tlsCfg := &tls.Config{
    MinVersion:   tls.VersionTLS13,
    ClientAuth:   tls.RequireAndVerifyClientCert,
    ClientCAs:    trustBundlePool,       // x509.CertPool from SPIRE trust bundle
    Certificates: []tls.Certificate{svid},
    NextProtos:   []string{"hyperspace/1"},
}
```

**Client-side TLS config:**
```go
tlsCfg := &tls.Config{
    MinVersion:   tls.VersionTLS13,
    RootCAs:      trustBundlePool,       // x509.CertPool from SPIRE trust bundle
    Certificates: []tls.Certificate{svid},
    NextProtos:   []string{"hyperspace/1"},
}
```

**`RequireAndVerifyClientCert`:** This mode requires the client to present a certificate AND verifies it against `ClientCAs`. If the client does not present a certificate, or the certificate is not signed by a CA in `ClientCAs`, the TLS handshake fails. This is the correct mode for service-to-service mTLS — not `RequireAnyClientCert` (which only requires a cert but does not verify it).

**CA pool construction:** The `x509.CertPool` is built from the SPIRE trust bundle returned by `SPIFFEIdentity.FetchSVID`. It is refreshed on every SVID rotation watch event. Both server and client use the same trust bundle (same trust domain = mutual trust).

**ALPN:** `NextProtos: []string{"hyperspace/1"}` must be set on both client and server `tls.Config`. If the ALPN protocol strings do not match, the TLS handshake fails with an `alert: no application protocol` error. This is a known gotcha — see `shared_knowledge.md` entry 4.

---

## mmap File Permissions

All mmap region files created by Hyperspace must use permission `0600` (owner read/write only).

**Why:** Log buffer and CNC files contain application data and command structs in shared memory. A world-readable file (0644) allows any process on the host to read raw message payloads or inject commands into the CNC ring buffer. This is a local privilege escalation vector.

**Where enforced:**
- `pkg/ipc/memmap.Map` passes the `perm os.FileMode` parameter directly to `os.OpenFile`. All callers are required to pass `0600`.
- `pkg/logbuffer.NewLogBuffer` uses `os.OpenFile(..., 0600)` explicitly — the permission is not parameterised.
- `TestMmap_FilePermissions` and `TestLogBuffer_FilePermissions` verify the created files have mode `0600` using `os.Stat().Mode().Perm()`.

**Failure mode:** The Security Evaluator will issue a FAIL verdict on any PR that creates mmap files with permissions other than `0600`. This is non-waivable.

---

## API Key Storage Pattern

Hyperspace does not store API keys directly. For reference, the darkmatter API key pattern (used by internal tooling that wraps Hyperspace) stores credentials as:

- SHA-256 hash of the key (hex-encoded) in the configuration store — never the plaintext key
- The plaintext key is delivered to the service at runtime via AWS Secrets Manager and held in memory only
- Comparison uses `crypto/subtle.ConstantTimeCompare` on the hash — never `==` or `strings.EqualFold`
- Keys rotate by inserting a new hash in the store before invalidating the old one (zero-downtime rotation)

This pattern is not directly implemented in Hyperspace v1 (Hyperspace uses SPIFFE/SPIRE, not API keys) but is documented here for agents implementing adjacent services that interact with Hyperspace.

---

## Go Security Standards

**`gosec` gate:** `gosec ./...` must pass with zero high-severity findings before any PR merges. The following `gosec` rules are most relevant to Hyperspace:

| Rule | Description | Hyperspace Relevance |
|---|---|---|
| G101 | Hardcoded credentials | All packages — no hardcoded secrets |
| G107 | URL from variable in HTTP request | `pkg/discovery`, `pkg/config` — AWS endpoints |
| G115 | Integer overflow conversion | Frame length parsing — `int32` to `int` |
| G304 | File path from variable | `pkg/ipc/memmap` — mmap file path |
| G402 | TLS MinVersion too low | `pkg/transport/quic` — must be TLS 1.3 |
| G501 | Blocklisted hash algorithm | `pkg/identity` — SHA-256 only; no MD5/SHA-1 |

**No shell subprocess:** Hyperspace must not invoke `exec.Command` or `os/exec` in any production code path. `gosec` G204 flags this. Any subprocess invocation requires an ADR and Security Evaluator PASS.

**No `unsafe` outside permitted packages:** `unsafe.Pointer` is used in `internal/atomic` (AtomicBuffer) and nowhere else. Any new `unsafe` use requires Architecture Evaluator approval.

---

## Frame Length Validation

**Risk:** If `frameLength` from a received QUIC frame or log buffer header is not validated before slicing the underlying buffer, a malformed or malicious frame can cause an out-of-bounds read, which in Go produces a runtime panic (`index out of range`). This is not exploitable for memory corruption (Go is memory-safe) but can cause a denial of service by crashing hsd.

**Where validated:**
- `pkg/logbuffer.Reader.Poll`: validates `frameLength > 0 && frameLength <= maxFrameLength` before slicing the term buffer
- `pkg/transport/quic` receive path: validates the frame type byte and `frameLength` against the connection's MTU before constructing a `Frame` struct
- `pkg/transport/probes.DecodePong`: validates `len(buf) >= MinPongFrameSize` before parsing fields

**Maximum frame size:** `maxFrameLength = termLength - FrameHeaderSize`. Frames larger than a single term are not supported in v1 (out of scope per SPEC Section 6).

**Failure mode:** A PR that reads from a slice without prior length validation is a P0 defect. The Code Evaluator checks for this pattern using `gosec` G115 and manual inspection. The Security Evaluator will FAIL any sprint that introduces an unvalidated frame read.

---

## Dependency Scanning

**`govulncheck ./...`** runs in CI before every build artifact is published. It checks all transitive dependencies against the Go vulnerability database (vuln.go.dev). A known vulnerability in any transitive dependency blocks the deploy gate.

**`go.sum` integrity:** All dependencies are pinned in `go.mod` and their hashes are in `go.sum`. `go mod verify` runs in CI to detect tampering.

**Key dependency versions (as of 2026-04-18):**

| Package | Version | Notes |
|---|---|---|
| `github.com/quic-go/quic-go` | v0.59.0 | QUIC transport; monitor for security advisories |
| `github.com/spiffe/go-spiffe/v2` | v2.6.0 | SPIFFE workload API; monitor SPIRE CVEs |
| `github.com/yalue/onnxruntime_go` | v1.27.0 | CGO; pinned; ONNX Runtime C library on AMI must match |
| `github.com/aws/aws-sdk-go-v2` | v1.41.6 | AWS SDK; monitor for IAM/STS CVEs |

---

## References

- [RFC 9000](https://www.rfc-editor.org/rfc/rfc9000) — QUIC Transport Protocol
- [RFC 8446](https://www.rfc-editor.org/rfc/rfc8446) — TLS 1.3
- [SPIFFE specification](https://spiffe.io/docs/latest/spiffe-about/spiffe-concepts/) — Workload identity concepts
- [Go security policy](https://go.dev/security/policy) — How Go handles vulnerability disclosure
- [OWASP Cryptographic Failures](https://owasp.org/Top10/A02_2021-Cryptographic_Failures/) — Relevant to TLS configuration and key storage
- `gosec` rules: https://github.com/securego/gosec#available-rules
