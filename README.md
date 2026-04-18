# Hyperspace

Hyperspace is a Go pub/sub messaging platform built for ultra-low-latency workloads on AWS EC2. It is inspired by the Aeron messaging library and transports data over a pool of concurrent QUIC connections (Multi-QUIC) rather than a single stream. Five cooperating driver agents — Conductor, Sender, Receiver, PathManager, and PoolManager — handle the control plane, data plane, path probing, and connection lifecycle. Applications interact through a clean `Client / Publication / Subscription` API that can run in embedded mode (all agents in-process, no daemon required) or external mode (connecting to a running `hsd` daemon via shared memory).

---

## Architecture

```
Application
    |
    v
+-------------------+
|   pkg/client      |  Client / Publication / Subscription / Image
+-------------------+
    |         ^
    | cmd     | broadcast
    v         |
+-----------------------------------------------+
|                   Driver                       |
|  +------------+  +---------+  +------------+  |
|  | Conductor  |  | Sender  |  | Receiver   |  |
|  +------------+  +---------+  +------------+  |
|  +-------------+  +-----------+               |
|  | PathManager |  | PoolMgr   |               |
|  +-------------+  +-----------+               |
+-----------------------------------------------+
    |
    v
+-----------------------------------------------+
|           Multi-QUIC Connection Pool          |
|   conn-0  conn-1  conn-2  conn-3              |
+-----------------------------------------------+
    |
    v
 Remote Peer (hsd or embedded driver)
```

---

## Quick Start

Run the embedded example — no daemon, no infrastructure required:

```bash
go run ./examples/embedded/
```

The embedded example creates one publisher and one subscriber in the same process and exchanges 10 messages over a loopback channel.

---

## Channel URI Format

Hyperspace channels are addressed with a URI scheme modelled after Aeron:

```
hs:quic?endpoint=host:port|pool=4|cc=bbrv3
```

| Parameter  | Description                                        | Default   |
|------------|----------------------------------------------------|-----------|
| `endpoint` | Remote host and port                               | required  |
| `pool`     | Number of concurrent QUIC connections in the pool  | 4         |
| `cc`       | Congestion control algorithm (see table below)     | bbrv3     |

---

## Congestion Control

| Algorithm | Key | Notes |
|-----------|-----|-------|
| CUBIC     | `cubic`  | Standard TCP CUBIC port; good general-purpose baseline |
| BBRv1     | `bbr`    | Google BBR; better throughput under shallow buffers |
| BBRv3     | `bbrv3`  | Latest BBR iteration; recommended default |
| DRL       | `drl`    | Deep Reinforcement Learning controller via ONNX Runtime (CGO); requires ONNX model artifact at `HYPERSPACE_DRL_MODEL_PATH`; falls back to BBRv3 if model is absent |

---

## Configuration

All configuration is via environment variables. No config files are required for basic use.

| Variable                          | Description                                              | Default        |
|-----------------------------------|----------------------------------------------------------|----------------|
| `HYPERSPACE_TERM_LENGTH`          | Log buffer term length in bytes (must be power of two)   | `16777216` (16 MiB) |
| `HYPERSPACE_POOL_SIZE`            | Default QUIC connection pool size                        | `4`            |
| `HYPERSPACE_CC`                   | Default congestion control algorithm                     | `bbrv3`        |
| `HYPERSPACE_LOG_LEVEL`            | Structured log level (`debug`, `info`, `warn`, `error`)  | `info`         |
| `HYPERSPACE_DRL_MODEL_PATH`       | Path to ONNX model file for DRL congestion control       | (none)         |
| `HYPERSPACE_SPIRE_SOCKET`         | SPIRE workload API socket path                           | (none)         |
| `HYPERSPACE_AWS_REGION`           | AWS region for Cloud Map / SSM / Secrets Manager         | (none)         |
| `HYPERSPACE_CLOUDMAP_NAMESPACE`   | AWS Cloud Map namespace for peer discovery               | (none)         |
| `HYPERSPACE_SSM_PREFIX`           | SSM Parameter Store key prefix for runtime config        | (none)         |
| `HYPERSPACE_SECRETS_CERT_ARN`     | Secrets Manager ARN for TLS certificate bundle           | (none)         |
| `HYPERSPACE_OTEL_ENDPOINT`        | OpenTelemetry OTLP endpoint for traces and metrics       | (none)         |

---

## AWS Integrations

**Cloud Map** — `pkg/discovery`: resolves peer endpoints from an AWS Cloud Map service registry. Set `HYPERSPACE_CLOUDMAP_NAMESPACE` and `HYPERSPACE_AWS_REGION` to enable. Peer addresses are refreshed periodically by the PoolManager agent.

**SSM Parameter Store** — `pkg/config`: loads runtime configuration from SSM at startup. Keys are prefixed with `HYPERSPACE_SSM_PREFIX`. Useful for injecting environment-specific overrides without rebuilding the image.

**Secrets Manager** — `pkg/identity`: fetches TLS certificate bundles (PEM) from Secrets Manager at startup and on rotation events. Specify the ARN via `HYPERSPACE_SECRETS_CERT_ARN`. Certificates are passed to the QUIC transport layer with `tls.VersionTLS13` enforced.

---

## Identity

Hyperspace uses **SPIFFE/SPIRE** for workload identity. When `HYPERSPACE_SPIRE_SOCKET` is set, `pkg/identity` fetches an X.509 SVID from the SPIRE workload API at startup and on rotation. The SVID is used as the QUIC TLS certificate; mutual TLS between peers is enforced automatically. Production deployments require a running SPIRE agent on the host.

---

## Observability

**`hyperspace-stat`** — CLI tool that reads the in-memory counter array (`cnc.dat`) and prints a live table of named counters (bytes sent/received, messages offered, back-pressure events, RTT samples). Run:

```bash
hyperspace-stat
```

**OpenTelemetry** — When `HYPERSPACE_OTEL_ENDPOINT` is set, Hyperspace exports traces and metrics via OTLP (gRPC). Spans are created for publication offer, subscription poll, and path probe round-trips. Metrics include histogram distributions of offer latency and poll fragment counts.

---

## Performance Targets

These are aspirational targets validated on AWS EC2 `c7gn.4xlarge` (50 Gbps, Graviton3, same-AZ):

| Metric                          | Target      |
|---------------------------------|-------------|
| Same-AZ P50 RTT                 | < 30 µs     |
| Same-AZ P99 RTT                 | < 100 µs    |
| Throughput (1 KB messages)      | 1 M msg/s   |
| Throughput (8 KB messages)      | 10 Gbps     |
| Pool connection failover        | < 5 ms      |
| SVID rotation (SPIRE)           | < 1 s       |

EC2 benchmark validation uses `go test -bench=. -benchmem ./benchmarks/` as the starting point for profiling, followed by `hyperspace-probe` for end-to-end latency histograms.

---

## Building

```bash
# Build all binaries (hsd, hyperspace-stat)
make build

# Build the hsd Docker image
make docker-build

# Build individual binaries
make build-hsd
make build-stat
```

---

## Testing

```bash
# Full test suite with race detector
make test

# Tests with coverage report
make test-cover

# Performance benchmarks
make bench

# Lint
make lint

# Vulnerability scan
make vuln
```

---

## Sprint History

| Sprint | Name              | Delivered |
|--------|-------------------|-----------|
| S1     | Foundation        | Log buffer (three-term rotating, lock-free appender/reader), SPSC/MPSC ring buffers, broadcast IPC, mmap utilities |
| S2     | QUIC Transport    | quic-go adapter, Multi-QUIC connection pool, LowestRTT/LeastOutstanding/Hybrid/Sticky/Random arbitrators, TLS 1.3 enforced |
| S3     | Driver Core       | Conductor, Sender, Receiver agents; cooperative scheduling with IdleStrategy; `hsd` daemon binary boots and shuts down cleanly |
| S4     | Path Intelligence | PathManager agent; PING/PONG probe frames; per-connection atomic RTT, loss, and throughput tracking |
| S5     | Pool Intelligence | Adaptive Pool Learner (latency-loss correlation); PoolManager agent managing connection lifecycle and certificate rotation |
| S6     | Client Library    | Public `Client / Publication / Subscription / Image` API; channel URI parser; embedded and external driver modes; end-to-end examples |
| S7     | Congestion Control | CUBIC, BBRv1, BBRv3 implemented; DRL controller loads ONNX model via CGO and runs inference; BBRv3 fallback when model absent |
| S8     | Observability     | Named counter array (`cnc.dat`); structured `slog` events with correlation IDs; OTel traces/metrics; `hyperspace-stat` CLI |
| S9     | AWS + Identity    | Cloud Map peer discovery; SSM config loading; Secrets Manager certificate fetch; SPIFFE/SPIRE X.509 SVID fetch and rotation |
| S10    | CI/CD + Docs      | GitHub Actions CI pipeline (lint, test, build, vuln); `.golangci.yml`; Makefile; multi-stage Dockerfile; benchmark scaffolding; README |
