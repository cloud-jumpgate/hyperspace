# Code Evaluator Report — Sprint S17

**Evaluator:** Code Evaluator
**Sprint:** S17
**Date:** 2026-04-19
**Baseline:** `go test -race ./...` exits 0 (all packages pass, verified pre-evaluation)

---

## Feature Verdicts

---

### F-037: Fix F-002 IPC Test Parameterisation

**Verdict: PASS**
**Resolves:** F-002 (Ring Buffers / IPC) → status promoted to `evaluator_pass`

#### Acceptance Criteria Evidence

**Criterion 1: `TestMPSC_ConcurrentProducers` uses exactly 16 goroutines × 1,000 messages (16,000 total asserted)**

Confirmed. File: `pkg/ipc/ringbuffer/ringbuffer_test.go`, lines 464–515.

```
const goroutines = 16
const msgsPerGoroutine = 1000
const totalMessages = goroutines * msgsPerGoroutine // 16,000
```

The test asserts `received != totalMessages` where `totalMessages = 16,000`. The ring buffer is sized to 512 KB (1 << 19) to avoid back-pressure. All 16 producers write 1,000 messages each; a single consumer drains until received == 16,000.

**Criterion 2: `TestBroadcast_MultipleReceivers` uses exactly 4 receivers, each asserting all messages received**

Confirmed. File: `pkg/ipc/broadcast/broadcast_test.go`, lines 275–331.

```
const receiverCount = 4
receivers := make([]*Receiver, receiverCount)
```

All 4 receivers are created via `NewReceiverFromStart`, 5 messages are published, and the test asserts `len(got) == len(messages)` for every receiver index 0–3.

**Criterion 3: Both tests pass with `go test -race -count=1`**

```
=== RUN   TestMPSC_ConcurrentProducers
--- PASS: TestMPSC_ConcurrentProducers (0.07s)
ok  github.com/cloud-jumpgate/hyperspace/pkg/ipc/ringbuffer  1.792s

=== RUN   TestBroadcast_MultipleReceivers
--- PASS: TestBroadcast_MultipleReceivers (0.00s)
ok  github.com/cloud-jumpgate/hyperspace/pkg/ipc/broadcast  1.395s
```

No data races detected.

**Criterion 4: No coverage regression in either package (minimum 85%)**

- `pkg/ipc/ringbuffer`: **96.6%** (was already above threshold; no regression)
- `pkg/ipc/broadcast`: **90.8%** (was already above threshold; no regression)

Both packages exceed the 85% project minimum and the F-002 contract threshold.

---

### F-038: Fix F-003 QUIC Coverage Gap

**Verdict: PASS**
**Resolves:** F-003 (QUIC Transport Adapter) → status promoted to `evaluator_pass`

#### Acceptance Criteria Evidence

**Criterion 1: `go test -count=1 -coverprofile=/tmp/quic_eval.out ./pkg/transport/quic/` succeeds**

Exit code 0. Runtime: 13.849s.

**Criterion 2: `go tool cover -func=/tmp/quic_eval.out | grep total` output**

```
total:  (statements)  91.5%
```

**Criterion 3: Coverage ≥ 90%**

91.5% — exceeds the 90% gate. Previous reported coverage was 88.1%. The gap has been closed.

**Criterion 4: All tests pass with `-race`**

```
ok  github.com/cloud-jumpgate/hyperspace/pkg/transport/quic  15.078s
```

No data races detected.

**Criterion 5: No test calls external services**

Confirmed by inspection of `pkg/transport/quic/`. Test files use quic-go's in-memory transport or loopback infrastructure. No external service calls present.

---

### F-039: Implement DEF-005 — SVID Cert Rotation in PoolManager

**Verdict: CONDITIONAL PASS**
**Note:** Security Evaluator PASS is still required before F-009 can be promoted to `evaluator_pass` per sprint contract.

#### Acceptance Criteria Evidence

**Criterion 1: `SVIDWatcher` interface defined in `pkg/driver/poolmgr`**

PASS. Defined at `pkg/driver/poolmgr/poolmgr.go` lines 21–25:

```go
type SVIDWatcher interface {
    StartWatch(ctx context.Context, callback func(newTLS *tls.Config)) error
}
```

**Criterion 2: `PoolManager.rotateCerts` method implemented**

PASS. Implemented at lines 276–308. The method: (1) snapshots old connections, (2) updates `tlsConf` under write lock, (3) removes old connections via `pool.Remove` (which calls `Close`), (4) opens new connections via `EnsureMinConnections`.

**Criterion 3: `NewWithSVID` constructor added**

PASS. Present at lines 205–215. Signature matches the sprint contract exactly.

**Criterion 4: Old connections drained and closed after rotation**

PASS. `pool.Remove(conn.ID())` is called for each snapshotted old connection; `pool.Remove` calls `conn.Close()` internally (verified in `pkg/transport/pool/pool.go` lines 67–68). Test `TestPoolManager_CertRotation_DrainsThenCloses` asserts `old1.IsClosed()` and `old2.IsClosed()`.

**Criterion 5: New connections opened with new TLS config BEFORE old are closed**

FAIL. The implementation deviates from this criterion. The actual rotation order in `rotateCerts` is:

1. Snapshot old connections
2. Update `tlsConf` under write lock
3. **Close old connections** (via `pool.Remove` for each old conn)
4. **Open new connections** via `EnsureMinConnections`

The contract criterion explicitly requires new connections to be opened **before** old ones are closed (blue-green rotation, zero-gap). The implementation uses drain-then-replace ordering, which creates a transient window where the pool is at zero connections.

However, several mitigating factors apply:

- The sprint contract "Required Implementation" section described `pendingClose []quictr.Connection` as the mechanism for achieving the ordered rotation, but this field is absent from the implementation.
- The test is named `TestPoolManager_CertRotation_DrainsThenCloses`, which precisely documents the implemented (drain-then-close) ordering — the test was written to match the implementation, not the criterion.
- The test passes and the behavior is internally consistent; the deviation is a functional gap relative to the stated acceptance criterion, not a correctness defect in isolation.
- The impact in production: during SVID rotation, the pool briefly dips to zero connections before new ones are established. For a `minSize=2` pool this is a sub-millisecond window (dialer is synchronous in `rotateCerts`), but it is a potential connectivity interruption for inflight requests.

This criterion is evaluated as FAIL against the sprint contract's literal text. The Code Evaluator issues a CONDITIONAL PASS pending remediation of the rotation ordering to open new connections before closing old ones.

**Criterion 6: `mu sync.RWMutex` protects `tlsConf` access; all read sites use `RLock/RUnlock`**

PASS. All four read sites (`EnsureMinConnections`, `DoWork` LearnerDecisionAdd branch, `healthCheck` reconnect path, and the write site in `rotateCerts`) correctly use `RLock/RUnlock` or `Lock/Unlock` respectively. No bare reads of `tlsConf` found outside a lock.

Read sites verified (all use `mu.RLock` / `mu.RUnlock`):
- `EnsureMinConnections` lines 314–316
- `DoWork` (Add branch) lines 358–360
- `healthCheck` (reconnect path) lines 447–449

Write site verified (uses `mu.Lock` / `mu.Unlock`):
- `rotateCerts` lines 286–288

**Criterion 7: `TestPoolManager_CertRotation_DrainsThenCloses` present and passes**

PASS.

```
=== RUN   TestPoolManager_CertRotation_DrainsThenCloses
--- PASS: TestPoolManager_CertRotation_DrainsThenCloses (0.00s)
```

**Criterion 8: `TestPoolManager_CertRotation_NoDeadlock` present and passes with `-race`**

PASS.

```
=== RUN   TestPoolManager_CertRotation_NoDeadlock
--- PASS: TestPoolManager_CertRotation_NoDeadlock (0.20s)
```

No data races. The `mockSVIDWatcher.StartWatch` correctly blocks on `<-ctx.Done()` so goleak finds no leaked goroutines.

**Criterion 9: `go test -race ./pkg/driver/poolmgr/` exits 0**

PASS. Full package run:

```
ok  github.com/cloud-jumpgate/hyperspace/pkg/driver/poolmgr  1.552s
```

Exit code 0. No data races.

**Criterion 10: Security Evaluator PASS required before `evaluator_pass`**

Not evaluated here — this is a Security Evaluator gate. F-009 cannot be marked `evaluator_pass` until the Security Evaluator issues PASS. The Code Evaluator finding on criterion 5 (rotation ordering) should be included in the Security Evaluator's scope, as it represents a transient zero-connection window during SVID rotation that could affect availability under cert expiry conditions.

---

## Required Remediation: F-039 Criterion 5

The implementing agent must correct the rotation order in `rotateCerts`:

**Current order (drain-then-replace):**
1. Update `tlsConf`
2. Close old connections via `pool.Remove`
3. Open new connections via `EnsureMinConnections`

**Required order (blue-green):**
1. Update `tlsConf`
2. Open `MinSize` new connections using the new TLS config (these go into the pool alongside old ones — pool must not be at `maxSize` for this to work, or `maxSize` must be temporarily relaxed)
3. Remove old connections (close them) after new ones are established

Note to implementing agent: the sprint contract's `pendingClose []quictr.Connection` pattern supports this — snapshot old IDs, open new connections first, then iterate pendingClose and call `Remove`/`Close` on old ones. The test name `DrainsThenCloses` must be updated to reflect the corrected ordering, or a new test added asserting blue-green semantics.

This remediation is low-severity from a test perspective (tests pass as-is) but is a functional gap that the Security Evaluator must also assess for production readiness.

---

## Full Test Suite Status

`go test -race -count=1 ./...` — **all packages PASS**, exit code 0.

---

## Summary

| Feature | Verdict | Key Finding |
|---|---|---|
| F-037 | PASS | 16 goroutines × 1,000 messages confirmed; 4 receivers confirmed; coverage 96.6% / 90.8% |
| F-038 | PASS | Coverage 91.5% — exceeds 90% gate; all tests pass with `-race` |
| F-039 | CONDITIONAL PASS | 8/9 code criteria met; criterion 5 (new-before-old ordering) not met; Security Evaluator gate still required |

---

## Verdict JSON

```json
{
  "verdicts": [
    {
      "id": "F-037",
      "code_evaluator_verdict": "PASS",
      "status": "evaluator_pass",
      "resolves": "F-002",
      "notes": "16 goroutines x 1000 messages confirmed. 4 receivers confirmed. Coverage: ringbuffer 96.6%, broadcast 90.8%. All tests pass with -race."
    },
    {
      "id": "F-038",
      "code_evaluator_verdict": "PASS",
      "status": "evaluator_pass",
      "resolves": "F-003",
      "notes": "Coverage 91.5% exceeds 90% gate. All tests pass with -race. No external service calls."
    },
    {
      "id": "F-039",
      "code_evaluator_verdict": "CONDITIONAL_PASS",
      "status": "conditional_pass_pending_security_evaluator",
      "resolves": "F-009",
      "blocking_criteria": [
        "Criterion 5: New connections must be opened before old are closed (blue-green rotation). Implementation uses drain-then-replace. Remediation required.",
        "Security Evaluator PASS required per sprint contract before evaluator_pass."
      ],
      "notes": "SVIDWatcher interface, rotateCerts, NewWithSVID all present. mu RWMutex correctly protects all tlsConf read/write sites. Both cert rotation tests pass with -race. go test -race ./pkg/driver/poolmgr/ exits 0. Rotation ordering (criterion 5) deviates from contract specification."
    }
  ],
  "parent_feature_updates": {
    "F-002": "evaluator_pass",
    "F-003": "evaluator_pass",
    "F-009": "requires Security Evaluator PASS; Code Evaluator CONDITIONAL_PASS — remediation of rotation ordering required before Security Evaluator review"
  }
}
```
