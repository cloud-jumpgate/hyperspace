# Domain Knowledge — Hyperspace

**Status:** Active
**Owner:** Harness Architect
**Last Updated:** 2026-04-18
**Project:** github.com/cloud-jumpgate/hyperspace

---

## Business Domain

Hyperspace is a Go-native high-performance publish/subscribe messaging platform for latency-sensitive distributed systems on AWS EC2. It solves the problem of inter-service messaging at sub-100-microsecond P99 latency and one million messages per second without the JVM overhead of Aeron or the broker complexity of Kafka. The core insight is that the publisher and subscriber hot paths should be shared-memory operations (zero system calls) while network I/O is handled asynchronously by a dedicated out-of-process driver daemon (`hsd`).

---

## Key Entities

### Log Buffer

The log buffer is the fundamental shared-memory unit. It is an mmap'd file (`termLength * 3` bytes plus a 64-byte metadata header) divided into three equal-sized terms. Publishers append frames to the active term; the driver daemon reads and sends them over QUIC. The three-term rotation provides backpressure: if a term is full and the next term has not been consumed yet, `Append` returns `ErrBackPressure` instead of blocking.

**Three-term rotation invariant:** At most one term is being written (active), one is being read/drained by the Sender, and one is clean and ready as the next active term. The active term ID is stored as an atomic int64 in the metadata header; term rotation is a single atomic CAS.

**Frame header layout (32 bytes, 9 fields):**

```
Offset  Size  Field
0       4     FrameLength int32      — total frame size including header
4       1     Flags uint8            — fragmentation/control flags
5       1     FrameType uint8        — DATA=0x01, PADDING=0x00, PING=0x02, PONG=0x03
6       2     Reserved uint16        — must be zero
8       4     StreamID int32         — logical stream identifier
12      4     SessionID int32        — connection session identifier
16      8     ReservedValue int64    — available for future use (zero for DATA frames)
24      8     TermOffset int64       — position within the term at which this frame begins
```

Note: The SPEC documents the header as 24 bytes with 6 fields; the implementation uses 32 bytes (with `Reserved uint16` and `TermOffset int64` added for alignment and reader position tracking). Agents should treat the implementation as authoritative for wire format.

**Tail-claim via atomic XADD:** The appender claims space in the active term by atomically adding `alignedFrameLength` to the term tail counter (`sync/atomic.AddInt64`). This is equivalent to an x86 LOCK XADD. Two concurrent appenders cannot claim the same offset because the add is atomic. The frame length is written via a volatile store (`atomic.StoreInt32`) after the frame payload is written, so the reader can detect a complete frame by polling the frame length field.

**Volatile frame length write:** The reader spins on the frame length field at its current read position. It sees a non-zero frame length only after the appender has completed the write (`atomic.StoreInt32` with the final frame length). This is the only synchronisation between appender and reader — no mutex is needed.

### Multi-QUIC Pool

Each peer pair maintains N concurrent QUIC connections (default N=4, configurable via `pool=N` in the channel URI). Each connection is an independent QUIC session with its own congestion control loop, flow control window, and ECMP hash bucket on the AWS EC2 spine fabric. This exploits AWS's equal-cost multipath routing: different QUIC connections hash to different physical paths.

**ECMP path diversity:** On AWS EC2 c7gn instances, ECMP hashing uses the 5-tuple (src IP, dst IP, src port, dst port, protocol). Multiple QUIC connections to the same peer will have different source port numbers, producing different hash buckets. The Multi-QUIC pool is the mechanism for exploiting this without kernel-level bonding.

**Independent CC state:** Each connection in the pool has its own `CongestionController` instance. A congestion event on one connection does not affect others. This prevents one slow path from degrading throughput across the entire connection pool.

**Pool data structure:** `pkg/transport/pool.Pool` holds `[]*poolEntry` protected by `sync.RWMutex`. Reads (`Connections()`, `Len()`) acquire a read lock; writes (`Add()`, `Remove()`) acquire a write lock. The Arbitrator operates on a snapshot returned by `Connections()`.

### Five Driver Agents

All five agents run as goroutines within the `hsd` process. They share state via atomic pointer swaps (pool snapshot) and channels (conductor state updates), never via direct mutex-protected shared structs.

**Conductor** (control plane): Reads commands from the MPSC ring buffer (`pkg/ipc/ringbuffer.MPSCRingBuffer`). Handles AddPublication, RemovePublication, AddSubscription, RemoveSubscription, ClientKeepalive. Propagates state changes to Sender and Receiver via an atomic pointer swap on the `DriverState` struct. Exposes `InjectSnapshot(DriverState)` for test-time state injection without going through the ring buffer.

**Sender** (outbound data plane): Busy-polls all active publication log buffers using `Reader.Poll`. Batches up to `maxBatchSize` frames (default 64) per iteration, calls `Arbitrator.Select` once per batch, and sends the batch on the selected connection. Falls back with 1 ms sleep when the pool is empty. Calls `runtime.Gosched()` between iterations when no frames are available to avoid burning a core at idle.

**Receiver** (inbound data plane): Accepts frames from all pool connections. Routes frames by `(streamID, sessionID)` to the correct image log buffer via `Appender.Append`. Silently drops frames with unknown `(streamID, sessionID)` pairs and increments a counter.

**PathManager** (path intelligence): Sends PING frames on each live connection every 100 ms (configurable). Matches PONG responses by sequence number and updates `ProbeRecord` atomically. Sets `RTTSampleEWMA = math.MaxInt64` on timeout (500 ms default) to exclude the connection from `LowestRTT` arbitration.

**PoolManager** (lifecycle): Opens and closes QUIC connections. Integrates `AdaptivePoolLearner` recommendations every 30 s. Watches SPIRE workload API for SVID rotation; opens new-cert connections before closing old ones to achieve zero-message-loss cert rotation. Uses a configurable `Dialer` function (injected at construction) for testability without network.

### AtomicBuffer Pattern

`internal/atomic.AtomicBuffer` wraps a raw byte slice (typically backed by an mmap region) and exposes typed atomic read/write operations via `sync/atomic` and `unsafe.Pointer`.

**Little-endian requirement:** All multi-byte integer reads and writes use `encoding/binary.LittleEndian` — not unsafe direct cast. ARM64 (the production target, c7gn.4xlarge) is natively little-endian so the performance cost is zero, but the explicit use of `encoding/binary.LittleEndian` prevents bugs if the code is ever run on a big-endian architecture.

**Implementation pattern:**
```go
func (b *AtomicBuffer) GetInt64(offset int) int64 {
    return int64(atomic.LoadUint64((*uint64)(unsafe.Pointer(&b.buffer[offset]))))
}

func (b *AtomicBuffer) PutInt64Ordered(offset int, value int64) {
    atomic.StoreUint64((*uint64)(unsafe.Pointer(&b.buffer[offset])), uint64(value))
}
```

### EWMA RTT Formula

PathManager uses the RFC 6298 EWMA formula for smoothed RTT:

```
sRTT  = (1 - α) * sRTT  + α * rttSample     α = 0.125 (1/8)
rttVar = (1 - β) * rttVar + β * |sRTT - rttSample|  β = 0.25 (1/4)
```

On the first sample, `sRTT = rttSample`. All values stored in nanoseconds as `int64`. The EWMA update is performed under no lock — the result is stored via `atomic.StoreInt64`.

### Adaptive Pool Learner Heuristics

`AdaptivePoolLearner.Evaluate(metrics []ProbeRecord)` is a pure function (no I/O, no side effects) that returns a `PoolSizeRecommendation`. Decision logic:

**Add** (open a new connection) when:
- Current pool size < `maxPoolSize`, AND
- Mean RTT improves by > 10% comparing current pool vs current pool + 1 projection (using probe history of the best existing connection as a proxy for what an additional connection could achieve)

**Remove** (close the LRU connection) when:
- Current pool size > `minPoolSize`, AND
- Loss rate < 0.1% (1000 parts-per-million) across all connections, AND
- No RTT benefit from additional connections observed for > 60 s (stability window)

**Hold** (no change) in all other cases, including:
- At `minPoolSize` when Remove conditions are met (cannot go below minimum)
- At `maxPoolSize` when Add conditions are met (cannot exceed maximum)
- Insufficient data (evaluation window not yet full)

### Channel URI Format

```
hs:quic?endpoint=host:port|pool=4|cc=bbrv3
```

- `hs` — scheme, always `hs` (Hyperspace)
- `quic` — transport, always `quic` in v1
- `endpoint=host:port` — required; the peer address
- `pool=N` — optional; pool size, default 4
- `cc=algo` — optional; congestion control algorithm (`cubic`, `bbr`, `bbrv3`, `drl`), default `bbrv3`

Parameters are separated by `|` not `&` (to avoid shell quoting issues in config files and environment variables). The parser in `pkg/channel` returns `ErrMissingEndpoint` if `endpoint` is absent, `ErrInvalidPoolSize` if `pool` is not a positive integer, and `ErrUnknownScheme` if the scheme is not `hs`.

---

## Domain Constraints and Invariants

| Constraint | Description | Enforcement |
|---|---|---|
| No mutex on hot paths | Log buffer appender, ring buffer read/write, counter updates must use `sync/atomic` | golangci-lint + code review |
| mmap files at 0600 | World-readable mmap files are a security defect | Security Evaluator; `TestMmap_FilePermissions` |
| TLS 1.3 minimum | All QUIC connections require `tls.VersionTLS13`; no downgrade | Security Evaluator; TLS config check in CI |
| Frame length validated before read | OOB read risk if `frameLen` not bounded by max MTU before payload slice | Code review; `gosec` |
| CGO restricted | CGO permitted only in `pkg/cc/drl` and `pkg/ipc/memmap` | ADR-003; Architecture Evaluator |
| Goroutine lifecycle must be clean | All goroutines terminate on ctx cancellation | `goleak` in all driver tests |
| Embedded driver in tests | Integration tests use `embedded.Driver`, not a real hsd subprocess | Code review; CI fast-path |
| No `log.Printf` in production | `log/slog` with structured fields only | golangci-lint revive rule |

---

## Terminology

| Term | Definition |
|---|---|
| hsd | Hyperspace Driver daemon — the out-of-process Go binary that manages all QUIC connections |
| Term | One of the three equal-sized segments of a log buffer; the unit of rotation |
| Active Term | The term currently being written to by publishers |
| CNC | Command-and-Control mmap region; contains the MPSC ring buffer, broadcast channel, and heartbeat timestamp |
| SVID | SPIFFE Verifiable Identity Document — the X.509 certificate issued by SPIRE Agent |
| Arbitrator | Component that selects which QUIC connection to use for each send batch (LowestRTT, Hybrid, etc.) |
| ProbeRecord | Per-connection struct storing EWMA RTT, loss rate, throughput — updated atomically by PathManager |
| PoolSnapshot | Atomic pointer to a point-in-time copy of the live connection list; updated by PoolManager |
| ECMP | Equal-Cost Multipath — AWS EC2 network feature that routes different flows across different physical paths |
| ErrBackPressure | Sentinel error returned by `Appender.Append` and `Publication.Offer` when the log buffer is full |
| Image | The receive-side log buffer backing a Subscription; written by Receiver, read by subscriber via `Poll` |
| FragmentHandler | `func(buf []byte, header *logbuffer.Header)` — the subscriber callback invoked by `Subscription.Poll` |
