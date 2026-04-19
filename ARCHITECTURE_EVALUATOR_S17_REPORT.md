# Architecture Evaluator Report — S17 (2026-04-19)

## Verdict: CONDITIONAL PASS

## Executive Summary

Hyperspace's architecture is substantially sound. The core design — a zero-copy shared-memory client path, an out-of-process driver daemon with five cooperating goroutine agents, a Multi-QUIC connection pool with an Arbitrator for per-batch connection selection, and SPIFFE/SPIRE for workload identity — correctly implements the system described in `SYSTEM_ARCHITECTURE.md`. Package layering is well-enforced with no import cycles detected, and the security architecture (TLS 1.3 enforcement, mmap 0600 permissions, defence-in-depth SPIFFE ID validation, gosec clean) is in good shape after S14–S16 remediation. However, there is one P0 defect that blocks production deploy: the `SVIDWatcher` interface in `pkg/driver/poolmgr` has a different method signature from `SPIFFESource.StartWatch` in `pkg/identity`, meaning `SPIFFESource` does NOT satisfy `SVIDWatcher` and cert rotation cannot be wired in production. Three P1 defects require resolution before the next sprint. One P2 and two advisory items are also noted.

---

## Domain Reviews

---

### 1. System Architecture Conformance

**Finding 1.1 — Architecture match: PASS (with noted gaps)**

The implementation broadly conforms to the `SYSTEM_ARCHITECTURE.md` container diagram and component responsibilities:

- Five driver agents (Conductor, Sender, Receiver, PathManager, PoolManager) are implemented in their specified packages and cooperate via the `Driver` composition root.
- The client library communicates with the driver via `ManyToOneRingBuffer` (client→driver) and `broadcast.Transmitter/Receiver` (driver→client), matching the design.
- The Multi-QUIC pool with Arbitrator strategies (LowestRTT, LeastOutstanding, Hybrid, Sticky, Random) is fully implemented.
- The QUIC adapter wraps quic-go behind the `Connection` interface; no component outside `pkg/transport/quic` imports quic-go types.
- SPIFFE/SPIRE identity fetching via `pkg/identity` is implemented with atomic TLS config rotation (ADR-008 compliant).
- AWS integrations (Cloud Map, SSM, Secrets Manager) are implemented as specified in S9.
- The congestion control registry (CUBIC, BBR, BBRv3, DRL with BBRv3 fallback per ADR-007) is implemented and wired via `CCAdapter`.

**Divergences from architecture:**

- **Observability gap (advisory):** `SYSTEM_ARCHITECTURE.md` and `SPEC.md F-012` specify an `InitTracer`/`StartSpan` OTel tracing API and a CloudWatch counter export. The `pkg/otel` package provides only an OTel gauge bridge from counters; OTel tracing and CloudWatch metric export are absent. The S16 Code Evaluator report documents these as CTO-deferred scope decisions, so this is not a conformance violation — but the architecture document should be updated to reflect the actual delivered scope.
- **External client mode not implemented (advisory):** `pkg/client.NewExternal()` returns an error stub. The architecture shows client connecting to hsd via CnC mmap; this path is not implemented. Acceptable for the current sprint given embedded mode coverage.
- **`pkg/obs` vs `pkg/otel` naming (advisory):** `SYSTEM_ARCHITECTURE.md` references `pkg/obs` but the implementation uses `pkg/otel`. The directory listing also shows an empty `pkg/obs` mount point. The spec package path diverges from implementation. No functional impact but a documentation inconsistency.

---

### 2. Package Layering and Dependency Graph

**Finding 2.1 — Dependency layering: PASS**

The intended layer ordering (client → driver → transport → ipc/logbuffer, internal/atomic at the bottom) is respected. Verified from import declarations:

| Package | Imports (non-stdlib, non-framework) |
|---|---|
| `pkg/client` | `pkg/driver`, `pkg/driver/conductor`, `pkg/ipc/broadcast`, `pkg/ipc/ringbuffer`, `internal/atomic` |
| `pkg/driver` | `pkg/driver/conductor`, `pkg/driver/sender`, `pkg/driver/receiver`, `pkg/transport/arbitrator`, `internal/atomic` |
| `pkg/driver/sender` | `pkg/driver/conductor`, `pkg/logbuffer`, `pkg/transport/arbitrator`, `pkg/transport/pool`, `pkg/transport/quic`, `pkg/counters`, `internal/atomic` |
| `pkg/driver/poolmgr` | `pkg/driver/pathmgr`, `pkg/transport/pool`, `pkg/transport/quic` |
| `pkg/transport/quic` | (quic-go only) |
| `pkg/logbuffer` | `internal/atomic` |
| `internal/atomic` | (stdlib only) |

No cycles are present. The lower layers (`internal/atomic`, `pkg/logbuffer`, `pkg/ipc/*`) do not import upper layers. The `pkg/cc` hierarchy is self-contained and imports only its own sub-packages.

**Finding 2.2 — CGO boundary respected: PASS**

CGO is used only in `pkg/cc/drl` (ONNX Runtime) and `pkg/ipc/memmap` (golang.org/x/sys/unix). No other package uses CGO. This conforms to the ADR-003 restriction and the HARD STOP rule.

---

### 3. Interface Boundaries

**Finding 3.1 — P0: SVIDWatcher interface/implementation signature mismatch**

*Severity: P0 — Blocks production deploy*

`pkg/driver/poolmgr` defines:

```go
type SVIDWatcher interface {
    StartWatch(ctx context.Context, callback func(newTLS *tls.Config)) error
}
```

`pkg/identity.SPIFFESource.StartWatch` has the signature:

```go
func (s *SPIFFESource) StartWatch(ctx context.Context) error
```

`SPIFFESource` does NOT satisfy `SVIDWatcher`. The production wiring code in `poolmgr.Run()` calls `mgr.svid.StartWatch(ctx, callback)` but there is no concrete type that satisfies this interface. In production, `NewWithSVID()` would require passing an `SVIDWatcher` implementation, but `SPIFFESource` cannot be used as one without an adapter or signature change.

**Consequence:** SVID cert rotation cannot be wired in production. This was partially masked in testing because `poolmgr` tests use a mock `SVIDWatcher`, not `SPIFFESource`. The feature F-039 (implement DEF-005) will fail at the integration point unless this mismatch is resolved first.

**Recommendation:** Choose one of two resolutions:
1. Change `SPIFFESource.StartWatch` to accept a callback: `StartWatch(ctx context.Context, onChange func(*tls.Config)) error`. The existing atomic-pointer approach becomes the internal implementation; the callback is invoked on each rotation event. This aligns `SPIFFESource` with `SVIDWatcher`.
2. Write an adapter in `pkg/identity` that wraps `SPIFFESource` and satisfies the callback-based `SVIDWatcher` interface.

Option 1 is simpler and is recommended. This must be resolved as part of F-039 before any production deploy.

**Finding 3.2 — P1: quic-go 0-RTT enabled without replay protection analysis**

*Severity: P1*

`quicConfig()` sets `Allow0RTT: true`. SPEC.md Open Question Q-001 ("Should Hyperspace support QUIC 0-RTT for reconnection? 0-RTT has replay attack implications at the messaging layer.") is marked Open since S2 with no resolution. At 16 sprints elapsed, this is overdue. 0-RTT means reconnecting clients can send data before the server verifies the TLS handshake completes, creating a replay window. For a pub/sub platform that does not use idempotency tokens at the message layer, a replayed frame could result in duplicate message delivery.

**Recommendation:** File ADR-015 resolving Q-001. Options: (a) disable 0-RTT (`Allow0RTT: false`) as the conservative choice given no session resumption performance data justifying the risk; (b) accept 0-RTT with explicit documentation that applications must handle duplicate delivery; (c) add a monotonic sequence number to the frame header's `reserved_val` field and implement duplicate detection in the receiver. This ADR must be resolved before production deploy.

**Finding 3.3 — P2: CCAdapter registry uses package-level mutex, not per-connection lifecycle**

*Severity: P2*

`pkg/cc.RegisterAdapter`/`UnregisterAdapter`/`GetAdapter` use a package-level `sync.Mutex` over a global `map[uint64]*CCAdapter`. The `Sender` consults this registry via `cc.GetAdapter(conn.ID())` to check `CanSend()`. The problem is that nothing in the current `Sender.sendPublication` code path actually calls `cc.GetAdapter`. The CC adapter is registered at connection creation time but the Sender's hot path (`sendPublication`) does not consult it — it calls `conn.Send()` unconditionally. The CCAdapter is therefore registered but not integrated into the send decision path. This was noted in the Code Evaluator S16 report (F-021) as "wire CC into connections" but the wiring stops at registration; the Sender does not use the registered adapter to gate sends.

**Recommendation:** In the Sender's `sendPublication`, before calling `conn.Send`, look up the adapter via `cc.GetAdapter(conn.ID())` and check `adapter.CanSend(bytesInFlight)`. If the CC says to hold, apply back-pressure rather than sending unconditionally. This is required for the congestion control system to have any effect on the data plane.

**Finding 3.4 — Advisory: Conductor.DoWork not context-safe on ring buffer read**

*Severity: Advisory*

`Conductor.DoWork` ignores the `ctx context.Context` parameter it receives — it neither checks `ctx.Done()` nor passes ctx to its ring buffer read. In practice this is fine because the agent loop in `driver.RunAgent` handles context cancellation by exiting the loop before calling DoWork again. However, if DoWork were called in a context where long spin-waits in the ring buffer reader could occur (e.g., the `ManyToOneRingBuffer.Read` spin on `recordLen == 0`), the goroutine would not be context-cancellable during that spin. For the current implementation, the spin is bounded by `maxMessages` and returns promptly, so this is low risk.

---

### 4. Concurrency Model

**Finding 4.1 — Hot path atomics: PASS**

- `TermAppender.claim()` uses `GetAndAddInt64` (atomic fetch-and-add) — no mutex on the appender hot path. Correct.
- `ManyToOneRingBuffer.Write` uses `CompareAndSetInt64` CAS for the multi-producer tail claim — correct lock-free MPSC pattern.
- `Conductor.Publications()` and `Subscriptions()` use `sync/atomic.Pointer[T]` for lock-free snapshot reads from the Sender and Receiver hot paths — correct (P-03 fix).
- `pkg/counters.CountersWriter.Add` uses `sync/atomic.AddInt64` — correct.
- `internal/atomic.AtomicBuffer` uses `sync/atomic` throughout — correct.
- `PathManager.PoolSnapshot` uses `atomic.Pointer[PoolSnapshot]` — snapshot publishing is lock-free. Correct.

**Finding 4.2 — Goroutine lifecycle: PASS**

All driver agents implement `DoWork(ctx context.Context)` and the `RunAgent` loop in `pkg/driver/agent.go` exits on `ctx.Done()`. `QUICConnection` starts a background goroutine (`acceptUniStreams`) that respects `qc.ctx` cancellation. `SPIFFESource.runWatch` goroutine respects context cancellation and closes `watchDone` on exit; `Close()` waits for it. The `pollBroadcast` goroutine in `pkg/client` is stopped via `c.stopPoll()` and `c.pollDone`. `poolmgr_test.go` uses `goleak.VerifyTestMain` confirming goroutine lifecycle is verified in tests.

**Finding 4.3 — P1: IsClosed check race in QUICConnection**

*Severity: P1*

`QUICConnection.IsClosed()` acquires `closedMu` and returns `qc.closed`. However, `Send()`, `SendControl()`, `SendProbe()`, `RecvControl()`, `RecvProbe()`, and `RecvData()` all call `qc.IsClosed()` (taking the lock), release it, then proceed with the stream write. Between the `IsClosed()` check and the subsequent stream write, `Close()` can be called by another goroutine, leaving a window where the connection is closed but the write proceeds. While quic-go's internal stream write will return an error in this case (which is propagated up), the double-lock-take pattern (lock for check, unlock, lock again for close) means the check provides only a best-effort fast path, not a hard guarantee. The current pattern is broadly acceptable for a non-blocking fast-return design, but callers must never treat `IsClosed() == false` as a guaranteed-open invariant.

This is not a data-corruption risk (quic-go handles the error), but the IsClosed check is misleading if read as a guarantee. Document this explicitly in the `Connection` interface comment, or restructure to use a single atomic bool for `closed` (eliminating the mutex from the hot read path).

**Finding 4.4 — Advisory: PathManager mu held across snapshot publication**

`PathManager` holds `pm.mu` during a block that calls `pm.connStates` lookup and then releases it before calling `ptr.Store(snap)`. The sequence is: lock → compute sRTT → unlock → call `ptr.Store`. This is correct — the store is atomic via `atomic.Pointer`. However, during `sweepTimedOutProbes`, the pattern lock/collect/unlock/rebuild/lock/rebuild/unlock introduces two separate critical sections for the same peer's snapshot update. If two sweep calls run concurrently (they do not in the current design — PathManager has no concurrent DoWork callers), a stale snapshot could be published. Given `PathManager.DoWork` is called from a single agent goroutine, this is safe today. Document the single-goroutine constraint explicitly.

---

### 5. Security Architecture

**Finding 5.1 — TLS 1.3 enforcement: PASS**

`validateTLSConfig` panics on `MinVersion == 0` (ADR-012) and returns an error for any value below `tls.VersionTLS13`. `enforceALPN` additionally overrides `MinVersion = tls.VersionTLS13` on every config clone. `buildTLSConfig` in `pkg/identity` sets `MinVersion: tls.VersionTLS13` explicitly. No path through the codebase creates a TLS config with a lower minimum version.

**Finding 5.2 — mmap permissions: PASS**

`memmap.Create` opens files with `os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600`. `memmap.Open` uses `0o600`. `memmap.OpenReadOnly` uses `0o400`. Directory creation uses `0o750` (not world-writable). Compliant with the security requirement.

**Finding 5.3 — SPIFFE ID validation on Accept: PASS**

`Accept()` (ADR-010) validates `HandshakeComplete`, non-empty `PeerCertificates`, and optionally SPIFFE URI SAN presence. `RequireSPIFFE: false` is the default for backward compatibility — production deployments must set it to `true`. This is documented in the ADR. The architecture evaluator notes that the `hsd` binary's production configuration must be audited to confirm `RequireSPIFFE: true` is set before deploy.

**Finding 5.4 — P1: 0-RTT replay risk (see Finding 3.2)**

Already captured under Interface Boundaries. Repeated here for severity tracking.

**Finding 5.5 — No hardcoded credentials: PASS**

All credential loading uses AWS SDK v2 with IAM role credentials (no hardcoded keys), SPIRE Workload API (no embedded certs), or file-loaded TLS certs for local development. The gosec scan (F-036) resolved all 75 findings including the G404 crypto/rand fix for conductor session IDs.

**Finding 5.6 — Frame length validation: PASS**

`Receiver.processFrame` validates `frameLen > 0 && frameLen <= len(data)` and `payloadLen <= r.mtu` before any read. `Sender.sendPublication` validates `payloadLen > s.mtu` before send. The `internal/atomic.AtomicBuffer` methods include `checkBounds` panics for out-of-range accesses. Frame length validation meets the security requirement.

---

### 6. Performance Architecture

**Finding 6.1 — Log buffer appender: PASS**

`TermAppender.AppendUnfragmented` performs one `GetAndAddInt64` atomic fetch-and-add to claim space, then writes payload bytes and header fields non-atomically, with `SetFrameLength` as the final volatile store signalling readers. This is the correct Aeron-style store-release pattern. No mutex on the hot append path.

**Finding 6.2 — Ring buffer write: PASS**

`OneToOneRingBuffer.Write` reads tail/head with `GetInt64Volatile` (atomic load), writes payload, then stores `record_length` last with `PutInt32Ordered` (atomic store). `ManyToOneRingBuffer.Write` uses `CompareAndSetInt64` CAS for multi-producer tail claim. Both are lock-free. Correct.

**Finding 6.3 — P2: CCAdapter uses sync.Mutex on hot path**

*Severity: P2*

Every method on `CCAdapter` (CanSend, CongestionWindow, PacingRate, OnPacketSent, etc.) acquires `a.mu sync.Mutex`. On the critical send path, if the Sender were to call `adapter.CanSend(inFlight)` before each frame send (as recommended in Finding 3.3), this mutex acquisition would occur at up to 1M msg/s, creating a serialisation point. The `CongestionController` interface methods should be callable from a single goroutine (the Sender), making the mutex unnecessary. `CCAdapter` should be redesigned as a non-thread-safe per-connection type accessed only from the Sender goroutine, with thread-safety at the registry level only. Alternatively, use `sync/atomic` operations on the cwnd value for the hot `CanSend` path.

**Finding 6.4 — Sender sync.Pool for frame buffers: PASS**

`Sender.framePool` (a `sync.Pool` of `*[]byte`) is used to avoid per-frame heap allocation on the send path. `bufPtr` is returned to the pool after use. This is the correct pattern (P-01 fix).

**Finding 6.5 — Advisory: Sender gatherConnections allocates per DoWork call**

`gatherConnections()` appends connections from all pools into a new `[]quictr.Connection` slice on every `DoWork` call. For a daemon with 4 peers × 4 connections = 16 connections, this is a 16-element slice allocation at the hot-path frequency of the Sender's busy-poll loop. At 1M msg/s this is not the dominant cost, but it is avoidable. A pre-allocated staging slice could be reused between DoWork calls.

---

### 7. Observability Architecture

**Finding 7.1 — Structured logging: PASS**

All production code paths use `log/slog` with structured fields. No `log.Printf`, `fmt.Print`, or `log.Print` calls were found in any `pkg/` source file. The key fields (`session_id`, `stream_id`, `conn_id`) are used consistently in Conductor, Sender, Receiver, and PathManager log calls.

**Finding 7.2 — Atomic counters: PASS**

`pkg/counters.CountersWriter.Add` and `Set` use `sync/atomic.AddInt64` and `atomic.StoreInt64` respectively. Counters are backed by a plain byte slice accessed via unsafe pointers (same pattern as `internal/atomic.AtomicBuffer`). The 12 defined counters cover the minimum set specified in SPEC.md F-012.

**Finding 7.3 — Advisory: probe_rtt_us and pool_size counters absent**

SPEC.md F-012 specifies `probe_rtt_us` (per connection) and `pool_size` as required counters. The current `pkg/counters` defines 12 counters, none of which is `probe_rtt_us` or `pool_size`. The available per-connection RTT data lives in `PathManager.connStates` (not in the counter array), and pool size is not tracked as a named counter. These two counters are needed for the `hyperspace-stat` display and CloudWatch export to be operationally useful.

---

## ADR Recommendations

### ADR-015 (Recommended): QUIC 0-RTT Policy

**Context:** Q-001 has been open since S2. `Allow0RTT: true` is set without a documented rationale or replay mitigation strategy. At 16 sprints elapsed this is overdue.

**Decision needed:** Choose one of (a) disable 0-RTT; (b) accept 0-RTT with explicit acknowledgement of at-most-once delivery requirement; (c) implement sequence-number-based duplicate detection.

**Trigger:** This ADR must be filed and resolved before production deploy.

---

### ADR-016 (Recommended): SVIDWatcher Interface Canonical Signature

**Context:** The `SVIDWatcher` interface in `pkg/driver/poolmgr` and `SPIFFESource.StartWatch` have incompatible signatures. The resolution chosen (callback-based or adapter-based) should be documented as the authoritative design decision for SVID rotation integration.

**Decision needed:** Canonical signature for SVID rotation callback integration between `pkg/identity` and `pkg/driver/poolmgr`.

**Trigger:** Must be filed and resolved as part of F-039 (DEF-005 SVID cert rotation).

---

## Sign-Off Conditions

This report issues a **CONDITIONAL PASS**. The following items must be resolved before this evaluation upgrades to PASS:

### P0 — Must resolve before production deploy

1. **P0-001 (Finding 3.1):** `SPIFFESource` does not satisfy the `SVIDWatcher` interface. Resolve the method signature mismatch. SVID cert rotation is non-functional in production until this is fixed. File ADR-016 documenting the chosen resolution. Verify with a compile-time interface assertion: `var _ SVIDWatcher = (*identity.SPIFFESource)(nil)` in `pkg/driver/poolmgr`.

### P1 — Must resolve before next sprint (S18)

2. **P1-001 (Finding 3.2):** File ADR-015 resolving Q-001 (QUIC 0-RTT policy). The decision may be to disable 0-RTT as the conservative default, which requires a single-line change to `quicConfig()`. The ADR must exist before the next sprint boundary.

3. **P1-002 (Finding 4.3):** Add a comment to the `Connection` interface documenting that `IsClosed()` is a best-effort fast path, not a guaranteed invariant. Alternatively, replace `closedMu sync.Mutex + closed bool` with `atomic.Bool` to make `IsClosed()` a single atomic load with no lock contention on the hot path.

### P2 — Should resolve within 2 sprints (S18–S19)

4. **P2-001 (Finding 3.3):** Wire the registered `CCAdapter` into the `Sender.sendPublication` path so that `adapter.CanSend(bytesInFlight)` gates frame sends. Without this, the congestion control system has no effect on the data plane.

5. **P2-002 (Finding 6.3):** Replace `sync.Mutex` in `CCAdapter` with `sync/atomic` operations for `CongestionWindow` and `CanSend` on the hot path. The adapter is per-connection; if accessed only from the Sender goroutine, the mutex can be removed entirely.

---

*Report produced by: Architecture Evaluator*
*Sprint: S17*
*Date: 2026-04-19*
*Status: CONDITIONAL PASS — awaiting P0-001, P1-001, P1-002 resolution*
