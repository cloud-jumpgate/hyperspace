# Shared Knowledge — Hyperspace

> **APPEND-ONLY.** Never delete or overwrite entries. Add new entries at the bottom with timestamp and author. This file accumulates domain knowledge discovered during implementation that is specific to this project's runtime environment, external APIs, and observed system behaviour.

**Format for new entries:**

```
## [DATE] — [Agent] — [Topic]
[Content]
```

---

## 2026-04-17 — harness-architect — Initial Setup

Project initialised. No shared knowledge accumulated yet. Entries will appear here as agents discover non-obvious behaviours, constraints, or patterns during implementation.

**Do not touch list (initial):** None.

**Known gotchas:** None yet.

---

*(Append new entries below this line)*

---

## 2026-04-17 — backend-engineer — Conditional CGO compilation with `//go:build onnx`

**Sprint:** S1 / S7 (DRL controller, delivered in S2)

**Discovery:** The `pkg/cc/drl` package requires CGO for ONNX Runtime. CGO must not be present in the default build (no ONNX Runtime shared library available in all environments). The correct pattern is to gate all CGO-dependent code with the `//go:build onnx` build tag at the top of every file in `pkg/cc/drl` that imports `github.com/yalue/onnxruntime_go`.

**Pattern:**
```go
//go:build onnx

package drl

import (
    ort "github.com/yalue/onnxruntime_go"
    // ...
)
```

A companion file without the build tag (`drl_stub.go`) provides a fallback `DRLController` that delegates directly to BBRv3. This stub is compiled in all builds; the full CGO controller is compiled only when `-tags onnx` is passed to `go build`.

**Impact:** `go build ./...` and `go test ./...` must pass without `-tags onnx`. CI runs both with and without the onnx tag.

---

## 2026-04-17 — backend-engineer — AtomicBuffer little-endian encoding requirement

**Sprint:** S1

**Discovery:** `internal/atomic.AtomicBuffer` wraps a raw byte slice backed by an mmap region. Initial implementation used `unsafe.Pointer` casts directly to read `int64` values from the buffer. This is unsafe on big-endian architectures and produces data that cannot be read by a process compiled for a different architecture.

**Correct approach:** Use `encoding/binary.LittleEndian.Uint64()` and `encoding/binary.LittleEndian.PutUint64()` combined with `sync/atomic` for the concurrent-safe load/store:

```go
// READ: atomic load, then decode little-endian
raw := atomic.LoadUint64((*uint64)(unsafe.Pointer(&b.buffer[offset])))
value := int64(raw)  // already LE on arm64/amd64; explicit decode not needed at runtime

// WRITE: encode little-endian, then atomic store
atomic.StoreUint64((*uint64)(unsafe.Pointer(&b.buffer[offset])), uint64(value))
```

The `unsafe.Pointer` cast is still required to read from an arbitrary byte offset in the buffer (Go does not support unaligned reads otherwise). The critical constraint is that the value encoding matches across all processes sharing the mmap file. Little-endian is used everywhere in Hyperspace.

**Rationale:** The production target (EC2 c7gn.4xlarge, arm64) is natively little-endian so there is zero performance cost. The explicit encoding choice prevents bugs if the code is ported.

---

## 2026-04-17 — backend-engineer — quic-go package naming conflict, alias `quictr`

**Sprint:** S2

**Discovery:** The quic-go package's default import name is `quic` (`github.com/quic-go/quic-go`). Hyperspace also has a package at `pkg/transport/quic`. When both packages are imported in the same file (as happens in connection pool code that imports both the adapter and the underlying library), Go reports a conflict.

**Solution:** Import quic-go with the alias `quictr` in any file where both are imported:

```go
import (
    quictr "github.com/quic-go/quic-go"
    hsquic "github.com/cloud-jumpgate/hyperspace/pkg/transport/quic"
)
```

The alias `quictr` (quic-transport) is the project standard. Do not use `q`, `qgo`, or other ad-hoc aliases — consistency prevents confusion in code review.

**Scope:** This alias is only needed in files that directly import both packages. Most code imports only `pkg/transport/quic` (the adapter) and never sees quic-go directly.

---

## 2026-04-17 — backend-engineer — ALPN `hyperspace/1` must be set on both TLS configs

**Sprint:** S2

**Discovery:** QUIC (via quic-go) uses ALPN (Application-Layer Protocol Negotiation) during the TLS handshake to agree on the application protocol. If `NextProtos` is not set or does not match between client and server, the TLS handshake fails with:

```
tls: no application protocol
```

This error is non-obvious because it looks like a TLS failure rather than a configuration mismatch.

**Required configuration (both client and server `tls.Config`):**
```go
tlsCfg.NextProtos = []string{"hyperspace/1"}
```

The string `"hyperspace/1"` is the Hyperspace ALPN identifier for protocol version 1. It must be identical on both sides. In `pkg/transport/quic/config.go`, `NewTLSConfig` sets this field automatically — callers must not override `NextProtos` after calling `NewTLSConfig`.

**Rule:** Any `tls.Config` used with quic-go that does not include `"hyperspace/1"` in `NextProtos` will fail to connect. Tests that use raw `tls.Config{}` instead of `NewTLSConfig` will produce this error.

---

## 2026-04-17 — backend-engineer — Conductor InjectSnapshot for test hermetics

**Sprint:** S3

**Discovery:** Testing Conductor's reaction to commands (AddPublication, RemovePublication, etc.) requires getting commands into the MPSC ring buffer and waiting for Conductor to process them. In early tests, this required starting the full ring buffer, writing a command, and polling — creating a 50–200 ms latency per test case due to the poll interval.

**Better pattern:** Expose `Conductor.InjectSnapshot(state DriverState)` which bypasses the ring buffer and directly updates the Conductor's internal state snapshot. This makes tests immediate and deterministic.

```go
// In tests:
c := conductor.New(...)
c.InjectSnapshot(conductor.DriverState{
    Publications: []Publication{{LogBufferPath: "/tmp/test.log", StreamID: 1001}},
})
// now test Sender or Receiver reactions to the state
```

The `InjectSnapshot` method is only available in non-production builds (guarded by the `testing` build constraint or via an interface that allows injection). Production Conductor only accepts state changes via ring buffer commands.

**Rule:** All Conductor tests must use `InjectSnapshot` for state setup — do not use the ring buffer command path in unit tests. The ring buffer path is covered by a dedicated integration test.

---

## 2026-04-17 — backend-engineer — Dialer injection in PoolManager

**Sprint:** S5

**Discovery:** PoolManager opens QUIC connections using a `Dialer` function. The initial implementation called `quic.DialAddr` directly, making PoolManager impossible to test without network infrastructure.

**Pattern adopted:** `Dialer` is a first-class field:

```go
type Dialer func(ctx context.Context, addr string, tlsCfg *tls.Config) (transport.Connection, error)

type PoolManager struct {
    dialer Dialer
    // ...
}

func New(dialer Dialer, ...) *PoolManager { ... }
```

In production, `NewPoolManager` passes `quic.DefaultDialer` (which wraps `quictr.DialAddr`). In tests, a mock dialer is injected that returns pre-constructed `mockConnection` objects.

**Test pattern:**
```go
mockDialer := func(ctx context.Context, addr string, _ *tls.Config) (transport.Connection, error) {
    return newMockConnection(addr), nil
}
pm := poolmgr.New(mockDialer, opts)
```

**Impact:** `TestPoolManager_EnsureMin` verifies dial count; `TestPoolManager_ReconnectOnFailure` injects a dialer that fails N times. Both tests run in < 5 ms with zero network calls.

---

## 2026-04-17 — backend-engineer — broadcast.NewReceiver must use current-tail, not from-start

**Sprint:** S6

**Discovery:** `pkg/ipc/broadcast` provides two constructors for `Receiver`:
- `NewReceiver(t *Transmitter)` — starts from the current tail position (only receives messages transmitted AFTER receiver creation)
- `NewReceiverFromStart(t *Transmitter)` — starts from position 0 (replays all messages ever transmitted)

In the client library, `Subscription.Poll` uses a `BroadcastReceiver` to receive frames from the driver. Using `NewReceiverFromStart` caused subscribers to replay all frames transmitted before the subscription was created, including frames from other streams that no longer exist. This produced spurious fragment deliveries and confused the session-ID routing logic.

**Rule:** Always use `broadcast.NewReceiver(t)` (current-tail) when creating subscribers in `pkg/client`. `NewReceiverFromStart` is only correct for replay/audit tools that specifically need historical data.

**Symptom if violated:** Subscriber receives frames with session IDs it has never seen; image log buffer routing fails; test `TestClient_PublishSubscribe_1000` sees more than 1000 messages.

---

## 2026-04-17 — backend-engineer — SSM ParameterNotFound treated as default

**Sprint:** S9

**Discovery:** When `pkg/config.SSMConfig.Get()` is called for a parameter that does not exist in SSM, `aws-sdk-go-v2` returns a specific error type `*ssm.ParameterNotFound`. The initial implementation propagated this error to the caller, requiring all callers to handle it explicitly.

**Better pattern:** `GetOrDefault(ctx, paramPath, defaultValue)` checks for `ParameterNotFound` and returns the default value instead of an error. This allows partial SSM deployments where some parameters are managed via SSM and others use code defaults.

```go
func (c *SSMConfig) GetOrDefault(ctx context.Context, path string, def string) string {
    val, err := c.Get(ctx, path)
    var notFound *types.ParameterNotFound
    if errors.As(err, &notFound) {
        return def
    }
    if err != nil {
        slog.Warn("ssm get failed; using default", "path", path, "err", err)
        return def
    }
    return val
}
```

**Impact:** hsd can start without any SSM parameters configured. All SSM-driven configuration has a sensible in-code default. This is important for local development and CI.

---

## 2026-04-17 — backend-engineer — go-spiffe svid.Marshal() produces PKCS#8 format

**Sprint:** S9

**Discovery:** After fetching an X.509 SVID from the SPIRE Agent, the private key must be serialised to pass to `tls.X509KeyPair`. Two serialisation functions are available:

1. `svid.Marshal()` — produces PKCS#8 DER-encoded private key (correct for `tls.X509KeyPair`)
2. `x509.MarshalECPrivateKey(key)` — produces SEC1/PKCS#1 DER-encoded EC private key (WRONG — produces `tls.X509KeyPair` error)

**Error when using `x509.MarshalECPrivateKey`:**
```
tls: failed to find any PEM data in certificate input
```
This error is misleading — the certificate PEM is correct; the problem is the private key PEM block type (`EC PRIVATE KEY` vs `PRIVATE KEY`).

**Correct code:**
```go
keyDER, err := svid.Marshal()  // produces PKCS#8, compatible with tls.X509KeyPair
if err != nil { return err }
keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
certPEM := svid.Certificates[0].Raw  // DER; encode to PEM separately
cert, err := tls.X509KeyPair(certPEM, keyPEM)
```

**Rule:** Never use `x509.MarshalECPrivateKey` for SPIFFE SVIDs. Always use `svid.Marshal()`.

---

## 2026-04-17 — backend-engineer — secrets/ gitignore pattern blocks pkg/secrets/

**Sprint:** S9

**Discovery:** The root `.gitignore` contains a `secrets/` entry to prevent accidentally committing credential files. This pattern also matches `pkg/secrets/` (the production code path for secret loading helpers), causing `git status` to show those files as untracked/ignored.

**Symptom:** `git add pkg/secrets/` fails silently; `git status` does not show `pkg/secrets/*.go` as untracked; the files are not committed.

**Fix:** Add a negation rule to `.gitignore`:

```gitignore
# Ignore raw secrets directories
secrets/

# Do NOT ignore the production secrets package
!pkg/secrets/
!pkg/secrets/**
```

The negation `!pkg/secrets/` must come AFTER the `secrets/` rule in `.gitignore`. Git applies negation rules in order.

**Verification:** After adding the negation rule, `git status` shows `pkg/secrets/*.go` as untracked (available to add). `git add pkg/secrets/` succeeds.

## 2026-04-17 -- CTO -- S11 Hot Path Correctness Patterns

### C-05: Frame Header Write Ordering
The Receiver's `processFrame` must write ALL header fields (version, flags, frameType, termOffset, sessionID, streamID, termID, reservedValue) BEFORE writing frameLength. The frameLength field is a volatile store (via `SetFrameLength` / `PutInt32Ordered`) that signals readers the frame is ready. If frameLength is written first or alone, readers see frameType=0 (PAD) and silently drop the frame. This is the Aeron-style "write payload, then header fields, then length-last" pattern.

### C-01: CAS Rotation Pattern
`Publication.Offer` uses `CompareAndSwapActivePartitionIndex(currentIdx, nextIdx)` instead of `SetActivePartitionIndex(nextIdx)` for term rotation. This prevents concurrent publishers from double-advancing the partition index. When CAS fails, it means another goroutine already rotated -- the caller simply retries from the new partition on its next Offer call.

### P-01: sync.Pool in Sender
`sender.sendPublication` uses `sync.Pool` with pre-allocated `[]byte` buffers (sized to MTU + HeaderLength) instead of `make([]byte, frameLen)` per frame. Buffers are returned to the pool after `conn.Send` completes. This eliminates ~1M allocs/sec on the hot path. The pool's `New` function captures `maxFrameSize` at construction time.

### A-01: Agent Panic Recovery
`RunAgent` wraps each `DoWork` call in `doWorkSafe`, which uses `defer recover()`. Recovered panics increment an atomic counter. When the counter exceeds `DefaultPanicThreshold` (10), the agent stops gracefully. This prevents a single panicking agent from crashing the entire driver process.

### S-01: Image Map TTL Eviction
Receiver image entries now carry a `lastAccess` timestamp. Every 1000 `DoWork` calls, stale entries (TTL default 60s) are evicted. `RemoveImage(sessionID)` provides immediate removal for `CmdRemoveSubscription`. The `nowFunc` field is injectable for deterministic testing.

---

## 2026-04-18 — CTO — Sprint S12: Fault Tolerance + CC Wiring Patterns

### A-02: Pool Health Check with Reconnection
`PoolManager.healthCheck` runs on a 500ms ticker inside `DoWork`. It detects closed connections (though `pool.Connections()` already prunes them via `pruneClosedLocked()`), then attempts reconnection with exponential backoff (base 100ms, max 10s, 5 retries). When the pool drains to zero, `ErrNoConnections` is logged once (deduplicated via `lastPoolEmpty` flag). `consecutiveFailures` resets when pool returns to healthy state (size >= min).

**Observation:** `pool.Connections()` calls `pruneClosedLocked()` internally, so closed connections are already removed before the health check's explicit `IsClosed()` loop runs. The health check's primary value is the reconnection logic, not closed-connection detection.

### F-03: CC Adapter Wiring
`CCAdapter` in `pkg/cc/adapter.go` wraps a `CongestionControl` instance with a mutex for thread-safe access. Created per-connection via `NewAdapter(ccName, initialCwnd, minRTT)`. Falls back to CUBIC if the requested algorithm is unknown. A global registry (`RegisterAdapter`/`UnregisterAdapter`/`GetAdapter`) maps connection IDs to adapters.

### S-03: Term-Aware Sender Position
`sender.sendPosition` tracks `(partitionIndex, termOffset)` instead of a single int64. When `sendPublication` detects the active partition has changed (term rotation), it resets `termOffset` to 0 for the new partition. This prevents the sender from reading stale data at a high offset in the new partition.

### C-02: Composite Session Key
Receiver image map changed from `map[int32]*imageEntry` (keyed by sessionID alone) to `map[uint64]*imageEntry` using `compositeKey(sessionID, streamID) = uint64(uint32(sessionID))<<32 | uint64(uint32(streamID))`. This eliminates birthday collisions when multiple streams share the same sessionID. `RemoveImage` now takes both `(sessionID, streamID int32)` parameters.

---

## 2026-04-18 -- CTO -- Sprint S13: Operability + Scale Patterns

### F-01: Transport Connection Interface
`quictr.Dial` and `quictr.Accept` now return `quictr.Connection` (the interface) instead of `*quictr.QUICConnection` (the concrete type). All consumers already referenced the interface. Tests that need quic-go-specific methods (e.g., `ConnectionState()`) use type assertion: `conn.(*quictr.QUICConnection)`.

### F-02: Config Externalisation
`sender.New` accepts `SenderOption` functional options. `WithFragmentsPerBatch(n)` overrides the default (32). `conductor.DefaultMaxCommandsPerCycle` (10) and `conductor.DefaultBroadcastMaxPayload` (512) are now exported constants; `maxCmdsPerCycle` is a field on `Conductor`. This pattern allows runtime configuration without breaking existing callers.

### P-03: Lock-Free Conductor Reads
`Conductor.Publications()` and `Subscriptions()` now return `*syncatomic.Pointer[[]*PublicationState]` snapshots. Writes (add/remove) rebuild the snapshot under `mu` and atomically publish via `Store()`. Reads use `Load()` with zero mutex contention. The mutex remains for write serialisation only -- reads on the sender/receiver hot path are lock-free.

### C-08: Sticky Arbitrator Pin Cleanup
`StickyArbitrator` (renamed from unexported `sticky`) exposes `Remove(publicationID int64)` and `PinCount() int`. `Remove` deletes the pin entry, preventing unbounded growth when publications are frequently added and removed. Wire to `handleRemovePublication` in conductor for automatic cleanup.

### A-04: Broadcast Lapping Reconciliation
After `pollBroadcast` detects `ErrLapped`, `reconcileAfterLap()` re-queries the conductor's publication and subscription state. For each pending correlationID found in conductor state, it synthesises the missed response (`RspPublicationReady` or `RspSubscriptionReady`) and delivers it to the waiting request channel.

### A-05: Adaptive pollBroadcast Backoff
`pollBroadcast` starts sleeping at 100us and increases to 1ms after 10 consecutive idle cycles. Any received message resets the backoff to 100us. This reduces CPU usage during idle periods while maintaining low latency when messages are flowing.
