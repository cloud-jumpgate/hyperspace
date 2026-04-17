# Hyperspace — Specification

**Version:** 0.1.0
**Status:** Draft
**Author:** Harness Architect
**Approved By:** —
**Last Updated:** 2026-04-17

---

## 1. Purpose

Hyperspace is a high-performance, Go-native publish/subscribe messaging platform built for latency-sensitive distributed systems on AWS EC2. It delivers sub-100-microsecond same-AZ P99 latency at one million messages per second by separating the client hot path (shared-memory log buffers, zero system calls) from the network transport (an out-of-process driver daemon running Multi-QUIC connections with adaptive path intelligence). Engineers who need reliable, ordered, high-throughput messaging between EC2 services — without the JVM overhead of Aeron or the operational complexity of Kafka for latency-critical paths — deploy Hyperspace as infrastructure alongside their services.

---

## 2. Stakeholders

| Role | Name / Team | Concern |
|---|---|---|
| Platform Engineering | cloud-jumpgate | Correctness, performance, operational burden |
| Service Engineers | Consumers of pkg/client | Simple, well-documented API; stable Go module |
| Infrastructure / SRE | cloud-jumpgate ops | Runbooks, observability, cert rotation |
| Security | cloud-jumpgate security | mTLS, SPIFFE identity, no secrets in code |
| CTO | Technical decision-maker | Architecture fitness, cost, velocity |

---

## 3. Functional Requirements

### 3.1 Core Features

| ID | Feature | Description | Priority | Sprint |
|---|---|---|---|---|
| F-001 | Log Buffer | Three-term mmap'd ring buffer: atomic appender, reader, term management | P0 | S1 |
| F-002 | Ring Buffers and IPC | SPSC and MPSC ring buffers; broadcast transmitter/receiver; mmap utilities | P0 | S1 |
| F-003 | QUIC Transport Adapter | quic-go wrapper implementing Hyperspace connection interface; TLS config | P0 | S2 |
| F-004 | Multi-QUIC Connection Pool | N concurrent connections per peer; pool data structure and lifecycle | P0 | S2 |
| F-005 | Connection Arbitrator | Strategy-based connection selection: LowestRTT, LeastOutstanding, Hybrid, Sticky, Random | P0 | S2 |
| F-006 | Path Manager + Latency Probes | PING/PONG probe frames; per-connection RTT/loss/throughput tracking | P1 | S4 |
| F-007 | Adaptive Pool Learner | Latency-loss correlation model; automatic pool size adjustment | P1 | S5 |
| F-008 | Driver Agents (Conductor/Sender/Receiver) | Three cooperating goroutines: control plane, outbound data plane, inbound data plane | P0 | S3 |
| F-009 | Pool Manager Agent | QUIC connection lifecycle management; certificate rotation via SPIRE | P0 | S5 |
| F-010 | Client Library | Public Go API: Client, Publication, Subscription, Image; channel URI parsing | P0 | S6 |
| F-011 | Congestion Control | CUBIC, BBR, BBRv3 algorithms; DRL controller via ONNX Runtime (CGO) | P1 | S7 |
| F-012 | Observability | Atomic named counters (cnc.dat); structured events; OTel traces/metrics; hyperspace-stat CLI | P1 | S8 |
| F-013 | AWS Integration | Cloud Map discovery; SSM configuration; Secrets Manager certificate fetch | P1 | S9 |
| F-014 | SPIFFE/SPIRE Identity | Workload identity via SPIRE workload API; X.509 SVID fetch and rotation | P0 | S9 |

### 3.2 Feature Detail

---

#### F-001: Log Buffer

**As a** Sender agent
**I want to** read messages from a shared-memory log buffer without system calls
**So that** the publisher's Offer() hot path has zero serialisation overhead and the Sender can drain at wire speed

**Acceptance criteria:**
- [ ] `pkg/logbuffer` package compiles with zero warnings under `golangci-lint`
- [ ] `LogBuffer` supports three-term rotation: when the active term is full, the writer atomically rotates to the next clean term
- [ ] `Appender.Append([]byte) (int64, error)` performs a single atomic CAS on the tail counter; no mutex required
- [ ] `Reader.Poll(handler FragmentHandler, limit int) int` reads up to `limit` messages from the current term without copying bytes into a new buffer
- [ ] Frame header format: `frameLength int32 | flags uint8 | type uint8 | streamId int32 | sessionId int32 | reservedValue int64` (24 bytes total)
- [ ] All log buffer files created with `os.OpenFile(..., os.O_RDWR|os.O_CREATE, 0600)` — not world-readable
- [ ] `pkg/logbuffer` test coverage ≥ 90%
- [ ] Benchmark: `BenchmarkAppender` demonstrates ≥ 10 million appends/second on a single core (informational; not a gate)

**Out of scope for this feature:**
- Network transport — log buffer is local shared memory only
- Term recycling strategy beyond three-term rotation

---

#### F-002: Ring Buffers and IPC

**As a** Conductor agent
**I want to** receive commands from the client library via a lock-free ring buffer
**So that** client-to-driver IPC does not require system calls or kernel involvement

**Acceptance criteria:**
- [ ] `pkg/ipc/ringbuffer` implements `SPSCRingBuffer` (single-producer, single-consumer) with `Write([]byte) error` and `Read() ([]byte, error)`
- [ ] `pkg/ipc/ringbuffer` implements `MPSCRingBuffer` (multi-producer, single-consumer) safe for concurrent writers using `sync/atomic` — no mutex
- [ ] `pkg/ipc/broadcast` implements `BroadcastTransmitter` and `BroadcastReceiver` for one-to-many event distribution
- [ ] `pkg/ipc/memmap` provides `Map(path string, size int64) ([]byte, error)` and `Unmap([]byte) error` wrappers around `syscall.Mmap`/`syscall.Munmap`
- [ ] All ring buffer reads/writes are atomic-safe under concurrent access — verified by `go test -race`
- [ ] `pkg/ipc` test coverage ≥ 90%

**Out of scope for this feature:**
- Cross-host IPC — these ring buffers are intra-host shared memory only

---

#### F-003: QUIC Transport Adapter

**As a** Sender agent
**I want to** send frame batches over a QUIC connection using a simple Go interface
**So that** the Sender does not depend on quic-go types directly and the transport is swappable in tests

**Acceptance criteria:**
- [ ] `pkg/transport/quic` defines and implements the `Connection` interface: `Send([]Frame) error`, `Recv() (Frame, error)`, `Close() error`, `RTT() time.Duration`, `RemoteAddr() net.Addr`
- [ ] TLS configuration enforces `MinVersion: tls.VersionTLS13` — no downgrade possible
- [ ] `Dial(ctx context.Context, addr string, tlsCfg *tls.Config) (Connection, error)` returns a connected `Connection`
- [ ] `Listen(addr string, tlsCfg *tls.Config) (Listener, error)` accepts incoming QUIC connections
- [ ] `pkg/transport/quic` test coverage ≥ 90%
- [ ] Integration test: two in-process QUIC endpoints exchange 1000 frames; all frames received in order with correct content
- [ ] QUIC connection parameters set: `MaxIncomingStreams: 4096`, `KeepAlivePeriod: 5s`

**Out of scope for this feature:**
- Multi-QUIC pool management — that is F-004
- SPIFFE identity loading — that is F-014

---

#### F-004: Multi-QUIC Connection Pool

**As a** Sender agent
**I want to** have N concurrent QUIC connections to a peer available in a pool
**So that** ECMP path diversity is exploited and a single slow connection does not block throughput

**Acceptance criteria:**
- [ ] `pkg/transport/pool` implements `Pool` struct with fields: `conns []*poolEntry`, `mu sync.RWMutex`, `target int` (desired pool size)
- [ ] `Pool.Add(conn Connection)` adds a connection to the pool
- [ ] `Pool.Remove(id string)` removes a connection by ID and closes it
- [ ] `Pool.Connections() []Connection` returns a snapshot of live connections
- [ ] `Pool.Len() int` returns the current number of live connections
- [ ] Pool supports concurrent reads without blocking — only writes take the mutex
- [ ] `pkg/transport/pool` test coverage ≥ 90%

**Out of scope for this feature:**
- Pool lifecycle (opening connections) — that is Pool Manager (F-009)
- Adaptive pool sizing — that is Adaptive Pool Learner (F-007)

---

#### F-005: Connection Arbitrator

**As a** Sender agent
**I want to** select the best QUIC connection from the pool for each send batch
**So that** latency is minimised and load is spread across connections based on the configured strategy

**Acceptance criteria:**
- [ ] `pkg/transport/arbitrator` defines `Strategy` enum: `LowestRTT`, `LeastOutstanding`, `Hybrid`, `Sticky`, `Random`
- [ ] `Arbitrator.Select(strategy Strategy) (Connection, error)` returns a connection or `ErrEmptyPool` if no connections available
- [ ] `LowestRTT`: selects the connection with the minimum `RTT()` value
- [ ] `LeastOutstanding`: selects the connection with the fewest outstanding unacknowledged frames
- [ ] `Hybrid`: weighted combination of RTT score and outstanding-frame score (weights configurable, default 0.7 RTT / 0.3 outstanding)
- [ ] `Sticky`: returns the last selected connection unless it has failed — falls back to `LowestRTT`
- [ ] `Random`: uniform random selection across all pool connections
- [ ] `pkg/transport/arbitrator` test coverage ≥ 90%
- [ ] Benchmark: `BenchmarkArbitrator_LowestRTT` with 8-connection pool completes `Select` in < 500 ns (informational)

**Out of scope for this feature:**
- Updating connection RTT/outstanding metrics — that is Path Manager (F-006)

---

#### F-006: Path Manager and Latency Probes

**As a** hsd daemon
**I want to** continuously measure per-connection RTT, packet loss, and throughput
**So that** the Arbitrator has accurate, current metrics for connection selection

**Acceptance criteria:**
- [ ] `pkg/driver/pathmgr` implements `PathManager` with a `Run(ctx context.Context)` method that starts the probe loop
- [ ] PING frames sent on each pool connection at configurable interval (default 100 ms)
- [ ] PONG frames received and matched to PING by sequence number; RTT computed as `pongReceivedAt - pingSentAt`
- [ ] `pkg/transport/probes` defines `PingFrame` and `PongFrame` wire formats with sequence number field
- [ ] Per-connection `ProbeRecord` updated atomically: `RTTSampleEWMA`, `LossRate`, `ThroughputBytesPerSec`
- [ ] If PONG not received within `probeTimeoutMs` (default 500 ms), connection RTT set to `math.MaxInt64` (excluded from LowestRTT selection)
- [ ] `pkg/driver/pathmgr` test coverage ≥ 90%

**Out of scope for this feature:**
- Adaptive pool sizing decisions — that is F-007

---

#### F-007: Adaptive Pool Learner

**As a** Pool Manager
**I want to** automatically right-size the connection pool based on latency-loss correlation
**So that** the pool is not over-provisioned (wasting file descriptors) or under-provisioned (bottlenecking throughput)

**Acceptance criteria:**
- [ ] `pkg/driver/poolmgr` implements `AdaptivePoolLearner` with `Evaluate(metrics []ProbeRecord) PoolSizeRecommendation`
- [ ] Learner increases pool size when: mean RTT improves by > 10% with each additional connection (up to `maxPoolSize`)
- [ ] Learner decreases pool size when: loss rate is < 0.1% and additional connections show no RTT benefit for > 60 seconds
- [ ] `PoolSizeRecommendation` has fields: `TargetSize int`, `Reason string`
- [ ] Pool size changes bounded to `[minPoolSize, maxPoolSize]` (both configurable via SSM or URI)
- [ ] `pkg/driver/poolmgr` AdaptivePoolLearner test coverage ≥ 90%

**Out of scope for this feature:**
- Executing the pool resize — that is Pool Manager agent (F-009)

---

#### F-008: Driver Agents — Conductor, Sender, Receiver

**As a** Hyperspace platform
**I want to** run three cooperating driver agents that handle client commands and move data between log buffers and the QUIC transport
**So that** the client library hot path is decoupled from all network I/O

**Acceptance criteria:**
- [ ] `pkg/driver/conductor` implements `Conductor` with `Run(ctx context.Context)` that processes client commands from the MPSC ring buffer
- [ ] Conductor handles commands: `AddPublication`, `RemovePublication`, `AddSubscription`, `RemoveSubscription`, `ClientKeepalive`
- [ ] `pkg/driver/sender` implements `Sender` with `Run(ctx context.Context)` that busy-polls active log buffer terms and sends frames via the Arbitrator
- [ ] Sender reads frame batches (up to `maxBatchSize` frames per iteration) to amortise Arbitrator overhead
- [ ] `pkg/driver/receiver` implements `Receiver` with `Run(ctx context.Context)` that accepts frames from QUIC connections and writes to image log buffers
- [ ] All three agents shut down cleanly when `ctx` is cancelled — no goroutine leak (verified with `goleak` in tests)
- [ ] `cmd/hsd/main.go` starts all five agents (`Conductor`, `Sender`, `Receiver`, `PathManager`, `PoolManager`) and blocks until SIGTERM/SIGINT
- [ ] `pkg/driver` test coverage ≥ 85% (embedded driver integration tests count toward this total)

**Out of scope for this feature:**
- Path Manager and Pool Manager — those are F-006 and F-009

---

#### F-009: Pool Manager Agent

**As a** hsd daemon
**I want to** manage the lifecycle of QUIC connections in the pool, including opening, closing, and certificate rotation
**So that** the pool always has the target number of healthy, authenticated connections

**Acceptance criteria:**
- [ ] `pkg/driver/poolmgr` implements `PoolManager` with `Run(ctx context.Context)` that manages connection lifecycle
- [ ] On startup, Pool Manager opens `targetPoolSize` QUIC connections to each configured peer
- [ ] Pool Manager watches the SPIRE workload API for SVID rotation events (via `pkg/identity`) and triggers cert rotation on affected connections
- [ ] Cert rotation uses QUIC connection migration (new connection on new cert, drain old, close old) — zero message loss during rotation
- [ ] Pool Manager integrates `AdaptivePoolLearner` recommendations to resize the pool
- [ ] Failed connections are replaced within `reconnectDelayMs` (default 500 ms) with exponential backoff up to 30 s
- [ ] `pkg/driver/poolmgr` test coverage ≥ 85%

**Out of scope for this feature:**
- Probe execution — that is Path Manager (F-006)

---

#### F-010: Client Library

**As a** service engineer
**I want to** publish and subscribe to Hyperspace channels using a simple Go API
**So that** I can integrate Hyperspace into my service without understanding the driver internals

**Acceptance criteria:**
- [ ] `pkg/client` exports: `Client`, `Publication`, `Subscription`, `Image`, `FragmentHandler` (func type)
- [ ] `NewClient(ctx context.Context, driverPath string) (*Client, error)` connects to hsd via mmap CNC file
- [ ] `client.AddPublication(ctx, channel string, streamId int32) (*Publication, error)` sends `AddPublication` command to Conductor and returns on acknowledgement
- [ ] `pub.Offer(buf []byte) (int64, error)` writes to log buffer; returns position on success, `ErrBackPressure` when buffer full
- [ ] `client.AddSubscription(ctx, channel string, streamId int32) (*Subscription, error)` registers subscription with Conductor
- [ ] `sub.Poll(handler FragmentHandler, limit int) int` returns number of fragments dispatched; `handler` receives `(buf []byte, header *Header)`
- [ ] `pkg/channel` parses channel URIs: `hs:quic?endpoint=host:port|pool=N|cc=algo` — returns `*ChannelParams` or error on malformed URI
- [ ] Client detects driver liveness via CNC heartbeat timestamp; returns `ErrDriverUnavailable` if heartbeat age > `clientLivenessTimeout`
- [ ] `pkg/client` test coverage ≥ 90%
- [ ] Example `examples/basic_publisher/main.go` and `examples/basic_subscriber/main.go` compile and run end-to-end in CI

**Out of scope for this feature:**
- Driver implementation — F-008 and F-009

---

#### F-011: Congestion Control

**As a** QUIC transport layer
**I want to** plug in different congestion control algorithms per connection
**So that** I can tune throughput vs. latency trade-offs and support a learned DRL controller

**Acceptance criteria:**
- [ ] `pkg/cc` defines `CongestionController` interface: `OnAck(ackedBytes int64, rtt time.Duration)`, `OnLoss(lostBytes int64)`, `CongestionWindow() int64`
- [ ] `pkg/cc/cubic` implements CUBIC algorithm conforming to RFC 8312
- [ ] `pkg/cc/bbr` implements BBR (v1) algorithm
- [ ] `pkg/cc/bbrv3` implements BBRv3 based on the Google proposal
- [ ] `pkg/cc/drl` implements `DRLController` that loads an ONNX model and runs inference: input `[rtt_ms, loss_rate, cwnd, outstanding]`, output `cwnd_delta`
- [ ] DRL controller falls back to BBRv3 if ONNX model fails to load or inference returns error
- [ ] `pkg/cc` test coverage ≥ 85% (DRL integration test uses a bundled minimal ONNX model)
- [ ] `golangci-lint` passes with CGO enabled

**Out of scope for this feature:**
- DRL training pipeline — this is a separate system; only inference is in scope

---

#### F-012: Observability

**As a** platform engineer
**I want to** see real-time counters, structured events, and distributed traces from hsd
**So that** I can diagnose latency spikes, identify slow connections, and audit message flow

**Acceptance criteria:**
- [ ] `pkg/counters` implements named `int64` counters backed by an atomic array in `cnc.dat`; counter names registered at startup
- [ ] Minimum counters: `bytes_sent`, `bytes_received`, `frames_sent`, `frames_received`, `offer_backpressure_count`, `poll_count`, `probe_rtt_us` (per connection), `pool_size`
- [ ] `cmd/hyperspace-stat/main.go` reads `cnc.dat` and prints a formatted counter table; refreshes every second with `--watch` flag
- [ ] `pkg/obs` wraps OpenTelemetry SDK: `InitTracer(serviceName string, endpoint string)`, `StartSpan(ctx, name string) (context.Context, trace.Span)`
- [ ] CloudWatch metric export: counters flushed to CloudWatch namespace `Hyperspace/` every 60 seconds when `HYPERSPACE_CLOUDWATCH_NAMESPACE` env var is set
- [ ] Structured log output via `log/slog` with fields: `session_id`, `stream_id`, `conn_id`, `rtt_us`, `msg` — no unstructured log.Printf in production code paths
- [ ] `pkg/counters` and `pkg/obs` test coverage ≥ 85%

**Out of scope for this feature:**
- Grafana dashboards — operational concern outside this spec

---

#### F-013: AWS Integration

**As a** hsd daemon
**I want to** discover peer endpoints from Cloud Map, load configuration from SSM, and fetch TLS certificates from Secrets Manager
**So that** the daemon requires no static configuration files in production

**Acceptance criteria:**
- [ ] `pkg/discovery` implements `CloudMapResolver` with `Resolve(ctx, serviceName string) ([]string, error)` returning `host:port` endpoints
- [ ] `pkg/config` implements `SSMConfig` with `Get(ctx, paramPath string) (string, error)` using AWS SDK Parameter Store API
- [ ] `pkg/identity` implements `SecretsManagerCertLoader` that fetches a PEM-encoded cert+key bundle from Secrets Manager by ARN
- [ ] All AWS SDK calls use IAM role credentials (no hardcoded keys) — verified by `bandit` equivalent (`gosec`)
- [ ] Localstack integration tests: Cloud Map resolver test, SSM config test, Secrets Manager cert test — all run in CI via `docker-compose`
- [ ] `pkg/discovery`, `pkg/config`, `pkg/identity` test coverage ≥ 80%

**Out of scope for this feature:**
- SPIFFE/SPIRE identity — that is F-014 (separate from cert loading)

---

#### F-014: SPIFFE/SPIRE Identity

**As a** hsd daemon
**I want to** obtain short-lived X.509 SVIDs from the SPIRE Agent and rotate them automatically
**So that** every QUIC connection is mutually authenticated with workload identity and certificates never expire undetected

**Acceptance criteria:**
- [ ] `pkg/identity` implements `SPIFFEIdentity` with `FetchSVID(ctx context.Context) (*tls.Certificate, *x509.CertPool, error)` via the SPIRE workload API Unix socket
- [ ] `SPIFFEIdentity.Watch(ctx context.Context, onChange func(*tls.Certificate, *x509.CertPool))` subscribes to SVID rotation events
- [ ] SVID fetched from SPIRE Agent at `unix:///run/spire/agent.sock` by default (path configurable via SSM)
- [ ] `pkg/identity` test coverage ≥ 85%
- [ ] Integration test: mock SPIRE Agent (using `go-spiffe` test utilities) issues a test SVID; `FetchSVID` returns a valid `*tls.Certificate`

**Out of scope for this feature:**
- SPIRE Server deployment — operational concern
- AWS Secrets Manager cert loading — that is F-013

---

## 4. Non-Functional Requirements

| ID | Category | Requirement | Target | Measurement |
|---|---|---|---|---|
| NFR-001 | Latency | Same-AZ P99 RTT at 1 M msg/s | < 100 µs | EC2 benchmark harness (`cmd/hyperspace-probe`) |
| NFR-002 | Latency | IPC RTT (same host, embedded driver) | < 1 µs | Microbenchmark in `pkg/logbuffer` |
| NFR-003 | Throughput | Peak sustained throughput | ≥ 1.5 M msg/s | EC2 benchmark harness |
| NFR-004 | Availability | Driver process restart recovery | < 500 ms | Integration test |
| NFR-005 | Security | All QUIC connections use mTLS | 100% | Security Evaluator audit + integration test |
| NFR-006 | Security | TLS minimum version | TLS 1.3 | Automated TLS config check in CI |
| NFR-007 | Security | No hardcoded credentials in source | Zero | `gosec` scan in CI |
| NFR-008 | Observability | All critical counters updated within 1 µs of event | < 1 µs counter write | Microbenchmark |
| NFR-009 | Test Coverage | Minimum line coverage across all packages | ≥ 85% | `make test` coverage report |
| NFR-010 | Build | `golangci-lint run` exits 0 | Zero lint errors | CI gate |
| NFR-011 | Build | `govulncheck ./...` exits 0 | Zero known vulnerabilities | CI gate before deploy |

---

## 5. Constraints

| Constraint | Description | Source |
|---|---|---|
| Language | Go 1.26 | Architecture decision (ADR-002) |
| Module | `github.com/cloud-jumpgate/hyperspace` | GitHub repository |
| Transport | QUIC via quic-go | ADR-002 |
| Identity | SPIFFE/SPIRE | ADR-004 |
| Deployment | AWS EC2 c7gn.4xlarge | Infrastructure constraint |
| CC model format | ONNX | ADR-003 |
| No CGO in hot path | DRL CGO calls only at inference boundary | Performance constraint |
| mmap files | 0600 permissions | Security requirement |

---

## 6. Out of Scope

The following are explicitly out of scope for Hyperspace v1. They may be addressed in future phases.

- **Persistence / replay.** Hyperspace is a live messaging system; it does not persist messages to disk for replay (that is a Kafka use case).
- **Multi-tenancy.** A single hsd instance serves one trust domain. Multi-tenant isolation is a future concern.
- **Windows support.** mmap and UNIX socket APIs are used; Windows is not a target platform.
- **DRL training pipeline.** Training the ONNX model is a separate system; only inference is in scope.
- **Grafana / Prometheus dashboards.** Metrics are exported to CloudWatch; dashboard design is an operational concern.
- **Hyperspace Broker.** A centralised broker / topic registry is a potential future component; v1 is peer-to-peer.
- **Message schema validation.** Payloads are opaque byte slices; schema enforcement is the application's responsibility.
- **Cross-region routing.** v1 targets same-region (same-AZ and cross-AZ) deployments.

---

## 7. Integrations

| System | Direction | Protocol | Auth | Notes |
|---|---|---|---|---|
| SPIRE Agent | Outbound (fetch SVIDs) | Unix socket (gRPC) | SPIFFE workload API | `pkg/identity` |
| AWS Cloud Map | Outbound (resolve peers) | HTTPS | IAM role | `pkg/discovery` |
| AWS SSM Parameter Store | Outbound (fetch config) | HTTPS | IAM role | `pkg/config` |
| AWS Secrets Manager | Outbound (fetch certs) | HTTPS | IAM role | `pkg/identity` |
| AWS CloudWatch | Outbound (push metrics) | HTTPS | IAM role | `pkg/obs` |
| OpenTelemetry Collector | Outbound (push traces) | gRPC (OTLP) | None / mTLS | `pkg/obs` |
| quic-go | In-process | Go library | — | `pkg/transport/quic` |

---

## 8. Security Requirements

- [x] All QUIC connections require mTLS — no anonymous connections permitted
- [x] TLS minimum version is 1.3 — enforced in quic-go TLS config
- [x] All secrets loaded from AWS Secrets Manager or SPIRE — never from environment variables or source code
- [x] mmap region files created with 0600 permissions
- [x] Frame length validated before buffer read (prevents OOB read)
- [x] `govulncheck ./...` runs in CI before every build artifact is published
- [x] `gosec` scan in CI — zero high-severity findings required to pass
- [x] Security Evaluator PASS required before S9 (AWS Integration) is marked passing

---

## 9. Open Questions

| ID | Question | Owner | Due | Status |
|---|---|---|---|---|
| Q-001 | Should Hyperspace support QUIC 0-RTT for reconnection? 0-RTT has replay attack implications at the messaging layer. | Harness Architect | S2 end | Open |
| Q-002 | What is the maximum supported message size? Fragmentation at the log buffer layer needs a defined limit. | Backend Engineer | S1 end | Open |
| Q-003 | Is ONNX Runtime's CGO interface stable enough for production, or should we pin to a specific ONNX Runtime version? | Backend Engineer | S7 start | Open |
| Q-004 | Should `Offer()` support non-blocking (return immediately with `ErrBackPressure`) and blocking (spin-wait with timeout) modes? | API design | S6 start | Open |

---

## 10. Revision History

| Version | Date | Change | Author |
|---|---|---|---|
| 0.1.0 | 2026-04-17 | Initial specification — 14 features, 10 sprints | Harness Architect |
