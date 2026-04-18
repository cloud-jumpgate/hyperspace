# Decision Log — Hyperspace

> All Architecture Decision Records (ADRs) for Hyperspace are recorded here in chronological order. Each ADR is immutable once accepted — amendments create new ADRs that supersede previous ones. The Architecture Evaluator checks this log at every evaluation.

---

## ADR Index

| ADR | Title | Status | Date | Author |
|---|---|---|---|---|
| ADR-001 | Multi-QUIC connection pool instead of single connection | Accepted | 2026-04-17 | Harness Architect |
| ADR-002 | QUIC as transport instead of Aeron reliable UDP | Accepted | 2026-04-17 | Harness Architect |
| ADR-003 | DRL congestion control via ONNX Runtime with CGO | Accepted | 2026-04-17 | Harness Architect |
| ADR-004 | SPIFFE/SPIRE for workload identity (full integration) | Accepted | 2026-04-17 | Harness Architect |
| ADR-005 | Embedded driver mode for testing | Accepted | 2026-04-17 | Harness Architect |
| ADR-006 | Real AWS integrations from day one | Accepted | 2026-04-17 | Harness Architect |
| ADR-007 | DRL fallback algorithm: BBRv3, not CUBIC | Accepted | 2026-04-18 | CTO |
| ADR-008 | SVID lifecycle: WatchX509Context for continuous rotation | Accepted | 2026-04-18 | CTO |
| ADR-009 | Send failure handling: retry with back-pressure propagation | Accepted | 2026-04-18 | CTO |
| ADR-010 | Inbound TLS and SPIFFE ID validation on Accept | Accepted | 2026-04-18 | CTO |
| ADR-011 | Frame header canonical size: 32 bytes | Accepted | 2026-04-18 | CTO |
| ADR-012 | TLS MinVersion=0 must be rejected explicitly | Accepted | 2026-04-18 | CTO |

---

### ADR-001: Multi-QUIC Connection Pool Instead of Single Connection

**Status:** Accepted
**Date:** 2026-04-17
**Author:** Harness Architect
**Supersedes:** —

#### Context

A single QUIC connection between two peers gives one congestion control loop and one path through the network. AWS EC2 c7gn instances in the same placement group have multiple equal-cost multipath (ECMP) routes through the spine fabric. A single connection cannot exploit ECMP — all traffic uses one path regardless of which physical port is elected by ECMP hashing. At 50 Gbps line rate, a single BBR or CUBIC congestion window can also become the bottleneck if the congestion window underestimates available bandwidth during bandwidth probing phases.

Aeron addresses this with its own publication/subscription model where multiple channels can be opened to the same peer, but Aeron's transport is not QUIC-based and each connection carries its own protocol overhead. Hyperspace needs to deliver path diversity without duplicating the protocol stack.

#### Decision

Hyperspace maintains N concurrent QUIC connections per peer pair (default N=4, configurable via `pool=N` in the channel URI). Each connection is an independent QUIC session with its own congestion control loop, flow control window, and ECMP hash bucket. The Arbitrator selects which connection to use per send batch. The Adaptive Pool Learner (F-007) observes the latency-loss correlation across pool connections and adjusts N over time.

#### Consequences

**Positive:**
- Exploits ECMP path diversity on EC2 without kernel bonding
- One congestion-impaired connection does not block others
- Independent flow control windows prevent head-of-line blocking at the pool level

**Negative (accepted trade-offs):**
- N connections multiply TLS handshake overhead at startup
- Pool Manager (F-009) is a significant implementation component
- File descriptor count scales with pool size × peer count

**Neutral:**
- Pool size can be tuned to 1 for deployments where ECMP is not relevant (e.g., co-located services on the same host)

#### Alternatives Considered

| Alternative | Why Rejected |
|---|---|
| Single QUIC connection with multiple streams | Cannot exploit ECMP; one congestion window limits peak throughput |
| TCP bonding (MPTCP) | TCP framing overhead unacceptable; loses QUIC features (0-RTT, connection migration) |
| Custom UDP multipath | Full protocol implementation burden; equivalent to building QUIC again |
| Single connection, no pool | Simplest; rejected because ECMP utilisation is a hard design requirement for 50 Gbps targets |

#### Review Trigger

Re-evaluate if: (a) quic-go adds native multipath support per RFC 9000 extensions; (b) EC2 networking architecture changes to eliminate ECMP within AZ.

---

### ADR-002: QUIC as Transport Layer Instead of Aeron Reliable UDP

**Status:** Accepted
**Date:** 2026-04-17
**Author:** Harness Architect
**Supersedes:** —

#### Context

Aeron's network transport is a custom reliable UDP protocol (Aeron Protocol Specification). It is highly optimised for Aeron's specific patterns (streaming, SBE encoding) but requires implementing all of: retransmission, flow control, session management, NAK handling, and heartbeating from scratch. Hyperspace adopts Aeron's shared-memory client/driver architecture and log buffer design but must make an independent transport choice.

TLS 1.3 is a non-negotiable requirement for Hyperspace (SPIFFE/SPIRE workload identity delivers X.509 SVIDs; mTLS enforces trust domain membership). Aeron's protocol has no built-in TLS. Adding TLS on top of Aeron's custom UDP would be building QUIC without QUIC's RFC testing and community maintenance.

quic-go is a pure-Go implementation of IETF RFC 9000 (QUIC) with production deployment at scale (Cloudflare, etc.), active maintenance, and a stable API. It handles retransmission, flow control, connection migration, 0-RTT handshake, and TLS 1.3 natively.

#### Decision

Use QUIC (via quic-go at the version pinned in `go.mod`) as the sole transport layer for all inter-host messaging. The quic-go library is wrapped behind Hyperspace's `pkg/transport/quic.Connection` interface so that the rest of the system never imports quic-go types directly.

#### Consequences

**Positive:**
- TLS 1.3 built-in — no separate crypto layer
- Connection migration — cert rotation does not require new address negotiation
- 0-RTT handshake — reconnect latency minimised for known peers
- RFC 9000 compliance — battle-tested retransmit and flow control
- No custom protocol to maintain

**Negative (accepted trade-offs):**
- quic-go per-packet overhead vs raw UDP (estimated 5–15 µs additional RTT vs raw UDP in benchmarks)
- Go GC pressure from quic-go's internal allocations at very high packet rates
- quic-go version pinning required — API changes in major versions require migration

**Neutral:**
- UDP is still the underlying transport; hardware offload (DPDK, io_uring) remains possible in future via a quic-go transport replacement

#### Alternatives Considered

| Alternative | Why Rejected |
|---|---|
| Aeron Protocol (custom UDP) | No TLS; full implementation of retransmit/CC/flow control required; maintenance burden |
| gRPC / HTTP/2 | HTTP framing overhead; not designed for tight pub/sub loops at µs latency |
| Raw UDP + custom framing | Equivalent to writing QUIC from scratch; rejected for same reasons as Aeron Protocol |
| DTLS over UDP | Separate congestion control needed; more complex than QUIC |

#### Review Trigger

Re-evaluate if: (a) quic-go performance measurements show > 50 µs median overhead vs raw UDP at 1 M msg/s target; (b) quic-go is abandoned or unmaintained for > 12 months.

---

### ADR-003: DRL Congestion Control via ONNX Runtime with CGO

**Status:** Accepted
**Date:** 2026-04-17
**Author:** Harness Architect
**Supersedes:** —

#### Context

CUBIC and BBR are proven static algorithms. At 1 million messages per second across diverse network conditions (same-AZ, cross-AZ, varying cross-traffic), static algorithms cannot adapt to per-path optima that change over minutes. A deep reinforcement learning (DRL) controller trained offline can learn that, for example, this specific pair of EC2 instances and this network path, optimal throughput requires a narrower congestion window than BBR would estimate — and adapt in real time.

ONNX (Open Neural Network Exchange) is the industry standard for deploying trained neural network models in production inference environments. It supports models trained in PyTorch, TensorFlow, scikit-learn, and others. ONNX Runtime provides a C API with official Go bindings (CGO-based). The inference latency for a small policy network (4 inputs, 1 output, 2 hidden layers) is < 50 µs — well within the per-send-batch budget.

#### Decision

Implement a `DRLController` in `pkg/cc/drl` that wraps ONNX Runtime via CGO. The controller observes a state vector `[rtt_ms float32, loss_rate float32, cwnd_bytes float32, outstanding_bytes float32]` and outputs `cwnd_delta float32`. The ONNX model is loaded from AWS Secrets Manager at daemon startup (verified by SHA-256 hash). A BBRv3 fallback is automatic if the model fails to load, fails to parse, or if inference errors exceed 5 in a 60-second window.

CGO is restricted to `pkg/cc/drl` only — no other package may use CGO without an ADR.

#### Consequences

**Positive:**
- Per-path learned optima unreachable by static algorithms
- Adapts continuously without operator tuning
- ONNX Runtime is vendor-neutral — training can use any framework

**Negative (accepted trade-offs):**
- CGO build dependency — cross-compilation requires ONNX Runtime shared library for the target architecture
- ONNX Runtime must be present on EC2 AMI — added to AMI build process
- Model training pipeline is a separate system (out of scope for Hyperspace v1)

**Neutral:**
- DRL is a pluggable `CongestionController` interface — using `cc=bbrv3` in the URI bypasses DRL entirely

#### Alternatives Considered

| Alternative | Why Rejected |
|---|---|
| Pure Go inference (custom NN) | Significant ML infrastructure to build; not ONNX-compatible | 
| TensorFlow Lite (Go binding) | CGO binding more complex; larger binary; ONNX has better ecosystem |
| Static BBRv3 only | Differentiating capability lost; no adaptation to per-path conditions |
| Python sidecar for inference | IPC overhead exceeds inference time benefit; operational complexity |

#### Review Trigger

Re-evaluate if: (a) ONNX Runtime Go binding becomes incompatible with Go 1.26+ CGO; (b) A pure-Go ONNX inference library reaches production maturity.

---

### ADR-004: SPIFFE/SPIRE for Workload Identity (Full Integration)

**Status:** Accepted
**Date:** 2026-04-17
**Author:** Harness Architect
**Supersedes:** —

#### Context

All Hyperspace QUIC connections require mTLS. The question is how TLS certificates are issued and rotated. Options considered:

1. **Static TLS certificates:** Generated once, stored in a file or Secrets Manager. Rotation requires operator action (or scripted automation). Certificate expiry is a production risk.
2. **AWS ACM Private CA:** AWS-managed CA that issues certificates on request. IAM-controlled. Certificates are valid for the ACM-configured duration (minimum 1 day). API latency on issuance.
3. **SPIFFE/SPIRE:** CNCF-standard workload identity framework. SPIRE Server issues short-lived X.509 SVIDs (1h TTL by default). SPIRE Agent on each node delivers SVIDs via a Unix socket workload API. The go-spiffe library provides a Watch API that delivers new SVIDs before expiry.

SPIFFE is the standard for workload identity in cloud-native environments. It is cloud-agnostic (works on EC2, EKS, GKE, bare metal). It integrates with Kubernetes, AWS EC2 instance identity, and other attestors. The short SVID TTL means a compromised SVID is automatically invalidated within 1 hour without operator action.

#### Decision

Integrate SPIFFE/SPIRE from day one. `pkg/identity` fetches X.509 SVIDs from the SPIRE Agent's Unix socket workload API using the go-spiffe library. Pool Manager watches the SPIRE workload API for SVID rotation events and triggers cert rotation on affected QUIC connections (new connection on new cert, drain old, close old). No self-signed certificates in production.

For local development, a conformant SPIRE stub (go-spiffe test utilities) is used in tests. Production requires a deployed SPIRE Server and SPIRE Agent.

#### Consequences

**Positive:**
- Zero-touch cert rotation — no operator action required for routine cert lifecycle
- Trust domain enforced at TLS layer — peers outside the trust domain are rejected at handshake
- SPIFFE is cloud-agnostic and portable
- Short SVID TTL limits blast radius of credential compromise

**Negative (accepted trade-offs):**
- SPIRE infrastructure required in every non-development environment
- SPIRE Agent must be running and healthy on each EC2 instance
- Local development requires SPIRE stub or skip-TLS mode (dev-only, never production)

**Neutral:**
- SPIFFE trust domain is a configuration value — can be changed without code changes

#### Alternatives Considered

| Alternative | Why Rejected |
|---|---|
| Static TLS certs | Manual rotation; expiry risk; operationally fragile at scale |
| AWS ACM Private CA | ACM API latency; per-cert cost; less portable than SPIFFE |
| Stub identity (forever) | Never becomes production-ready; violates ADR-006 (real integrations) |
| Let's Encrypt (ACME) | Not designed for service-to-service mTLS at millisecond rotation speeds |

#### Review Trigger

Re-evaluate if: (a) SPIRE adoption significantly declines and an alternative becomes the CNCF standard; (b) AWS introduces a native workload identity API with equivalent TTL and watch semantics.

---

### ADR-005: Embedded Driver Mode for Testing

**Status:** Accepted
**Date:** 2026-04-17
**Author:** Harness Architect
**Supersedes:** —

#### Context

The standard Hyperspace deployment runs `hsd` as a separate OS process. The client library communicates with `hsd` via mmap'd ring buffers. For integration testing of `pkg/client`, `pkg/driver`, and the end-to-end message flow, an out-of-process `hsd` requires: (a) spawning a subprocess, (b) waiting for the CNC heartbeat to become live, (c) managing cleanup on test failure. This adds 200–500 ms of setup time per test and introduces port conflicts when tests run in parallel.

#### Decision

Implement an `embedded.Driver` type in `pkg/driver` that starts all five driver agents (Conductor, Sender, Receiver, Path Manager, Pool Manager) as goroutines within the test process. In embedded mode, client-to-driver IPC uses in-process channels instead of mmap'd ring buffers where safe to do so. The `embedded.Driver` satisfies the same `Driver` interface used by production code.

Embedded mode is permitted only in test code — `go build` without the `embed` build tag must not include `embedded.Driver`. Scenario tests for `hsd` binary correctness still run against the real process in CI.

#### Consequences

**Positive:**
- Fast, hermetic integration tests (< 5 ms setup vs 300 ms for subprocess)
- No port conflicts when tests run in parallel (`-parallel N`)
- Goroutine leak detection with `goleak` works naturally in-process

**Negative (accepted trade-offs):**
- Embedded mode uses in-process channels at the IPC boundary; subtle differences from mmap path could mask bugs
- Keeping embedded and production IPC paths in sync requires discipline

**Neutral:**
- Embedded mode is additive — removing it does not affect production binary

#### Alternatives Considered

| Alternative | Why Rejected |
|---|---|
| Test against real hsd subprocess only | Slow; port conflicts; 300 ms setup per test; fragile in parallel |
| Mock transport layer | Fast but does not exercise real driver logic |
| Docker Compose hsd | Even slower; requires Docker daemon in CI |

#### Review Trigger

Re-evaluate if: more than 3 bugs are found that pass embedded tests but fail against real `hsd` — at that point, the embedded-production divergence is too large.

---

### ADR-006: Real AWS Integrations from Day One

**Status:** Accepted
**Date:** 2026-04-17
**Author:** Harness Architect
**Supersedes:** —

#### Context

A common pattern in platform engineering is to stub AWS integrations behind Go interfaces during development and "wire in the real thing" before production. This pattern consistently produces late-stage integration failures: IAM permission errors, API parameter format mismatches, unexpected Secrets Manager rotation behaviour, Cloud Map namespace naming conflicts. These bugs are expensive to fix because they are discovered during production preparation, not during feature development.

The Engineering Department's Build Mandate requires working, tested code — not stubs. Localstack provides AWS-compatible API endpoints for Cloud Map, SSM, Secrets Manager, and CloudWatch that run locally and in CI, enabling real AWS SDK code paths to be tested without an AWS account.

#### Decision

All AWS integrations (`pkg/discovery`, `pkg/config`, `pkg/identity` Secrets Manager path) use real AWS SDK v2 calls from the moment they are written (Sprint S9). In earlier sprints, these packages do not exist yet — when they are created, they are created against real APIs. Localstack runs in CI via Docker Compose. The `AWS_ENDPOINT_URL` environment variable overrides the AWS API endpoint for Localstack in tests.

No interface stubs that return hardcoded values are accepted as production code. Test doubles that call Localstack are acceptable.

#### Consequences

**Positive:**
- Integration bugs found in CI, not in production preparation
- IAM policy testing via Localstack validates real permission requirements
- No stub-to-real migration step

**Negative (accepted trade-offs):**
- CI requires Docker for Localstack
- Additional CI setup time (Localstack startup ~10s)
- Localstack fidelity is not 100% — some AWS edge cases may not be reproduced

**Neutral:**
- This is a policy decision — no code changes are implied until S9

#### Alternatives Considered

| Alternative | Why Rejected |
|---|---|
| Interface stubs with hardcoded returns | Integration bugs found late; false confidence in test suite |
| AWS SDK with mock client generated by mockery | Mocks drift from real API behaviour over time |
| Real AWS account in CI | Cost; permission management complexity; S3/SSM state accumulates |

#### Review Trigger

Re-evaluate if: Localstack is discontinued or falls significantly behind AWS API parity (> 6 months behind on a used API).

---

### ADR-007: DRL Fallback Algorithm — BBRv3, Not CUBIC

**Status:** Accepted
**Date:** 2026-04-18
**Author:** CTO
**Supersedes:** Clarifies ADR-003

#### Context

ADR-003 specifies that the DRL congestion controller falls back to BBRv3 when ONNX inference fails or times out. The Architecture Evaluator (finding F-001) discovered that the implementation falls back to CUBIC instead. The `DRLController.runPolicy` method delegates to `d.fallback.CongestionWindow()` where `fallback` is a `cubicWrapper` wrapping `cubic.New(...)`.

This is an ADR conformance violation. BBRv3 was chosen as the fallback because it shares DRL's bandwidth-probing philosophy — a CUBIC fallback produces fundamentally different congestion window dynamics (loss-based vs model-based), creating unpredictable behaviour transitions.

#### Decision

Fix the DRL fallback to use BBRv3. Replace the `cubicWrapper` with a BBRv3 instance created via `cc.New("bbrv3", initialCwnd, minRTT)`. When inference fails or exceeds the 5 us deadline, the DRL controller delegates to BBRv3's congestion window. All DRL event callbacks (OnPacketSent, OnPacketAcked, OnPacketLost, OnRTTUpdate) must also be forwarded to the BBRv3 fallback to keep its state current.

#### Consequences

- Fallback behaviour is now consistent with the DRL controller's bandwidth-probing design
- BBRv3 fallback will produce slightly different window dynamics than the previous CUBIC fallback during inference timeout periods
- No API changes — the `DRLController` still implements `cc.CongestionControl`

#### Review Trigger

Re-evaluate if DRL training results indicate CUBIC produces better fallback behaviour than BBRv3 in specific network conditions.

---

### ADR-008: SVID Lifecycle — WatchX509Context for Continuous Rotation

**Status:** Accepted
**Date:** 2026-04-18
**Author:** CTO
**Supersedes:** Clarifies ADR-004

#### Context

ADR-004 specifies Watch API semantics for SVID rotation. The Architecture Evaluator (finding F-002) discovered that `SPIFFESource` implements only `Fetch()` — a one-shot retrieval. In production, SPIRE issues SVIDs with short TTLs (typically 1 hour). Without Watch, all QUIC connections will fail after SVID expiry because the TLS credentials become invalid.

The go-spiffe library provides `workloadapi.WatchX509Context()` which continuously receives fresh SVIDs before expiry, enabling zero-downtime cert rotation.

#### Decision

Replace the one-shot `FetchX509Context` model with `WatchX509Context`. The `SPIFFESource` must:

1. Start a background goroutine calling `workloadapi.WatchX509Context(ctx, watcher)` where the watcher implements `workloadapi.X509ContextWatcher`
2. On each `OnX509ContextUpdate`, atomically update the stored `*tls.Config` via `sync/atomic.Pointer[tls.Config]`
3. Expose `TLSConfig() *tls.Config` that always returns the latest live config
4. Stop cleanly when the context is cancelled

The `Fetch()` method is retained for initial bootstrap (blocking until the first SVID is received) but all subsequent rotations use the Watch path.

#### Consequences

- Zero-downtime cert rotation — Pool Manager reads fresh TLS config on every new connection
- Background goroutine must be lifecycle-managed (context cancellation, goleak verification)
- Atomic pointer swap means consumers never hold a stale config reference

#### Review Trigger

Re-evaluate if go-spiffe deprecates `WatchX509Context` or introduces a superior rotation API.

---

### ADR-009: Send Failure Handling — Retry with Back-Pressure Propagation

**Status:** Accepted
**Date:** 2026-04-18
**Author:** CTO
**Supersedes:** —

#### Context

The Architecture Evaluator (finding F-003) identified that when `conn.Send()` returns an error, the Sender silently drops the frame. The frame has already been consumed from the log buffer (the reader advanced past it). There is no retry, no counter increment, and no signal to the publisher. This is a data-loss path.

Silent frame drop is unacceptable for a messaging platform that claims reliable delivery. The design must acknowledge that QUIC send failures happen (congestion, connection closure, stream limits) and handle them explicitly.

#### Decision

When `conn.Send(data)` returns an error:

1. **Retry on the next connection** in the gathered pool, up to 3 attempts (round-robin across available connections)
2. **If all 3 attempts fail**: increment `CtrLostFrames` counter and log at `slog.Error` with connection IDs tried
3. **Back-pressure propagation**: after 3 consecutive failed frames for a publication, signal back-pressure via the `ErrBackPressure` return code to the publication's next `Offer()` call. Use a per-publication failure counter, reset on success.

This approach prioritises delivery over latency: a single-connection failure is retried transparently, but persistent failures are surfaced to the publisher so it can take application-level action (slow down, buffer, alert).

#### Consequences

- Frame delivery reliability increases — single-connection failures are transparent
- `CtrLostFrames` counter provides operational visibility into persistent failures
- Back-pressure propagation gives publishers a signal to slow down
- Additional complexity in the Sender's hot path (retry loop, failure counter) — bounded to 3 attempts

#### Review Trigger

Re-evaluate if the retry loop introduces measurable latency on the happy path (> 1 us per frame when no failure occurs).

---

### ADR-010: Inbound TLS and SPIFFE ID Validation on Accept

**Status:** Accepted
**Date:** 2026-04-18
**Author:** CTO
**Supersedes:** —

#### Context

The Architecture Evaluator (finding F-004) identified that `Accept()` wraps an incoming `quic.Conn` without validating TLS state. It does not check `HandshakeComplete`, peer certificate presence, or SPIFFE ID. If the QUIC listener's TLS config is misconfigured (or a future code change weakens it), anonymous connections could be accepted.

Defence in depth requires validating the security state at every trust boundary, not relying solely on upstream configuration being correct.

#### Decision

After `listener.Accept(ctx)` returns a `quic.Connection`:

1. Verify `conn.ConnectionState().TLS.HandshakeComplete == true` — if false, close and return error
2. Verify `conn.ConnectionState().TLS.PeerCertificates` is non-empty — if empty, close and return error
3. If `RequireSPIFFE` is true (configurable in `QUICConfig`), verify that at least one peer certificate contains a SPIFFE URI SAN (`spiffe://` scheme in `Certificate.URIs`)
4. Reject connections that fail validation with a descriptive error

`RequireSPIFFE` defaults to `false` for backward compatibility. Production deployments must set it to `true`.

#### Consequences

- Defence-in-depth TLS validation on inbound connections
- SPIFFE ID validation ensures only trusted workloads can connect
- `RequireSPIFFE` configuration flag allows gradual rollout
- Slight increase in Accept() latency (certificate parsing) — negligible compared to TLS handshake

#### Review Trigger

Re-evaluate if SPIFFE trust domain federation requires accepting SVIDs from multiple trust domains.

---

### ADR-011: Frame Header Canonical Size — 32 Bytes

**Status:** Accepted
**Date:** 2026-04-18
**Author:** CTO
**Supersedes:** SPEC.md F-001 frame header specification

#### Context

SPEC.md F-001 specifies a 24-byte frame header. The implementation uses 32 bytes, adding `version` (1 byte), expanding `type` from `uint8` to `uint16`, and including `termOffset` (4 bytes) and `termID` (4 bytes). The original spec had a `reservedValue int64` (8 bytes) that is retained.

The Architecture Evaluator (finding F-015) notes that the additional fields are required for correct receiver operation (the receiver needs `termOffset` to write at the correct position in the image buffer) and protocol versioning.

#### Decision

The 32-byte frame header is the canonical Hyperspace wire format:

| Field | Size | Type | Encoding |
|---|---|---|---|
| frame_length | 4 bytes | int32 | little-endian |
| version | 1 byte | uint8 | — |
| flags | 1 byte | uint8 | BEGIN=0x80, END=0x40 |
| frame_type | 2 bytes | uint16 | little-endian |
| term_offset | 4 bytes | int32 | little-endian |
| session_id | 4 bytes | int32 | little-endian |
| stream_id | 4 bytes | int32 | little-endian |
| term_id | 4 bytes | int32 | little-endian |
| reserved_val | 8 bytes | int64 | little-endian |

SPEC.md must be updated to reflect this canonical layout. The `reserved_val` field is reserved for future extensions (e.g., sequence number for ordered delivery, encryption nonce).

#### Consequences

- SPEC.md and implementation are now consistent
- 32-byte header is 8 bytes larger than original spec — negligible overhead at 1M msg/s
- `reserved_val` provides extension point without wire format change

#### Review Trigger

Re-evaluate when the reserved field is allocated for a specific extension.

---

### ADR-012: TLS MinVersion=0 Must Be Rejected Explicitly

**Status:** Accepted
**Date:** 2026-04-18
**Author:** CTO
**Supersedes:** —

#### Context

The Architecture Evaluator (finding F-005) identified that `validateTLSConfig()` in `pkg/transport/quic/conn.go` accepts `MinVersion == 0`. In Go's `crypto/tls` package, a zero `MinVersion` defaults to TLS 1.2. While `enforceALPN()` always overrides `MinVersion` to TLS 1.3, the validation function should reject zero to prevent a future code path from bypassing `enforceALPN` and inadvertently allowing TLS 1.2.

Defence in depth: validation should catch configuration errors at the earliest possible point, not rely on a downstream function to fix them.

#### Decision

In `validateTLSConfig`, treat `MinVersion == 0` as a configuration error. The function must panic with a clear message: `"hyperspace: tls.Config.MinVersion must be set explicitly; zero value permits TLS 1.2"`. This check must come before the existing `MinVersion < tls.VersionTLS13` check.

A panic (not an error return) is appropriate because this is a programmer error — a misconfigured `tls.Config` should never reach production, and a panic makes the error impossible to ignore during development and testing.

#### Consequences

- Zero-value `MinVersion` is caught at creation time, not silently overridden
- Existing tests that use `MinVersion: 0` must be updated to set `MinVersion: tls.VersionTLS13`
- Defence-in-depth: no code path can bypass TLS 1.3 enforcement

#### Review Trigger

Re-evaluate if Go changes the default TLS minimum version to 1.3 or higher.
