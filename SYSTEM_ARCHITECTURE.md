# Hyperspace — System Architecture

**Version:** 0.1.0
**Status:** Draft
**Last Updated:** 2026-04-17
**Author:** Harness Architect
**Approved By:** —

---

## 1. Overview

### 1.1 Purpose

Hyperspace is a Go-based high-performance publish/subscribe messaging platform that delivers sub-100-microsecond same-AZ P99 latency at 1 million messages per second. It takes its architectural DNA from Aeron — using shared-memory log buffers and a dedicated out-of-process driver daemon — but replaces Aeron's custom reliable-UDP transport with a Multi-QUIC connection pool, gaining QUIC's built-in TLS 1.3, stream multiplexing, and connection migration for free. Hyperspace is designed for latency-sensitive distributed systems deployed on AWS EC2, where message ordering, back-pressure, and microsecond-level observability are first-class requirements.

The system separates the client library (`pkg/client`) from the driver daemon (`hsd`). Client applications call `Offer()` and `Poll()` on shared-memory log buffers without system calls; the driver daemon moves data asynchronously over the QUIC transport layer. This architecture means client hot paths are NUMA-local memory operations — not network calls — and the driver can implement sophisticated path intelligence (RTT probing, adaptive pool sizing, DRL congestion control) without coupling that complexity to the application.

Hyperspace targets AWS EC2 c7gn.4xlarge instances with 50 Gbps networking, using AWS Cloud Map for peer discovery, AWS Secrets Manager for TLS certificate distribution, AWS SSM Parameter Store for runtime configuration, and SPIFFE/SPIRE for workload identity. The platform is designed to operate as infrastructure: deploy once, route all inter-service messaging through it, and let the adaptive pool learner right-size the connection pool based on observed latency and loss patterns.

### 1.2 System Context (C4 Level 1)

```excalidraw
{
  "type": "excalidraw",
  "version": 2,
  "source": "excalidraw",
  "elements": [
    {
      "id": "app-publisher",
      "type": "rectangle",
      "x": 40, "y": 160, "width": 140, "height": 70,
      "strokeColor": "#868e96", "backgroundColor": "#f1f3f5",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0, "opacity": 100, "angle": 0
    },
    {
      "id": "app-publisher-label",
      "type": "text",
      "x": 55, "y": 183,
      "text": "Publisher App\n[Go service]",
      "fontSize": 13, "fontFamily": 1,
      "strokeColor": "#495057", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 120, "height": 36
    },
    {
      "id": "hyperspace-system",
      "type": "rectangle",
      "x": 280, "y": 100, "width": 220, "height": 180,
      "strokeColor": "#1971c2", "backgroundColor": "#d0ebff",
      "fillStyle": "solid", "strokeWidth": 3, "roughness": 0, "opacity": 100, "angle": 0
    },
    {
      "id": "hyperspace-label",
      "type": "text",
      "x": 305, "y": 145,
      "text": "Hyperspace\n[Go / QUIC]\nPub/Sub Platform",
      "fontSize": 15, "fontFamily": 1,
      "strokeColor": "#1971c2", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 170, "height": 60
    },
    {
      "id": "app-subscriber",
      "type": "rectangle",
      "x": 620, "y": 160, "width": 140, "height": 70,
      "strokeColor": "#868e96", "backgroundColor": "#f1f3f5",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0, "opacity": 100, "angle": 0
    },
    {
      "id": "app-subscriber-label",
      "type": "text",
      "x": 633, "y": 183,
      "text": "Subscriber App\n[Go service]",
      "fontSize": 13, "fontFamily": 1,
      "strokeColor": "#495057", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 120, "height": 36
    },
    {
      "id": "aws-cloud-map",
      "type": "rectangle",
      "x": 280, "y": 340, "width": 140, "height": 60,
      "strokeColor": "#f08c00", "backgroundColor": "#fff3bf",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0, "opacity": 100, "angle": 0
    },
    {
      "id": "aws-cloud-map-label",
      "type": "text",
      "x": 295, "y": 357,
      "text": "AWS Cloud Map\n[Discovery]",
      "fontSize": 12, "fontFamily": 1,
      "strokeColor": "#f08c00", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 120, "height": 36
    },
    {
      "id": "spire",
      "type": "rectangle",
      "x": 440, "y": 340, "width": 140, "height": 60,
      "strokeColor": "#f08c00", "backgroundColor": "#fff3bf",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0, "opacity": 100, "angle": 0
    },
    {
      "id": "spire-label",
      "type": "text",
      "x": 460, "y": 357,
      "text": "SPIFFE/SPIRE\n[Identity]",
      "fontSize": 12, "fontFamily": 1,
      "strokeColor": "#f08c00", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 110, "height": 36
    },
    {
      "id": "arrow-pub-to-hs",
      "type": "arrow",
      "x": 180, "y": 195, "width": 100, "height": 0,
      "strokeColor": "#495057", "backgroundColor": "transparent",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0, "opacity": 100, "angle": 0,
      "points": [[0, 0], [100, 0]],
      "startBinding": {"elementId": "app-publisher", "gap": 5, "focus": 0},
      "endBinding": {"elementId": "hyperspace-system", "gap": 5, "focus": 0}
    },
    {
      "id": "arrow-hs-to-sub",
      "type": "arrow",
      "x": 500, "y": 195, "width": 120, "height": 0,
      "strokeColor": "#495057", "backgroundColor": "transparent",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0, "opacity": 100, "angle": 0,
      "points": [[0, 0], [120, 0]],
      "startBinding": {"elementId": "hyperspace-system", "gap": 5, "focus": 0},
      "endBinding": {"elementId": "app-subscriber", "gap": 5, "focus": 0}
    },
    {
      "id": "arrow-hs-to-discovery",
      "type": "arrow",
      "x": 390, "y": 280, "width": 0, "height": 60,
      "strokeColor": "#f08c00", "backgroundColor": "transparent",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0, "opacity": 100, "angle": 0,
      "points": [[0, 0], [0, 60]],
      "startBinding": {"elementId": "hyperspace-system", "gap": 5, "focus": 0},
      "endBinding": {"elementId": "aws-cloud-map", "gap": 5, "focus": 0}
    }
  ],
  "appState": {"viewBackgroundColor": "#ffffff"}
}
```

### 1.3 Key Design Principles

- **Zero-copy hot path.** Client `Offer()` writes directly to mmap'd log buffers. No serialisation, no syscall, no heap allocation on the critical path.
- **Driver isolation.** The `hsd` daemon owns all network I/O. Client library failures cannot corrupt the driver; driver restarts do not require application restart.
- **Path diversity over channel bonding.** Multiple independent QUIC connections per peer pair give ECMP-level path diversity without kernel bonding. The Arbitrator selects the best connection per batch.
- **Observable by design.** Every internal counter is a named `int64` written atomically to `cnc.dat`. External tools (`hyperspace-stat`) read counters without entering the driver.
- **Adaptive, not static.** The Adaptive Pool Learner right-sizes the connection pool based on empirical latency-loss correlation. The DRL congestion controller learns per-path optima at runtime.

---

## 2. Components (C4 Level 2)

### 2.1 Container Diagram

```excalidraw
{
  "type": "excalidraw",
  "version": 2,
  "source": "excalidraw",
  "elements": [
    {
      "id": "client-lib",
      "type": "rectangle",
      "x": 40, "y": 80, "width": 160, "height": 90,
      "strokeColor": "#2f9e44", "backgroundColor": "#d3f9d8",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0, "opacity": 100, "angle": 0
    },
    {
      "id": "client-lib-label",
      "type": "text",
      "x": 55, "y": 100,
      "text": "Client Library\npkg/client\n[Go in-process]",
      "fontSize": 12, "fontFamily": 1,
      "strokeColor": "#2f9e44", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 140, "height": 54
    },
    {
      "id": "logbuffer",
      "type": "rectangle",
      "x": 240, "y": 80, "width": 160, "height": 90,
      "strokeColor": "#e03131", "backgroundColor": "#ffe3e3",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0, "opacity": 100, "angle": 0
    },
    {
      "id": "logbuffer-label",
      "type": "text",
      "x": 258, "y": 100,
      "text": "Log Buffer\npkg/logbuffer\n[mmap shared mem]",
      "fontSize": 12, "fontFamily": 1,
      "strokeColor": "#e03131", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 140, "height": 54
    },
    {
      "id": "hsd-daemon",
      "type": "rectangle",
      "x": 440, "y": 40, "width": 200, "height": 360,
      "strokeColor": "#1971c2", "backgroundColor": "#e7f5ff",
      "fillStyle": "solid", "strokeWidth": 2, "strokeStyle": "dashed",
      "roughness": 0, "opacity": 40, "angle": 0
    },
    {
      "id": "hsd-label",
      "type": "text",
      "x": 480, "y": 48,
      "text": "hsd daemon",
      "fontSize": 14, "fontFamily": 1,
      "strokeColor": "#1971c2", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 120, "height": 20
    },
    {
      "id": "conductor",
      "type": "rectangle",
      "x": 455, "y": 75, "width": 170, "height": 50,
      "strokeColor": "#2f9e44", "backgroundColor": "#d3f9d8",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0, "opacity": 100, "angle": 0
    },
    {
      "id": "conductor-label",
      "type": "text",
      "x": 475, "y": 90,
      "text": "Conductor [control]",
      "fontSize": 12, "fontFamily": 1,
      "strokeColor": "#2f9e44", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 150, "height": 20
    },
    {
      "id": "sender",
      "type": "rectangle",
      "x": 455, "y": 138, "width": 170, "height": 50,
      "strokeColor": "#2f9e44", "backgroundColor": "#d3f9d8",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0, "opacity": 100, "angle": 0
    },
    {
      "id": "sender-label",
      "type": "text",
      "x": 490, "y": 153,
      "text": "Sender [TX]",
      "fontSize": 12, "fontFamily": 1,
      "strokeColor": "#2f9e44", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 110, "height": 20
    },
    {
      "id": "receiver",
      "type": "rectangle",
      "x": 455, "y": 201, "width": 170, "height": 50,
      "strokeColor": "#2f9e44", "backgroundColor": "#d3f9d8",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0, "opacity": 100, "angle": 0
    },
    {
      "id": "receiver-label",
      "type": "text",
      "x": 488, "y": 216,
      "text": "Receiver [RX]",
      "fontSize": 12, "fontFamily": 1,
      "strokeColor": "#2f9e44", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 110, "height": 20
    },
    {
      "id": "pathmgr",
      "type": "rectangle",
      "x": 455, "y": 264, "width": 170, "height": 50,
      "strokeColor": "#2f9e44", "backgroundColor": "#d3f9d8",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0, "opacity": 100, "angle": 0
    },
    {
      "id": "pathmgr-label",
      "type": "text",
      "x": 465, "y": 279,
      "text": "Path Manager [probe]",
      "fontSize": 12, "fontFamily": 1,
      "strokeColor": "#2f9e44", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 150, "height": 20
    },
    {
      "id": "poolmgr",
      "type": "rectangle",
      "x": 455, "y": 327, "width": 170, "height": 50,
      "strokeColor": "#2f9e44", "backgroundColor": "#d3f9d8",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0, "opacity": 100, "angle": 0
    },
    {
      "id": "poolmgr-label",
      "type": "text",
      "x": 465, "y": 342,
      "text": "Pool Manager [lifecycle]",
      "fontSize": 12, "fontFamily": 1,
      "strokeColor": "#2f9e44", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 155, "height": 20
    },
    {
      "id": "quic-pool",
      "type": "rectangle",
      "x": 690, "y": 140, "width": 160, "height": 80,
      "strokeColor": "#f08c00", "backgroundColor": "#fff3bf",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0, "opacity": 100, "angle": 0
    },
    {
      "id": "quic-pool-label",
      "type": "text",
      "x": 705, "y": 158,
      "text": "Multi-QUIC Pool\n[N conns/peer]",
      "fontSize": 12, "fontFamily": 1,
      "strokeColor": "#f08c00", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 140, "height": 36
    },
    {
      "id": "arrow-client-to-lb",
      "type": "arrow",
      "x": 200, "y": 125, "width": 40, "height": 0,
      "strokeColor": "#495057", "backgroundColor": "transparent",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0, "opacity": 100, "angle": 0,
      "points": [[0, 0], [40, 0]],
      "startBinding": {"elementId": "client-lib", "gap": 5, "focus": 0},
      "endBinding": {"elementId": "logbuffer", "gap": 5, "focus": 0}
    },
    {
      "id": "arrow-lb-to-hsd",
      "type": "arrow",
      "x": 400, "y": 125, "width": 55, "height": 0,
      "strokeColor": "#495057", "backgroundColor": "transparent",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0, "opacity": 100, "angle": 0,
      "points": [[0, 0], [55, 0]],
      "startBinding": {"elementId": "logbuffer", "gap": 5, "focus": 0},
      "endBinding": {"elementId": "sender", "gap": 5, "focus": 0}
    },
    {
      "id": "arrow-sender-to-pool",
      "type": "arrow",
      "x": 625, "y": 163, "width": 65, "height": 0,
      "strokeColor": "#495057", "backgroundColor": "transparent",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0, "opacity": 100, "angle": 0,
      "points": [[0, 0], [65, 0]],
      "startBinding": {"elementId": "sender", "gap": 5, "focus": 0},
      "endBinding": {"elementId": "quic-pool", "gap": 5, "focus": 0}
    }
  ],
  "appState": {"viewBackgroundColor": "#ffffff"}
}
```

### 2.2 Component Descriptions

| Component | Type | Technology | Responsibility | Package |
|---|---|---|---|---|
| Client Library | In-process library | Go 1.26 | Public API: Client, Publication, Subscription, Image | `pkg/client` |
| Log Buffer | Shared memory | Go + mmap | Three-term ring buffer for pub/sub data exchange | `pkg/logbuffer` |
| Ring Buffers / IPC | Shared memory | Go + mmap | SPSC/MPSC command rings between client and driver | `pkg/ipc/ringbuffer` |
| Conductor | Driver agent | Go goroutine | Control plane: handles client commands, manages pub/sub state | `pkg/driver/conductor` |
| Sender | Driver agent | Go goroutine | Outbound data plane: reads log buffers, sends over QUIC | `pkg/driver/sender` |
| Receiver | Driver agent | Go goroutine | Inbound data plane: receives QUIC frames, writes image log buffers | `pkg/driver/receiver` |
| Path Manager | Driver agent | Go goroutine | Sends PING/PONG probes, maintains per-connection RTT/loss/throughput | `pkg/driver/pathmgr` |
| Pool Manager | Driver agent | Go goroutine | QUIC connection lifecycle, certificate rotation, pool sizing | `pkg/driver/poolmgr` |
| QUIC Adapter | Transport | quic-go | Wraps quic-go into Hyperspace connection interface | `pkg/transport/quic` |
| Arbitrator | Transport | Go | Selects best connection per send batch (LowestRTT/LeastOutstanding/Hybrid/Sticky/Random) | `pkg/transport/arbitrator` |
| Multi-QUIC Pool | Transport | Go | Maintains N concurrent QUIC connections per peer pair | `pkg/transport/pool` |
| Congestion Control | Algorithm | Go + ONNX | CUBIC, BBR, BBRv3; DRL controller via ONNX Runtime (CGO) | `pkg/cc` |
| Counters / CNC | Observability | Go + mmap | Named int64 counters in cnc.dat, readable by external tools | `pkg/counters` |
| AWS Discovery | Integration | Go + AWS SDK | Cloud Map service discovery for peer endpoints | `pkg/discovery` |
| Config | Integration | Go + AWS SDK | SSM Parameter Store runtime configuration | `pkg/config` |
| Identity | Security | Go + SPIFFE | SPIFFE/SPIRE workload identity, TLS cert loading | `pkg/identity` |
| Observability | Integration | Go + OTel | OpenTelemetry traces/metrics, CloudWatch metrics export | `pkg/obs` |

### 2.3 Component Interfaces

| From | To | Protocol | Auth | Notes |
|---|---|---|---|---|
| Client Library | Log Buffer | mmap read/write | None (same-process or same-host) | Zero-copy; atomic CAS for appender position |
| Client Library | Ring Buffer | mmap read/write | None | SPSC ring for client-to-conductor commands |
| Conductor | Log Buffer | mmap | None | Reads terms, manages flow control |
| Sender | Log Buffer | mmap read | None | Busy-poll; reads from active term |
| Sender | QUIC Adapter | Go interface | mTLS (SPIFFE SVIDs) | `Send(batch []Frame) error` |
| Receiver | QUIC Adapter | Go interface | mTLS (SPIFFE SVIDs) | `Recv() (Frame, error)` |
| Path Manager | QUIC Adapter | Go interface | mTLS | PING/PONG probe frames |
| Pool Manager | QUIC Adapter | Go interface | mTLS | Open/close connections, rotate certs |
| Arbitrator | Pool | Go interface | None | `Select(strategy) *Conn` |
| hsd daemon | AWS Cloud Map | HTTPS | IAM role | Peer endpoint discovery |
| hsd daemon | AWS Secrets Manager | HTTPS | IAM role | TLS certificate fetch |
| hsd daemon | AWS SSM | HTTPS | IAM role | Runtime configuration |
| hsd daemon | SPIRE Agent | Unix socket | SPIFFE workload API | SVID fetch and rotation |

---

## 3. Data Flow

### 3.1 Primary Publish Flow: Offer() → Network → Poll()

```
Publisher App
  │
  │  1. pub.Offer([]byte) — atomic CAS on appender position
  ▼
Log Buffer (mmap'd file, three terms)
  │
  │  2. Sender agent busy-polls active term
  ▼
Sender Agent (pkg/driver/sender)
  │
  │  3. Arbitrator.Select(LowestRTT) → conn
  ▼
Multi-QUIC Pool (N connections to peer)
  │
  │  4. conn.Send(batch) over QUIC stream — TLS 1.3 in-flight
  ▼
[network — QUIC/UDP, AZ or cross-AZ]
  │
  │  5. Receiver agent reads QUIC stream
  ▼
Receiver Agent (pkg/driver/receiver)
  │
  │  6. Write frame into image log buffer
  ▼
Image Log Buffer (mmap'd, subscriber side)
  │
  │  7. sub.Poll(handler) — zero-copy read
  ▼
Subscriber App
```

Numbered steps:

1. Publisher calls `pub.Offer(buf)`. The appender performs an atomic CAS on the log term's tail position. No syscall. No lock. The message is visible to the Sender agent immediately via mmap.
2. The Sender agent busy-polls the active log term in a tight loop (`runtime.LockOSThread` on a dedicated core). When new data is visible, it reads the frame header and payload without copying.
3. The Sender calls `Arbitrator.Select(strategy)` to pick the best QUIC connection from the pool. Default strategy is `LowestRTT` during S2; `Hybrid` (RTT + loss) is used once Path Manager is active (S4).
4. The frame batch is sent over the selected QUIC stream using quic-go's `SendMessage` or stream write. QUIC handles fragmentation, retransmit, and TLS 1.3 encryption.
5. On the remote host, the Receiver agent accepts the QUIC stream, reads frames, and validates frame headers.
6. Validated frames are written into the subscriber's image log buffer via mmap write.
7. The subscriber application calls `sub.Poll(handler, limit)`, which reads from the image buffer and invokes the handler for each message. Zero-copy — handler receives a direct slice into the mmap region.

### 3.2 Error Flows

| Scenario | Trigger | System Response | User-Visible Effect |
|---|---|---|---|
| QUIC connection failure | Network partition or peer crash | Pool Manager removes connection; Pool Learner may scale down; Arbitrator excludes failed conn | Temporary increase in latency while pool recovers; messages may retransmit |
| Log buffer term full | Publisher faster than Sender drains | Offer() returns `ErrBackPressure`; publisher must retry or drop | Back-pressure signal; publisher controls send rate |
| Certificate expiry | SVID TTL elapsed | Pool Manager fetches fresh SVID from SPIRE, rotates cert on connections via QUIC connection migration | Zero downtime cert rotation |
| Path probe timeout | PING unanswered > threshold | Path Manager marks connection RTT as MaxInt; Arbitrator avoids it | Traffic re-routed to other pool connections |
| SPIRE unavailable | SPIRE agent socket not responding | Identity pkg retries with exponential backoff; log error to slog | If cert expires before SPIRE recovers, new connections refused |
| hsd daemon crash | OS SIGKILL | Client lib detects driver heartbeat timeout; returns `ErrDriverUnavailable` on next Offer/Poll | Application must restart or reconnect; mmap regions persist for post-mortem |

### 3.3 Data Model

Hyperspace is stateless with respect to persistent storage — there is no database. The data model is the in-memory / mmap'd structures:

| Structure | Location | Description | Key Fields |
|---|---|---|---|
| LogBuffer | mmap file per publication | Three-term ring; term length configurable | `termLength`, `activeTermId`, `termTailCounters[3]` |
| LogFrame | Within LogBuffer | Fixed header + variable payload | `frameLength int32`, `flags uint8`, `type uint8`, `streamId int32`, `sessionId int32` |
| ImageLogBuffer | mmap file per subscription | Mirror of remote LogBuffer; written by Receiver | Same structure as LogBuffer |
| CncData (cnc.dat) | mmap file | Named counter array; driver heartbeat | `driverHeartbeatTime int64`, `clientLivenessTimeout int64`, counters[] |
| PoolEntry | In-memory (Pool) | One entry per QUIC connection in pool | `conn quic.Connection`, `rtt time.Duration`, `lossRate float64`, `outstandingFrames int64` |
| ProbeRecord | In-memory (PathMgr) | Per-connection probe state | `lastPingTime int64`, `lastPongTime int64`, `rttSamples []time.Duration` |

---

## 4. Infrastructure

### 4.1 Deployment Architecture

```excalidraw
{
  "type": "excalidraw",
  "version": 2,
  "source": "excalidraw",
  "elements": [
    {
      "id": "aws-boundary",
      "type": "rectangle",
      "x": 40, "y": 40, "width": 720, "height": 440,
      "strokeColor": "#1971c2", "backgroundColor": "#e7f5ff",
      "fillStyle": "solid", "strokeWidth": 2, "strokeStyle": "dashed",
      "roughness": 0, "opacity": 20, "angle": 0
    },
    {
      "id": "aws-label",
      "type": "text",
      "x": 60, "y": 50,
      "text": "AWS Region (us-east-1)",
      "fontSize": 14, "fontFamily": 1,
      "strokeColor": "#1971c2", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 200, "height": 20
    },
    {
      "id": "az-a",
      "type": "rectangle",
      "x": 60, "y": 80, "width": 280, "height": 380,
      "strokeColor": "#2f9e44", "backgroundColor": "#d3f9d8",
      "fillStyle": "solid", "strokeWidth": 2, "strokeStyle": "dashed",
      "roughness": 0, "opacity": 20, "angle": 0
    },
    {
      "id": "az-a-label",
      "type": "text",
      "x": 80, "y": 90,
      "text": "AZ-a",
      "fontSize": 13, "fontFamily": 1,
      "strokeColor": "#2f9e44", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 60, "height": 20
    },
    {
      "id": "ec2-a",
      "type": "rectangle",
      "x": 80, "y": 120, "width": 240, "height": 100,
      "strokeColor": "#2f9e44", "backgroundColor": "#d3f9d8",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0, "opacity": 100, "angle": 0
    },
    {
      "id": "ec2-a-label",
      "type": "text",
      "x": 95, "y": 130,
      "text": "c7gn.4xlarge (50Gbps)\nPublisher App + hsd\nSPIRE Agent",
      "fontSize": 11, "fontFamily": 1,
      "strokeColor": "#2f9e44", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 210, "height": 54
    },
    {
      "id": "az-b",
      "type": "rectangle",
      "x": 460, "y": 80, "width": 280, "height": 380,
      "strokeColor": "#2f9e44", "backgroundColor": "#d3f9d8",
      "fillStyle": "solid", "strokeWidth": 2, "strokeStyle": "dashed",
      "roughness": 0, "opacity": 20, "angle": 0
    },
    {
      "id": "az-b-label",
      "type": "text",
      "x": 480, "y": 90,
      "text": "AZ-b",
      "fontSize": 13, "fontFamily": 1,
      "strokeColor": "#2f9e44", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 60, "height": 20
    },
    {
      "id": "ec2-b",
      "type": "rectangle",
      "x": 480, "y": 120, "width": 240, "height": 100,
      "strokeColor": "#2f9e44", "backgroundColor": "#d3f9d8",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0, "opacity": 100, "angle": 0
    },
    {
      "id": "ec2-b-label",
      "type": "text",
      "x": 495, "y": 130,
      "text": "c7gn.4xlarge (50Gbps)\nSubscriber App + hsd\nSPIRE Agent",
      "fontSize": 11, "fontFamily": 1,
      "strokeColor": "#2f9e44", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 210, "height": 54
    },
    {
      "id": "quic-arrow",
      "type": "arrow",
      "x": 320, "y": 170, "width": 160, "height": 0,
      "strokeColor": "#f08c00", "backgroundColor": "transparent",
      "fillStyle": "solid", "strokeWidth": 3, "roughness": 0, "opacity": 100, "angle": 0,
      "points": [[0, 0], [160, 0]],
      "startBinding": {"elementId": "ec2-a", "gap": 5, "focus": 0},
      "endBinding": {"elementId": "ec2-b", "gap": 5, "focus": 0}
    },
    {
      "id": "quic-label",
      "type": "text",
      "x": 355, "y": 148,
      "text": "Multi-QUIC\n(N conns)",
      "fontSize": 11, "fontFamily": 1,
      "strokeColor": "#f08c00", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 90, "height": 36
    },
    {
      "id": "spire-server",
      "type": "rectangle",
      "x": 280, "y": 360, "width": 200, "height": 60,
      "strokeColor": "#e03131", "backgroundColor": "#ffe3e3",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0, "opacity": 100, "angle": 0
    },
    {
      "id": "spire-server-label",
      "type": "text",
      "x": 295, "y": 378,
      "text": "SPIRE Server\n[workload identity]",
      "fontSize": 11, "fontFamily": 1,
      "strokeColor": "#e03131", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 170, "height": 30
    }
  ],
  "appState": {"viewBackgroundColor": "#ffffff"}
}
```

### 4.2 Environment Matrix

| Component | Local Dev | Staging | Production |
|---|---|---|---|
| hsd daemon | Process (embedded mode) | EC2 t4g.medium | EC2 c7gn.4xlarge |
| Client apps | Same process (embedded driver) | EC2 t4g.medium | EC2 c7gn.4xlarge |
| QUIC transport | loopback UDP | VPC intra-AZ | VPC intra-AZ + cross-AZ |
| SPIRE | Stub / test identity | SPIRE Agent + Server | SPIRE Agent + Server (HA) |
| AWS Cloud Map | Mock (local config) | Real AWS Cloud Map | Real AWS Cloud Map |
| AWS SSM | env var override | Real SSM | Real SSM |
| AWS Secrets Manager | Local cert files | Real Secrets Manager | Real Secrets Manager |
| Observability | stdout JSON | CloudWatch + OTel | CloudWatch + OTel + Grafana |

### 4.3 Resource Requirements

| Component | CPU | Memory | Storage | Scaling |
|---|---|---|---|---|
| hsd daemon | 4–8 vCPU dedicated | 4–8 GB (log buffers) | Ephemeral mmap (tmpfs) | Vertical (one per host) |
| Publisher/Subscriber App | Application-defined | Shared mmap regions | None (mmap via driver) | Horizontal |
| SPIRE Server | 1 vCPU | 512 MB | 10 GB (cert store) | HA pair |
| EC2 Instance | 16 vCPU (c7gn.4xlarge) | 32 GB | 50 GB root | Auto Scaling Group |

### 4.4 Infrastructure as Code

- **IaC tool:** Terraform
- **Repository location:** `infra/` directory in project root
- **State backend:** S3 + DynamoDB lock table

---

## 5. Security

### 5.1 Threat Model Summary

| Threat | Vector | Mitigation | Residual Risk |
|---|---|---|---|
| Unauthorized message injection | Unauthenticated QUIC connection | mTLS with SPIFFE SVIDs required on every connection | Low — SVIDs are short-lived (1h TTL) |
| Man-in-the-middle on QUIC | Network intercept | TLS 1.3 mandatory; no downgrade path in quic-go config | Low |
| SVID theft / replay | Compromised host exfiltrates SVID | SVIDs are short-lived; SPIRE rotates automatically | Medium — host compromise is out-of-scope |
| Log buffer memory dump | Local process reads mmap regions | mmap files created with 0600 permissions; driver validates PID liveness | Low on hardened hosts |
| Oversized frame DoS | Malformed frame with large length field | Receiver validates `frameLength` against max configured MTU before read | Low |
| Secrets in environment | TLS key in env var | All secrets fetched from Secrets Manager; never env vars | Low |
| DRL model poisoning | Adversarial ONNX model input | Model loaded from Secrets Manager at startup; hash verified | Low |

### 5.2 Authentication and Authorisation

- **Authentication mechanism:** mTLS with SPIFFE SVIDs (X.509, 1h TTL, auto-rotated by SPIRE)
- **Authorisation model:** Trust domain membership — connections from outside the trust domain are rejected at TLS handshake
- **Certificate rotation:** SPIRE rotates SVIDs automatically; Pool Manager detects rotation via SPIRE workload API watch and re-handshakes QUIC connections
- **Session management:** QUIC connection lifetime managed by Pool Manager; no application-layer sessions

### 5.3 Data Classification

| Data Type | Classification | Encryption at Rest | Encryption in Transit | Retention |
|---|---|---|---|---|
| Message payloads | Internal | No (mmap on tmpfs) | TLS 1.3 (QUIC) | Log buffer TTL (configurable) |
| TLS private keys | Confidential | Yes (Secrets Manager) | TLS 1.3 | Until rotated |
| SPIFFE SVIDs | Confidential | No (in-memory only) | mTLS | 1h TTL |
| Latency metrics | Internal | No | TLS 1.3 (CloudWatch export) | 90 days (CloudWatch) |
| ONNX model weights | Internal | Yes (Secrets Manager) | TLS 1.3 | Until model version updated |

### 5.4 Security Controls Checklist

- [x] All QUIC connections require mTLS — no anonymous connections permitted
- [x] TLS version floor is 1.3 — configured in quic-go TLS config (`MinVersion: tls.VersionTLS13`)
- [x] SPIFFE SVIDs fetched via Unix socket workload API — never written to disk
- [x] mmap region files created with `os.OpenFile(..., 0600)` — not world-readable
- [x] Frame length validated before buffer read — prevents OOB read from malformed frames
- [x] `govulncheck ./...` run before every production deploy
- [x] Secrets fetched from AWS Secrets Manager at startup — no hardcoded credentials
- [ ] Rate limiting on QUIC connection accept — to be implemented in Pool Manager (S5)
- [ ] ONNX model hash verification — to be implemented in DRL loader (S7)

---

## 6. API Contracts

### 6.1 API Specification

Hyperspace does not expose an HTTP API. The public interface is the Go client library (`pkg/client`). The authoritative API contract is the Go package documentation.

- **Format:** Go package interface (godoc)
- **Location:** `pkg/client/client.go`, `pkg/client/publication.go`, `pkg/client/subscription.go`
- **Versioning strategy:** Go module semver — breaking changes require major version bump

### 6.2 Client Library Interface Summary

| Type | Method | Description |
|---|---|---|
| `Client` | `Connect(ctx, uri string) (*Client, error)` | Connect to hsd daemon; parse channel URI |
| `Client` | `AddPublication(ctx, channel string, streamId int32) (*Publication, error)` | Create outbound publication |
| `Client` | `AddSubscription(ctx, channel string, streamId int32) (*Subscription, error)` | Create inbound subscription |
| `Client` | `Close() error` | Close client and release resources |
| `Publication` | `Offer(buf []byte) (int64, error)` | Write message to log buffer; returns position or error |
| `Publication` | `Close() error` | Close publication |
| `Subscription` | `Poll(handler FragmentHandler, limit int) int` | Read up to limit messages; invokes handler per message |
| `Subscription` | `Close() error` | Close subscription |
| `Image` | `Position() int64` | Current consumer position |
| `Image` | `IsConnected() bool` | Whether the image has an active connection |

### 6.3 Channel URI Format

```
hs:quic?endpoint=<host>:<port>|pool=<N>|cc=<algo>|term-length=<bytes>
```

Examples:
- `hs:quic?endpoint=10.0.1.5:7777|pool=4|cc=bbrv3`
- `hs:quic?endpoint=peer-a.local:7777|pool=8|cc=hybrid|term-length=16777216`

| Parameter | Type | Default | Description |
|---|---|---|---|
| `endpoint` | `host:port` | Required | Remote hsd listener address |
| `pool` | int | 4 | Number of concurrent QUIC connections |
| `cc` | string | `bbrv3` | Congestion control algorithm (cubic/bbr/bbrv3/drl) |
| `term-length` | int | 16 MiB | Log buffer term length in bytes |

### 6.4 Breaking Change Policy

- Removing or renaming exported types/methods requires a major module version bump (`v2`)
- Adding new optional fields or methods is backward-compatible
- Channel URI parameter additions are backward-compatible; removals require a major version

---

## 7. Decision Log (ADRs)

| ADR | Title | Status | Date |
|---|---|---|---|
| ADR-001 | Multi-QUIC connection pool instead of single connection | Accepted | 2026-04-17 |
| ADR-002 | QUIC as transport layer instead of Aeron reliable UDP | Accepted | 2026-04-17 |
| ADR-003 | DRL congestion control via ONNX Runtime with CGO | Accepted | 2026-04-17 |
| ADR-004 | SPIFFE/SPIRE for workload identity (full integration) | Accepted | 2026-04-17 |
| ADR-005 | Embedded driver mode for testing | Accepted | 2026-04-17 |
| ADR-006 | Real AWS integrations from day one (no stub interfaces) | Accepted | 2026-04-17 |

---

### ADR-001: Multi-QUIC Connection Pool Instead of Single Connection

**Date:** 2026-04-17
**Status:** Accepted
**Decider:** Harness Architect

#### Context

A single QUIC connection between two peers offers one congestion control loop and one path through the network. At 50 Gbps line rate on EC2 c7gn.4xlarge, a single congestion window can become a bottleneck — particularly when BBR or CUBIC under-estimates available bandwidth. Additionally, EC2 instances in the same placement group often have multiple ECMP paths through the spine; a single connection cannot utilise them simultaneously.

#### Decision

Hyperspace maintains N concurrent QUIC connections per peer pair (default N=4, configurable via `pool=` URI parameter). Each connection is an independent congestion control loop. The Arbitrator selects which connection to use per send batch. The Adaptive Pool Learner adjusts N based on observed latency-loss correlation.

#### Alternatives Considered

| Option | Pros | Cons | Why Rejected |
|---|---|---|---|
| Single QUIC connection with many streams | Simple; one congestion loop | Cannot exploit ECMP; head-of-line blocking across streams | Rejected — ECMP utilisation is a hard requirement |
| TCP bonding via MPTCP | ECMP-aware; kernel handles path selection | TCP overhead; no QUIC features (0-RTT, connection migration) | Rejected — latency overhead unacceptable |
| UDP multipath (custom) | Maximum control | Full protocol implementation burden | Rejected — Aeron already tried this; complexity too high |

#### Consequences

- **Positive:** ECMP path diversity; independent CC loops; one slow connection doesn't block others
- **Negative:** N connections consume N times the handshake overhead on startup; Pool Manager complexity is significant
- **Risks:** Pool size misconfiguration can waste file descriptors and increase CPU overhead
- **Reversibility:** Easy — reduce N to 1 via URI parameter for single-connection mode

---

### ADR-002: QUIC as Transport Layer Instead of Aeron Reliable UDP

**Date:** 2026-04-17
**Status:** Accepted
**Decider:** Harness Architect

#### Context

Aeron's transport is a custom reliable UDP protocol (Aeron Protocol). It is highly optimised but requires implementing retransmission, flow control, and session management from scratch. Hyperspace needs all of these, plus TLS 1.3 (which Aeron's protocol does not include). Building a TLS layer on top of Aeron's transport would duplicate QUIC's core design. quic-go is a pure-Go QUIC implementation with active maintenance, IETF RFC 9000 compliance, and production use at scale (Cloudflare, etc.).

#### Decision

Use QUIC (via quic-go) as the sole transport. Accept that quic-go has higher per-packet overhead than a raw UDP implementation, in exchange for: built-in TLS 1.3, connection migration, 0-RTT handshake, and not maintaining a custom transport protocol.

#### Alternatives Considered

| Option | Pros | Cons | Why Rejected |
|---|---|---|---|
| Aeron Protocol (custom UDP) | Maximum performance | No TLS; full implementation burden | Rejected — TLS is a hard requirement; implementation cost too high |
| gRPC / HTTP/2 | Standard; widely supported | HTTP framing overhead; not designed for tight pub/sub loops | Rejected — latency overhead unacceptable |
| Raw UDP + custom framing | Maximum control | Full retransmit/CC implementation required; no TLS built-in | Rejected — equivalent to writing QUIC from scratch |

#### Consequences

- **Positive:** No TLS implementation required; connection migration for free; battle-tested retransmit
- **Negative:** quic-go overhead vs raw UDP (estimated 5–15 µs per RTT in benchmarks); CGO dependency if using quictls
- **Risks:** quic-go performance regressions in future versions; QUIC stack CPU overhead at high packet rates
- **Reversibility:** Hard — transport is a core architectural boundary; switching would require rewriting Sender/Receiver agents

---

### ADR-003: DRL Congestion Control via ONNX Runtime with CGO

**Date:** 2026-04-17
**Status:** Accepted
**Decider:** Harness Architect

#### Context

CUBIC and BBR are static algorithms. At very high packet rates and varying network conditions (cross-AZ, mixed workloads), a learned controller that observes RTT, loss, and throughput can find per-path optima that static algorithms cannot. ONNX Runtime is the industry standard for deploying trained neural network models in production — it supports Go via CGO bindings and has a C API that is stable across model formats (PyTorch, TensorFlow, custom). The DRL training pipeline runs offline; the inference loop runs in the hsd daemon at sub-millisecond latency.

#### Decision

Implement a `DRLController` in `pkg/cc/drl` that uses ONNX Runtime via CGO to run inference on a trained policy network. The controller observes `(rtt, lossRate, cwnd, outstandingBytes)` and outputs a `cwndDelta` action. The ONNX model is loaded from AWS Secrets Manager at daemon startup. A fallback to BBRv3 is automatic if the model fails to load or inference errors exceed a threshold.

#### Alternatives Considered

| Option | Pros | Cons | Why Rejected |
|---|---|---|---|
| Pure Go inference (custom NN) | No CGO dependency | Significant ML infrastructure to build | Rejected — ONNX Runtime is mature; not worth building again |
| TensorFlow Lite | Well-known | CGO binding complexity; larger binary | Rejected — ONNX has better Go binding ergonomics |
| Static BBRv3 only | No CGO; simpler | Cannot adapt to per-path conditions at scale | Rejected — DRL is a differentiating capability |

#### Consequences

- **Positive:** Per-path learned optima; adapts to network conditions without manual tuning
- **Negative:** CGO build dependency; ONNX Runtime shared library must be present on EC2; model training pipeline is a separate system
- **Risks:** Adversarial inference inputs could destabilise CC — mitigated by model hash verification and BBRv3 fallback
- **Reversibility:** Easy — DRL is a pluggable `CongestionController` interface; switch to BBRv3 via URI parameter `cc=bbrv3`

---

### ADR-004: SPIFFE/SPIRE for Workload Identity (Full Integration)

**Date:** 2026-04-17
**Status:** Accepted
**Decider:** Harness Architect

#### Context

Hyperspace peers must authenticate each other at the QUIC layer. The options are: static TLS certificates (operationally fragile), AWS ACM private CA (requires ACM API calls on rotation), or SPIFFE/SPIRE (workload identity with automatic short-lived SVIDs). SPIFFE is the CNCF standard for workload identity; SPIRE is its reference implementation. SVIDs have a 1-hour TTL and rotate automatically without operator intervention.

#### Decision

Integrate SPIFFE/SPIRE from day one. The `pkg/identity` package fetches X.509 SVIDs from the SPIRE Agent's Unix socket workload API. Pool Manager watches the SPIRE workload API for SVID rotation events and re-handshakes QUIC connections with new certificates. No self-signed certificates in production.

#### Alternatives Considered

| Option | Pros | Cons | Why Rejected |
|---|---|---|---|
| Static TLS certs | Simple | Manual rotation; cert management at scale is fragile | Rejected — operational burden unacceptable |
| AWS ACM private CA | AWS-native | ACM API latency on cert fetch; per-cert cost | Rejected — SPIRE is lower latency and more flexible |
| Stub identity (dev only) | Simplifies early sprints | Never becomes production-ready without significant refactor | Rejected — ADR-006 mandates real integrations from day one |

#### Consequences

- **Positive:** Zero-touch cert rotation; trust domain enforced at TLS layer; SPIFFE is portable across clouds
- **Negative:** SPIRE infrastructure required in every environment; local dev requires SPIRE or a conformant stub
- **Risks:** SPIRE server unavailability can prevent new connections (mitigated by SVID caching in SPIRE Agent)
- **Reversibility:** Hard — removing SPIFFE requires a new identity model; an ADR would be required

---

### ADR-005: Embedded Driver Mode for Testing

**Date:** 2026-04-17
**Status:** Accepted
**Decider:** Harness Architect

#### Context

The standard Hyperspace deployment runs `hsd` as a separate OS process. For integration testing, spinning up a separate process and waiting for IPC readiness adds significant test latency and complexity (race conditions in test setup, port conflicts, etc.). An embedded driver mode runs the five driver agents as goroutines within the test process, using in-process channels instead of mmap'd ring buffers for the client-driver IPC boundary.

#### Decision

Implement an `embedded.Driver` type in `pkg/driver` that satisfies the same `Driver` interface as the out-of-process `hsd` daemon. Tests import `embedded.Driver` and start it in-process. The client library detects the embedded driver via interface assertion and bypasses mmap ring buffers when possible.

#### Alternatives Considered

| Option | Pros | Cons | Why Rejected |
|---|---|---|---|
| Test against real hsd only | Tests are maximally realistic | Test setup complexity; slow; port conflicts in parallel tests | Rejected — slows CI unacceptably |
| Mock transport layer | Fast | Does not test the actual driver logic | Rejected — need to test real driver behaviour |
| Docker Compose hsd | Realistic | Even slower setup; Docker required in CI | Rejected — dependency on Docker in unit test layer is wrong |

#### Consequences

- **Positive:** Fast, hermetic integration tests; no port conflicts; parallelisable
- **Negative:** Embedded mode diverges from production if the in-process IPC path is not kept in sync with mmap path
- **Risks:** Test passing in embedded mode but failing against real hsd — mitigated by requiring scenario tests against real hsd in CI
- **Reversibility:** Easy — embedded mode is additive; removing it does not affect production

---

### ADR-006: Real AWS Integrations from Day One

**Date:** 2026-04-17
**Status:** Accepted
**Decider:** Harness Architect

#### Context

Many projects stub out AWS integrations behind interfaces during development and then "wire in the real thing" at production time. This approach consistently produces integration bugs discovered late (Secrets Manager IAM permissions wrong, Cloud Map namespace mismatch, SSM parameter name drift). The Engineering Department framework mandates the Build Mandate: produce working, tested code — not stubs.

#### Decision

All AWS integrations (Cloud Map, SSM, Secrets Manager) use real AWS SDK calls from Sprint S9. In earlier sprints, integrations are not yet implemented — but when they are implemented, they are implemented against real AWS services, not mock interfaces. Localstack is used in CI to provide AWS-compatible endpoints for testing.

#### Alternatives Considered

| Option | Pros | Cons | Why Rejected |
|---|---|---|---|
| Interface stubs in production | Simple during dev | Integration bugs found late; false confidence in tests | Rejected — violates Build Mandate |
| AWS SDK with mock client | Testable without AWS | Mocks drift from real API behaviour | Rejected — Localstack provides better fidelity |

#### Consequences

- **Positive:** Integration bugs found in CI, not production; real IAM policy testing via Localstack
- **Negative:** CI requires Localstack or real AWS account; additional CI setup complexity
- **Risks:** AWS API changes could break integration; mitigated by pinning AWS SDK version
- **Reversibility:** N/A — this is a policy, not a code decision

---

## Compliance Checklist

- [x] Section 1 Overview is present and complete
- [x] C4 Level 1 diagram is present with valid Excalidraw JSON
- [x] C4 Level 2 diagram is present with valid Excalidraw JSON
- [x] Section 3 Data Flow documents the primary flow with numbered steps
- [x] Error flows table is populated
- [x] Data model documented (mmap structures — no relational DB)
- [x] Section 4 Infrastructure documents all environments
- [x] Section 5 Security threat model is populated
- [x] Security controls checklist has been evaluated
- [x] Section 6 API Contracts lists all public client library methods and channel URI parameters
- [x] Section 7 has 6 ADRs covering all significant architectural decisions
