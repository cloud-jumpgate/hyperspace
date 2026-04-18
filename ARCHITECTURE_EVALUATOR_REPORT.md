# Architecture Evaluator Report -- Hyperspace

**Date:** 2026-04-18
**Evaluator:** Architecture Evaluator
**Sprints covered:** S1--S13
**Verdict:** CONDITIONAL PASS

---

## 1. Executive Summary

Hyperspace is a substantial Go-native pub/sub messaging platform with 100 Go source files across 34 testable packages. All packages compile cleanly (`go build ./...` and `go vet ./...` produce zero errors) and all 34 test suites pass under the race detector with zero data races. The architecture faithfully follows the Aeron-inspired shared-memory client/driver separation pattern specified in `SYSTEM_ARCHITECTURE.md`, with QUIC replacing Aeron's reliable UDP as decided in ADR-002.

After reviewing every primary source file across all packages, I find the architecture to be **sound in its core design** with strong adherence to the specified ADRs. The concurrency model is correctly implemented (lock-free log buffers, atomic conductor snapshots, single-writer invariants). The interface abstraction layers (transport.Connection, CongestionControl, discovery.Provider, secrets.Provider) are well-designed and provide the right seams for testability and future evolution.

However, I identify **4 critical findings, 5 high findings, and 7 medium findings** that must be addressed. The most significant architectural gap is the absence of SVID rotation watching (ADR-004 specifies Watch semantics, but only single-fetch is implemented). The CI pipeline failure (golangci-lint) is a governance concern rather than an architectural one, but it blocks the quality gate.

**Verdict: CONDITIONAL PASS.** The architecture is fit for continued development. The 4 critical findings and 5 high findings must be resolved before production deployment. No architectural redesign is required.

---

## 2. ADR Conformance (ADR-001 to ADR-006)

| ADR | Decision | Implementation | Conformant? | Notes |
|-----|----------|----------------|-------------|-------|
| ADR-001 | Multi-QUIC pool: N connections per peer, configurable via `pool=N` in channel URI, Arbitrator selects per batch, Adaptive Pool Learner adjusts N | `pkg/transport/pool` manages N connections with min/max bounds. `pkg/channel` parses `pool=N` from URI. Arbitrator implements 5 strategies. Adaptive Pool Learner evaluates snapshots and recommends Add/Remove/Hold. | **YES** | Pool does not explicitly bind different source ports for ECMP hash diversity -- it relies on OS ephemeral port assignment, which is architecturally correct for QUIC over UDP. |
| ADR-002 | QUIC via quic-go as sole transport; TLS 1.3 mandatory; ALPN `hyperspace/1`; quic-go types never exported beyond `pkg/transport/quic` | `pkg/transport/quic` wraps quic-go behind `Connection` interface. `enforceALPN()` adds ALPN and sets `MinVersion: tls.VersionTLS13`. `validateTLSConfig()` rejects configs below TLS 1.3. No quic-go types leak outside the package. | **YES** | `validateTLSConfig` allows `MinVersion == 0` (which Go defaults to TLS 1.2). However, `enforceALPN` always overrides to TLS 1.3, so the effective enforcement is correct. See finding F-005. |
| ADR-003 | DRL via ONNX Runtime with CGO; `//go:build onnx` gate; NullPolicy fallback; BBRv3 fallback on inference failure | `pkg/cc/drl/onnx.go` has `//go:build onnx`. `NullPolicy` is the default (returns 0 = no cwnd change). `DRLController.runPolicy` falls back to CUBIC window on inference error (not BBRv3 as specified). DRL has a loss-spike safety rail. | **PARTIAL** | Fallback on inference failure goes to CUBIC (embedded), not BBRv3 as ADR-003 specifies. See finding F-001. |
| ADR-004 | SPIFFE/SPIRE via go-spiffe; Unix socket workload API; socket path configurable; Watch API for SVID rotation; Pool Manager triggers cert rotation on Watch events | `pkg/identity` implements `SPIFFESource.Fetch()` for single SVID fetch. Socket path configurable. TLS 1.3 + mTLS enforced in the assembled config. | **PARTIAL** | **No Watch/rotation implementation exists.** `SPIFFESource` has only `Fetch()`, not `Watch(ctx, onChange)` as specified in F-014 acceptance criteria. Pool Manager has no SVID rotation detection. See finding F-002. |
| ADR-005 | Embedded driver mode for testing; goroutines instead of separate process; in-process IPC; same Driver interface | `driver.NewEmbedded()` starts Conductor/Sender/Receiver as goroutines with in-process byte slices (no mmap). `client.NewEmbedded()` creates the full stack. ThreadingMode supports Dedicated/Dense/Shared. | **YES** | Integration tests in `tests/integration/` use embedded mode. Context cancellation is honoured. |
| ADR-006 | Real AWS integrations from day one; no hardcoded stubs; Localstack in CI | `pkg/discovery/cloudmap` uses real AWS SDK v2 with injected `DiscoverInstancesAPI` interface. `pkg/config/ssm` uses real AWS SDK v2 with injected `GetParameterAPI`. `pkg/secrets/secretsmanager` uses real AWS SDK v2 with injected interface. All accept `AWS_ENDPOINT_URL` for Localstack. | **YES** | Interfaces are defined for testability, but the implementations call real AWS SDK paths. This matches the ADR intent. |

---

## 3. Package Structure Conformance

| Package (Spec) | Actual | Status |
|-----------------|--------|--------|
| `cmd/hsd/` | `cmd/hsd/main.go` | PRESENT |
| `cmd/hyperspace-stat/` | `cmd/hyperspace-stat/main.go` | PRESENT |
| `cmd/hyperspace-probe/` | `cmd/hyperspace-probe/` (empty) | **STUB ONLY** -- no main.go |
| `pkg/client/` | Present, 4 files | PRESENT |
| `pkg/driver/conductor/` | Present | PRESENT |
| `pkg/driver/sender/` | Present | PRESENT |
| `pkg/driver/receiver/` | Present | PRESENT |
| `pkg/driver/pathmgr/` | Present | PRESENT |
| `pkg/driver/poolmgr/` | Present | PRESENT |
| `pkg/transport/quic/` | Present | PRESENT |
| `pkg/transport/arbitrator/` | Present | PRESENT |
| `pkg/transport/probes/` | Present | PRESENT |
| `pkg/transport/pool/` | Present | PRESENT |
| `pkg/cc/` (api, adapter) | Present | PRESENT |
| `pkg/cc/cubic/` | Present | PRESENT |
| `pkg/cc/bbr/` | Present | PRESENT |
| `pkg/cc/bbrv3/` | Present | PRESENT |
| `pkg/cc/drl/` | Present (+ onnx.go) | PRESENT |
| `pkg/logbuffer/` | Present, 4 files | PRESENT |
| `pkg/channel/` | Present | PRESENT |
| `pkg/counters/` | Present | PRESENT |
| `pkg/ipc/ringbuffer/` | Present | PRESENT |
| `pkg/ipc/broadcast/` | Present | PRESENT |
| `pkg/ipc/memmap/` | Present | PRESENT |
| `pkg/discovery/` | Interface only at root | PRESENT |
| `pkg/config/` | Interface only at root | PRESENT |
| `pkg/identity/` | Present | PRESENT |
| `pkg/obs/` | **EMPTY DIRECTORY** | **MISSING** |
| `internal/atomic/` | Present | PRESENT |
| `examples/` | 7 example directories | PRESENT |

**Packages in code but not in spec:**

| Package | Notes |
|---------|-------|
| `pkg/otel/` | OTel metrics provider -- functionally replaces `pkg/obs/` but uses different name |
| `pkg/secrets/` | Secrets provider interface + file/secretsmanager implementations -- not listed in CLAUDE.md package table |
| `pkg/config/env/` | Environment variable config loader -- sub-package not in spec |
| `pkg/config/ssm/` | SSM Parameter Store loader -- sub-package not in spec |
| `pkg/discovery/cloudmap/` | Cloud Map implementation -- sub-package not in spec |
| `pkg/discovery/static/` | Static in-memory discovery -- sub-package not in spec |
| `pkg/secrets/file/` | File-based secrets loader |
| `pkg/secrets/secretsmanager/` | AWS Secrets Manager loader |
| `tests/integration/` | Integration test package |
| `benchmarks/` | Benchmark package |

**Assessment:** The actual package structure is a reasonable evolution of the specification. The sub-package pattern (e.g., `discovery/cloudmap`, `config/ssm`) provides better separation of interface from implementation than the flat structure in the spec. `pkg/obs/` should either be removed (empty) or `pkg/otel/` should be renamed to match the spec, or CLAUDE.md should be updated. `pkg/secrets/` is a valuable addition not originally specified.

---

## 4. Interface Design Assessment

| Interface | Location | Quality | Issues |
|-----------|----------|---------|--------|
| `transport.Connection` | `pkg/transport/quic/conn.go` | **Good** | Well-abstracted: ID, Send/Recv by channel (control/probe/data), RTT, Stats, Close, IsClosed. However, it is defined inside the `quictr` package rather than a separate `pkg/transport/` interface file. This means non-QUIC transport implementations would need to import the quic package to satisfy the interface. |
| `cc.CongestionControl` | `pkg/cc/api.go` | **Good** | Clean event-driven interface. OnPacketSent/Acked/Lost/RTTUpdate + state queries. The Factory pattern with registry is well-designed. |
| `cc.CCAdapter` | `pkg/cc/adapter.go` | **Adequate** | Thread-safe wrapper with mutex. Global adapter registry by connID is a code smell (global mutable state) but acceptable given the architecture. |
| `discovery.Provider` | `pkg/discovery/discovery.go` | **Good** | Minimal: Resolve + Close. Correct abstraction level. |
| `secrets.Provider` | `pkg/secrets/secrets.go` | **Good** | Minimal: LoadTLSConfig. Returns fully assembled `*tls.Config`. |
| `config.Loader` | `pkg/config/config.go` | **Good** | Minimal: Load returns `*DriverConfig`. |
| `driver.Agent` | `pkg/driver/agent.go` | **Good** | DoWork/Name/Close. Cooperatively scheduled. IdleStrategy is a clean companion interface. |
| `broadcast.Transmitter/Receiver` | `pkg/ipc/broadcast/broadcast.go` | **Good** | SPSC transmitter with lock-free receiver. Correct lapping detection. |
| `ringbuffer.OneToOne/ManyToOne` | `pkg/ipc/ringbuffer/ringbuffer.go` | **Good** | CAS-based MPSC. Correct padding on wrap. Record-length-last publishing protocol. |
| `arbitrator.Arbitrator` | `pkg/transport/arbitrator/arbitrator.go` | **Good** | Pick(candidates, pubID, bytes). Five implementations. StickyArbitrator correctly exports Remove for pin cleanup. |

**Key finding on Connection interface location:** The `Connection` interface is defined in `pkg/transport/quic/conn.go` (the implementation package). This couples all consumers to the QUIC adapter package at the type level, even though they only use the interface. For the `transport.Connection` abstraction to truly decouple the system from QUIC, the interface should be promoted to `pkg/transport/connection.go`. This was acknowledged in the S13 sprint contract as acceptable, but is an architectural concern for future transport alternatives.

---

## 5. Data Flow Analysis

### 5.1 Publish Path

`client.Publication.Offer(data)` -> `logbuffer.TermAppender.AppendUnfragmented(...)` -> `driver/sender.DoWork()` reads term via `TermReader.Read()` -> `arbitrator.Pick()` selects connection -> `conn.Send(streamID, frameBytes)` -> QUIC/UDP to remote peer

**Assessment: CORRECT with one gap.**

- The atomic CAS tail-claim in `TermAppender` is correctly implemented. Payload is written before the frame-length volatile store (correct publish ordering).
- The CAS term rotation in `Publication.Offer` (C-01 fix) is correct -- multiple goroutines can safely race on rotation.
- The Sender's term-aware position tracking (S-03 fix) correctly resets `termOffset` when the active partition changes.
- The `sync.Pool` frame buffer (P-01 fix) eliminates per-frame allocation.

**Gap:** Back-pressure from the QUIC layer is NOT propagated to the publisher. When `conn.Send()` returns an error, the Sender logs a warning and drops the frame. The frame has already been consumed from the log buffer (the reader advanced past it). There is no mechanism to retry the send or signal the publisher that data was lost. The `ErrBackPressure` error defined in `quictr` is never returned by `Send()` -- quic-go's stream write either succeeds or returns an I/O error. **This is a data-loss risk under QUIC congestion.** See finding F-003.

### 5.2 Subscribe Path

Remote peer -> `conn.RecvData(ctx)` -> `driver/receiver.DoWork()` -> `receiver.processFrame(data)` validates header and writes to image log buffer -> `client.Subscription.Poll(handler, limit)` -> `Image.Poll()` reads from image log buffer via `TermReader.Read()` -> `FragmentHandler` callback

**Assessment: CORRECT.**

- The C-05 fix (complete frame header writing) is verified: all header fields (version, flags, frameType, termOffset, sessionID, streamID, termID, reservedValue) are written before frameLength (volatile store last).
- The C-02 fix (composite session key) correctly uses `(sessionID << 32) | streamID` for the image map key, eliminating birthday collision risk.
- The S-01 fix (image TTL eviction) correctly tracks `lastAccess` time and evicts stale entries every 1000 DoWork cycles.
- Frame length is validated against MTU before payload read (OOB protection).

**One concern:** The receiver writes frames at the `termOffset` provided by the sender's frame header. If two senders (unlikely but possible in a misconfigured system) send frames for the same (sessionID, streamID) with overlapping termOffsets, the image buffer would be corrupted. This is an acceptable trust boundary assumption given mTLS authentication, but should be documented.

---

## 6. Concurrency Architecture Assessment

| Property | Assessment | Evidence |
|----------|------------|----------|
| Single-writer principle on log buffers | **MAINTAINED** | Publisher writes via atomic CAS tail-claim; only one goroutine occupies a given byte range. Sender reads from a different position. Receiver writes to image buffers on a single goroutine per pool. |
| Lock-free conductor snapshots (P-03) | **CORRECT** | `sync/atomic.Pointer[[]*PublicationState]` and `sync/atomic.Pointer[[]*SubscriptionState]`. `Publications()` and `Subscriptions()` call `.Load()` -- no torn read possible on pointer-sized atomic. Snapshot slice is rebuilt immutably on each mutation under mutex. |
| Broadcast transmitter serialisation | **CORRECT** | `Transmitter.Transmit()` is called only from the Conductor's `DoWork()`, which runs on a single goroutine. The `EventLog.Log()` wraps Transmit in a mutex for its separate use case. |
| Ring buffer SPSC vs MPSC | **CORRECT** | `OneToOneRingBuffer` (SPSC) is used for client->driver commands where needed; `ManyToOneRingBuffer` (MPSC) is used in the conductor's command ring to allow concurrent client writers. The MPSC uses CAS on tail for space claiming. |
| Adaptive pool learner lock-free evaluation | **CORRECT** | The learner's `Evaluate()` reads a `PoolSnapshot` that was atomically swapped by the PathManager. No locks are held during evaluation. The PoolManager's `DoWork()` holds no lock when calling `Evaluate()`. |
| Agent panic recovery (A-01) | **CORRECT** | `doWorkSafe()` uses `defer recover()`. Panic counter tracks via `atomic.Int64`. Threshold-based graceful stop. |
| Context cancellation | **CORRECT** | All agents check `ctx.Done()` in their run loops. `Driver.Stop()` cancels context and waits via `sync.WaitGroup`. |

**One concern:** The `CCAdapter` wraps every CC method call in a `sync.Mutex`. This is on the Sender's hot path (via `CanSend()` / `PacingRate()` checks). At very high packet rates, this mutex could become a contention point if multiple goroutines share the same adapter. Currently, the Sender is single-goroutine per pool, so this is acceptable. However, if the Dense threading mode is used (Sender+Receiver share a goroutine), and CC callbacks arrive from QUIC's ACK processing goroutine simultaneously, mutex contention is real. This is a medium-priority concern.

---

## 7. Security Architecture Assessment

| Control | Status | Evidence |
|---------|--------|----------|
| mTLS on all inbound connections | **PARTIAL** | `Accept()` in `pkg/transport/quic/conn.go` wraps an incoming `*quic.Conn` but does **not** validate TLS state or call `enforceALPN`/`validateTLSConfig`. TLS enforcement depends entirely on the QUIC listener's TLS config being set correctly upstream. The Dial path is correctly enforced. See finding F-004. |
| Credentials only via secrets provider | **YES** | `pkg/identity` fetches SVIDs from SPIRE. `pkg/secrets/secretsmanager` fetches from AWS. `pkg/secrets/file` reads from local files (dev mode). No hardcoded keys in source. |
| SPIFFE SVID refresh on expiry | **NOT IMPLEMENTED** | Only `Fetch()` exists. No `Watch()` for automatic rotation. SVIDs fetched once at startup. If SVID expires (1h TTL), connections will fail. See finding F-002. |
| Frame length validated before payload read | **YES** | Receiver validates `frameLen > 0 && frameLen <= len(data)` and `payloadLen <= mtu`. Sender validates `payloadLen > mtu`. |
| No SQL injection / command injection / path traversal risks | **YES** | No SQL, no shell execution, no user-controlled file paths. All file paths are controlled by the driver config. |
| mmap files created with 0600 | **YES** | `memmap.Create()` uses `os.OpenFile(..., 0o600)`. |
| TLS 1.3 minimum | **YES** (with caveat) | `enforceALPN()` always sets `MinVersion: tls.VersionTLS13`. `identity.buildTLSConfig()` sets `MinVersion: tls.VersionTLS13`. However, `validateTLSConfig()` allows `MinVersion == 0` through (Go's default is TLS 1.2, but `enforceALPN` overrides it). |

---

## 8. Operational Architecture Assessment

| Component | Status | Notes |
|-----------|--------|-------|
| `hyperspace-stat` CLI | **PRESENT** | `cmd/hyperspace-stat/main.go` exists and builds. Reads from counter buffer. |
| OTel bridge | **PRESENT** | `pkg/otel/provider.go` creates OTel meter and registers counter instruments. |
| `init.sh` gate script | **PRESENT** | Referenced in `CLAUDE.md` enforcement checklist. |
| CI pipeline | **FAILING** | `golangci-lint` has 30+ errors across all branches. This is a hard quality gate violation. Tests pass; lint does not. |
| `cmd/hyperspace-probe/` | **EMPTY** | No implementation. This is a benchmark/diagnostic tool specified in CLAUDE.md. |

---

## 9. Implementation Gaps vs SYSTEM_ARCHITECTURE.md

| Component | Specified | Implemented | Gap |
|-----------|-----------|-------------|-----|
| SPIFFE SVID Watch/rotation | Yes (ADR-004, F-014) | Only single Fetch | **CRITICAL** -- no automatic cert rotation |
| Pool Manager cert rotation via QUIC connection migration | Yes (ADR-004, F-009) | Not implemented | **CRITICAL** -- cert rotation will cause connection drops |
| DRL BBRv3 fallback | Yes (ADR-003) | Falls back to CUBIC | **HIGH** -- wrong fallback algorithm |
| `hyperspace-probe` CLI | Yes (CLAUDE.md package table) | Empty directory | **MEDIUM** -- benchmark tool missing |
| `pkg/obs/` OTel + CloudWatch | Yes (SPEC F-012, CLAUDE.md) | `pkg/obs/` empty; `pkg/otel/` partial | **MEDIUM** -- CloudWatch export not implemented |
| Rate limiting on QUIC connection accept | Yes (Security Controls 5.4) | Not implemented | **MEDIUM** -- DoS protection absent |
| ONNX model hash verification | Yes (Security Controls 5.4, ADR-003) | Not implemented | **MEDIUM** -- model integrity not verified |
| External driver mode (CnC shared memory) | Yes (SYSTEM_ARCHITECTURE.md) | Stub returns error | **LOW** -- expected for current sprint |
| Receiver -> image integration path | Yes (data flow 3.1) | Receiver creates images; subscription reconciles from conductor | **LOW** -- works via conductor state, not direct receiver wiring |

---

## 10. Findings Summary

| ID | Severity | Dimension | Finding | Recommendation |
|----|----------|-----------|---------|----------------|
| F-001 | CRITICAL | ADR Conformance | DRL fallback uses CUBIC, not BBRv3 as specified in ADR-003. The `DRLController.runPolicy` method falls back to `d.fallback.CongestionWindow()` where `fallback` is a `cubicWrapper`. | Change the DRL fallback from CUBIC to BBRv3. Update `cubicWrapper` to `bbrv3Wrapper` or use `cc.New("bbrv3", ...)`. |
| F-002 | CRITICAL | ADR Conformance | SPIFFE SVID Watch/rotation is not implemented. `SPIFFESource` has only `Fetch()`. F-014 requires `Watch(ctx, onChange)` for automatic SVID rotation. Pool Manager has no rotation detection. In production, SVIDs expire after 1 hour; without Watch, all QUIC connections will fail after SVID expiry. | Implement `SPIFFESource.Watch()` using `go-spiffe`'s `workloadapi.WatchX509Context`. Wire the onChange callback into Pool Manager's health check loop. |
| F-003 | CRITICAL | Data Flow | Sender drops frames silently on QUIC send failure. When `conn.Send()` returns an error, the frame has already been consumed from the log buffer. There is no retry, no dead-letter, no counter increment, and no signal to the publisher. This is a data-loss path. | Implement a send retry queue or NAK-based resend mechanism. At minimum, increment `CtrLostFrames` and log at WARN. Consider not advancing the reader position on send failure. |
| F-004 | CRITICAL | Security | `Accept()` does not validate TLS state. The `Accept(conn *quic.Conn)` function in `pkg/transport/quic/conn.go` takes a raw quic-go connection and wraps it without checking ALPN, TLS version, or client certificate presence. If the listener's TLS config is misconfigured, anonymous connections could be accepted. | Add TLS state validation in `Accept()`: verify ALPN is `hyperspace/1`, TLS version is 1.3, and peer certificates are present. Return an error and close the connection if any check fails. |
| F-005 | HIGH | Security | `validateTLSConfig` allows `MinVersion == 0` (Go defaults to TLS 1.2). While `enforceALPN` always overrides to TLS 1.3, the validation function should reject `MinVersion == 0` to prevent a future code change from bypassing `enforceALPN` and allowing TLS 1.2. | Change the validation to `if tlsConf.MinVersion != tls.VersionTLS13` (require explicit TLS 1.3, do not allow zero). |
| F-006 | HIGH | Interface Design | `Connection` interface is defined inside `pkg/transport/quic/` (the implementation package). All consumers import `quictr` to use the interface, coupling them to the QUIC adapter package. | Move the `Connection` interface and `ConnectionStats` struct to `pkg/transport/connection.go`. Have `pkg/transport/quic` implement this external interface. |
| F-007 | HIGH | Operational | CI pipeline fails on all branches. `golangci-lint` has 30+ errors (revive stutter, errcheck, unused, gofmt, ineffassign). This is a hard quality gate violation per CLAUDE.md. | Fix all lint errors. The revive stutter warnings are already suppressed with `//nolint:revive` in most places but some are missing. Address errcheck, unused fields, gofmt, and ineffassign. |
| F-008 | HIGH | Concurrency | `CCAdapter` uses `sync.Mutex` on every `CanSend()` / `PacingRate()` call, which is on the Sender hot path. This is correct for thread safety but introduces lock contention potential, especially if CC callbacks arrive from quic-go's internal goroutines while the Sender is querying. | Consider using `sync.RWMutex` (read lock for CanSend/PacingRate queries, write lock for On* callbacks) or lock-free atomic snapshot for the CC state queries. |
| F-009 | HIGH | Coverage | `pkg/client` coverage is 81.5%, below the 85% minimum. This package contains critical path code (reconcileAfterLap, pollBroadcast backoff). | Add tests for reconcileAfterLap edge cases, pollBroadcast timeout paths, and error paths in AddPublication/AddSubscription. |
| F-010 | MEDIUM | Implementation Gap | `pkg/obs/` is an empty directory. OTel metrics are in `pkg/otel/` instead. CLAUDE.md specifies `pkg/obs/`. CloudWatch metric export (F-012) is not implemented. | Either rename `pkg/otel/` to `pkg/obs/` or update CLAUDE.md. Implement CloudWatch metric export. |
| F-011 | MEDIUM | Implementation Gap | `cmd/hyperspace-probe/` is empty. CLAUDE.md lists it as a diagnostic/benchmark tool. SPEC.md references it for NFR-001 benchmarking. | Implement the probe tool or formally defer it with a decision in `decision_log.md`. |
| F-012 | MEDIUM | Implementation Gap | ONNX model hash verification (specified in Security Controls 5.4 and ADR-003) is not implemented. The model is loaded from disk without integrity verification. | Add SHA-256 hash verification on model load. Store expected hash in config/SSM. |
| F-013 | MEDIUM | Implementation Gap | Rate limiting on QUIC connection accept (Security Controls 5.4) is not implemented. A malicious peer could exhaust resources by opening many connections. | Add a connection rate limiter in the QUIC listener accept loop. |
| F-014 | MEDIUM | Concurrency | `healthCheck` in Pool Manager calls `pool.Connections()` which takes a lock and prunes closed connections, then iterates the returned slice checking `IsClosed()`. The `IsClosed()` check is redundant since `Connections()` already pruned closed ones. The reconnection loop inside `healthCheck` blocks the agent's DoWork cycle with `time.After(delay)` -- this prevents other agents from progressing in Dense/Shared threading mode. | Remove redundant IsClosed check. Consider non-blocking reconnection (spawn a goroutine or use a channel-based timer). |
| F-015 | MEDIUM | Data Model | The frame header in `logbuffer/header.go` is 32 bytes, but `SPEC.md` F-001 specifies 24 bytes (`frameLength int32 | flags uint8 | type uint8 | streamId int32 | sessionId int32 | reservedValue int64`). The implementation adds `version`, `frameType` (uint16 instead of uint8 type), `termOffset`, and `termID` fields. | This is a deliberate extension beyond the original spec for a richer wire format. Document the actual 32-byte header layout as the canonical format and update SPEC.md F-001 to match the implementation. |
| F-016 | MEDIUM | Package Naming | `CLAUDE.md` specifies `pkg/identity/` for both SPIFFE/SPIRE and AWS Secrets Manager cert loading. In practice, `pkg/identity/` handles SPIFFE only, while `pkg/secrets/secretsmanager/` handles AWS Secrets Manager. This is a better separation but diverges from the spec. | Update CLAUDE.md package table to reflect the actual structure. |

---

## 11. Architecture Decision Records -- New Recommendations

### Recommended ADR-007: Secrets Provider Abstraction

**Context:** The implementation introduced `pkg/secrets/` with a `Provider` interface and two implementations (file, secretsmanager) that are not covered by any existing ADR. This is a valuable architectural boundary that should be documented.

**Decision to document:** TLS credentials are loaded via the `secrets.Provider` interface. In production, AWS Secrets Manager is used; in development, file-based loading is used. This abstracts credential source from credential consumption.

### Recommended ADR-008: Frame Header Extension to 32 Bytes

**Context:** The original spec (F-001) specified a 24-byte frame header. The implementation uses 32 bytes, adding version, termOffset, and termID fields while expanding type from uint8 to uint16. This enables richer receiver logic (the receiver needs termOffset to write at the correct position in the image buffer) and future protocol versioning.

**Decision to document:** The 32-byte frame header is the canonical Hyperspace wire format. The additional fields are required for correct receiver operation and protocol evolution.

---

## 12. Verdict Rationale

The Hyperspace architecture is **well-designed and correctly implemented** for a platform of this complexity. The Aeron-inspired shared-memory client/driver separation is faithfully realised in Go with appropriate adaptations (QUIC replacing custom UDP, Go atomics replacing Java Agrona). The interface abstraction layers provide clean boundaries for testing and future evolution. The concurrency model is sound -- lock-free where it matters (log buffer appender, conductor snapshots, counter updates) and mutex-protected where correctness demands it.

The 4 critical findings are:
1. **DRL fallback algorithm mismatch** (CUBIC vs BBRv3) -- straightforward code fix.
2. **SPIFFE Watch/rotation missing** -- significant feature gap with production impact.
3. **Silent frame drop on send failure** -- data-loss risk requiring architectural decision on retry/NAK strategy.
4. **Accept() TLS validation gap** -- security boundary incomplete on the inbound path.

None of these require architectural redesign. Findings 1, 4, and 5 are code-level fixes. Finding 2 requires implementing the Watch API per the existing design. Finding 3 requires a design decision on retry semantics (which should be documented in a new ADR).

**CONDITIONAL PASS** is issued because the architecture is fundamentally sound but the security and data-integrity gaps (F-002, F-003, F-004) must be closed before production readiness.

---

## 13. Conditions for Full PASS

1. **Resolve all CRITICAL findings (F-001 through F-004).** Each must be implemented, tested, and verified by the Code Evaluator.

2. **Resolve all HIGH findings (F-005 through F-009).** F-007 (CI lint) is a governance blocker. F-009 (coverage) is a quality gate violation.

3. **File ADR-007 and ADR-008** to document the secrets provider abstraction and 32-byte frame header decisions.

4. **File an ADR for the send-failure retry strategy** (triggered by F-003). This is an architectural decision that affects the data-loss guarantees of the platform.

5. **Update CLAUDE.md and SPEC.md** to reflect the actual package structure (sub-packages for discovery, config, secrets) and the 32-byte frame header format.

6. **Architecture Evaluator re-review** after critical and high findings are resolved (estimated: 2 sprints).
