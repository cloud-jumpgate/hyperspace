# Knowledge Base Index — Hyperspace

**Status:** Active
**Owner:** Harness Architect
**Last Updated:** 2026-04-17

> This index is the master reference for all domain knowledge relevant to the Hyperspace project. It is updated by the Harness Architect whenever an agent discovers new knowledge worth preserving. Agents reference this index to find relevant resources before beginning implementation tasks.

---

## How to Use This Index

1. Before starting any implementation task, check this index for relevant resources
2. If you discover knowledge not captured here, note it in `session_handoff.md`
3. The Harness Architect will add it to the appropriate knowledge_base/ file and update this index
4. Never add knowledge directly to this index without also creating or updating the source file

---

## Resource Categories

| Category | File | Description | Status |
|---|---|---|---|
| Domain Knowledge | `DOMAIN_KNOWLEDGE.md` | Hyperspace-specific: QUIC multi-connection transport, log buffer architecture, Aeron-inspired IPC, DRL congestion control, channel URI format | Active |
| Security | `SECURITY.md` | SPIFFE/SPIRE workload identity, mTLS enforcement, TLS 1.3 minimum, Go security patterns (gosec, govulncheck), mmap file permissions, frame length validation | Active |
| Architecture Patterns | `ARCHITECTURE_PATTERNS.md` | Aeron DNA (media driver pattern), pub/sub with shared-memory log buffers, zero-copy IPC via mmap, Multi-QUIC connection pooling, embedded driver mode for testing | Active |
| External Resources | `EXTERNAL_RESOURCES.md` | quic-go docs (github.com/quic-go/quic-go), go-spiffe library (github.com/spiffe/go-spiffe/v2), ONNX Runtime Go binding (github.com/yalue/onnxruntime_go), SPIFFE specification, RFC 8312 (CUBIC), BBRv3 Google proposal | Active |

---

## Key Domain Concepts (Quick Reference)

- **Log Buffer**: Three-term mmap'd ring buffer for zero-syscall IPC between client and driver
- **Driver Daemon (hsd)**: Out-of-process agent running Conductor, Sender, Receiver, PathManager, PoolManager
- **Multi-QUIC Pool**: N concurrent QUIC connections per peer for ECMP path diversity
- **Arbitrator**: Strategy-based connection selection (LowestRTT, LeastOutstanding, Hybrid, Sticky, Random)
- **Path Manager**: PING/PONG probe loop measuring per-connection RTT, loss, throughput
- **Adaptive Pool Learner**: Latency-loss correlation model for automatic pool sizing
- **Channel URI**: `hs:quic?endpoint=host:port|pool=N|cc=algo`
- **CNC file (cnc.dat)**: Shared counter array for observability; read by hyperspace-stat CLI
- **SVID**: SPIFFE Verifiable Identity Document -- short-lived X.509 cert from SPIRE Agent

---

## Resource Addition Protocol

Any agent that discovers a resource worth adding:

1. Notes it in `session_handoff.md` under "New Domain Knowledge Discovered"
2. Does NOT add it directly to knowledge_base/ files
3. The Harness Architect reviews the note and adds the resource at the next harness maintenance cycle
4. The Harness Architect updates this index
