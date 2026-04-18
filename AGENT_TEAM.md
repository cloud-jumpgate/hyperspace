# Hyperspace — Agent Team Composition

**Version:** 1.1
**Author:** Harness Architect
**Status:** Active
**Last Updated:** 2026-04-18
**Project:** github.com/cloud-jumpgate/hyperspace

> This document defines the specific agent team composition for the Hyperspace project. It supersedes the template placeholder version (v1.0). It is derived from `framework/teams/FULL_TEAM_STRUCTURE.md` and customised for Hyperspace's technology stack (Go 1.26, QUIC, SPIFFE/SPIRE, AWS EC2), risk profile (mTLS identity, AWS production integration), and complexity (10 sprints, 14 features, CGO dependency).

---

## Active Agents for This Project

### Harness Engineering Team (Always Active)

| Agent | Model | Active | Rationale |
|---|---|---|---|
| Harness Orchestrator | `claude-opus-4-7` | Yes | Required for all projects; conducted the 2026-04-18 compliance review |
| Harness Architect | `claude-opus-4-7` | Yes | Produced SYSTEM_ARCHITECTURE.md, SPEC.md, all ADRs, and sprint contracts S1–S2 at kickoff |
| Repository Engineer | `claude-sonnet-4-6` | Yes | Required for all projects; manages git hygiene and branch structure |
| Documentation Engineer | `claude-sonnet-4-6` | Yes | Produced governance remediation documents on 2026-04-18 |
| Process Engineer | `claude-sonnet-4-6` | Yes | Manages progress.json, session_handoff.md, and sprint protocol compliance |
| Artifact Engineer | `claude-sonnet-4-6` | Yes | Manages binary builds (hsd, hyperspace-stat, hyperspace-probe) and Docker image |
| Harness Evaluator | `claude-opus-4-7` | Yes | Required for all projects; HARNESS_QUALITY_REPORT.md pending (open action) |

### Software Engineering Team

| Agent | Model | Active | Rationale |
|---|---|---|---|
| Engineering Orchestrator | `claude-opus-4-7` | Yes | Top-level coordination; signs off sprint contracts |
| Software Architect | `claude-opus-4-7` | Yes | Reviews ADRs; Architecture Evaluator PASS required every 4 sprints |
| Backend Engineer | `claude-sonnet-4-6` | Yes | Primary implementer for S1–S9 (all 14 features); 38 packages delivered |
| Frontend Engineer | `claude-sonnet-4-6` | No | No web UI. Hyperspace is a Go library and daemon; all interfaces are Go API, CLI, and mmap IPC. Frontend Engineer not required for Hyperspace v1. |
| DevOps Engineer | `claude-sonnet-4-6` | Yes | Implemented S10: GitHub Actions pipeline, Makefile, Dockerfile, Localstack CI integration |
| Data Engineer | `claude-sonnet-4-6` | No | No data pipeline or ETL. Hyperspace is a transport layer; message content is opaque. Data Engineer not required. |
| Security Engineer | `claude-sonnet-4-6` | No | Security reviews performed by Security Evaluator (Opus). No separate Security Engineer role needed for this project size. |
| QA Engineer | `claude-sonnet-4-6` | No | Test coverage is the responsibility of Backend Engineer per the Build Mandate (≥ 85% gate). Dedicated QA Engineer not required for v1. |
| AI/ML Engineer | `claude-sonnet-4-6` | No | DRL training pipeline is out of scope for Hyperspace v1 (see SPEC §6). ONNX inference stub is implemented by Backend Engineer. AI/ML Engineer not required until training pipeline is in scope. |
| Tech Writer | `claude-sonnet-4-6` | Yes | Documentation Engineer (Harness team) handles docs. Tech Writer role is fulfilled by the Documentation Engineer for this project. |
| Engineering PM | `claude-sonnet-4-6` | No | Harness Orchestrator fulfils project management for this project. Dedicated Engineering PM not required. |

### Evaluator Team (Always Active)

| Agent | Model | Trigger | Status (2026-04-18) |
|---|---|---|---|
| Code Evaluator | `claude-opus-4-7` | After every sprint implementation | PASS issued for S1–S10 |
| Security Evaluator | `claude-opus-4-7` | After any auth/data/integration feature; required for S9 | PENDING — not yet invoked for S9. P1 open action. |
| Architecture Evaluator | `claude-opus-4-7` | Every 4 sprints; every production deploy | PENDING — not yet invoked. Required after S4, S8. P1 open action. |

---

## Agent Assignments by Sprint

| Sprint | Name | Implementing Agent | Supporting Agent | Evaluating Agent |
|---|---|---|---|---|
| S1 | Foundation | backend-engineer | — | Code Evaluator |
| S2 | QUIC Transport | backend-engineer | — | Code Evaluator |
| S3 | Driver Core | backend-engineer | — | Code Evaluator |
| S4 | Path Intelligence | backend-engineer | — | Code Evaluator + Architecture Evaluator |
| S5 | Pool Intelligence | backend-engineer | — | Code Evaluator |
| S6 | Client Library | backend-engineer | — | Code Evaluator |
| S7 | Congestion Control | backend-engineer | — | Code Evaluator |
| S8 | Observability | backend-engineer | — | Code Evaluator + Architecture Evaluator |
| S9 | AWS + Identity | backend-engineer | — | Code Evaluator + Security Evaluator (PENDING) |
| S10 | CI/CD + Polish | devops-engineer | backend-engineer | Code Evaluator |
| remediation | Governance Remediation | documentation-engineer | process-engineer | Harness Evaluator (PENDING) |

---

## Agent Assignments by Feature

| Feature | F-ID | Implementing Agent | Evaluating Agent | Notes |
|---|---|---|---|---|
| Log Buffer | F-001 | backend-engineer | Code Evaluator | ≥ 90% coverage required |
| Ring Buffers and IPC | F-002 | backend-engineer | Code Evaluator | ≥ 90% coverage required |
| QUIC Transport Adapter | F-003 | backend-engineer | Code Evaluator | TLS 1.3 enforcement tested |
| Multi-QUIC Connection Pool | F-004 | backend-engineer | Code Evaluator | ≥ 90% coverage required |
| Connection Arbitrator | F-005 | backend-engineer | Code Evaluator | ≥ 90% coverage required |
| Path Manager + Latency Probes | F-006 | backend-engineer | Code Evaluator | ≥ 90% coverage; EWMA RFC 6298 |
| Adaptive Pool Learner | F-007 | backend-engineer | Code Evaluator | ≥ 90% coverage; pure function |
| Driver Agents (Conductor/Sender/Receiver) | F-008 | backend-engineer | Code Evaluator | ≥ 85% coverage; goleak required |
| Pool Manager Agent | F-009 | backend-engineer | Code Evaluator | ≥ 85% coverage; Dialer injection |
| Client Library | F-010 | backend-engineer | Code Evaluator | ≥ 90% coverage; 1000-msg integration test |
| Congestion Control | F-011 | backend-engineer | Code Evaluator | ≥ 85% coverage; CGO gate (onnx build tag) |
| Observability | F-012 | backend-engineer | Code Evaluator | ≥ 85% coverage; hyperspace-stat binary |
| AWS Integration | F-013 | backend-engineer | Code Evaluator + Security Evaluator | ≥ 80% coverage; Localstack integration tests |
| SPIFFE/SPIRE Identity | F-014 | backend-engineer | Code Evaluator + Security Evaluator | ≥ 85% coverage; Security Evaluator PENDING |

---

## Knowledge Base Resources for This Team

| Agent | Primary Knowledge Resources |
|---|---|
| Harness Architect | `knowledge_base/DOMAIN_KNOWLEDGE.md`, `knowledge_base/EXTERNAL_RESOURCES.md`, `SYSTEM_ARCHITECTURE.md`, `decision_log.md` |
| Backend Engineer | `knowledge_base/DOMAIN_KNOWLEDGE.md`, `knowledge_base/SECURITY.md`, `knowledge_base/EXTERNAL_RESOURCES.md`, `shared_knowledge.md` |
| DevOps Engineer | `knowledge_base/EXTERNAL_RESOURCES.md`, `knowledge_base/SECURITY.md`, `SYSTEM_ARCHITECTURE.md` |
| Documentation Engineer | `knowledge_base/DOMAIN_KNOWLEDGE.md`, `SPEC.md`, `decision_log.md` |
| Security Evaluator | `knowledge_base/SECURITY.md`, `knowledge_base/DOMAIN_KNOWLEDGE.md`, `decision_log.md` (ADR-003, ADR-004) |
| Architecture Evaluator | `SYSTEM_ARCHITECTURE.md`, `decision_log.md`, `knowledge_base/ARCHITECTURE_PATTERNS.md`, `knowledge_base/EXTERNAL_RESOURCES.md` |
| Code Evaluator | Sprint contract for the sprint under review, `SPEC.md` feature detail, test output |

---

## Open Team Actions (2026-04-18)

| Action | Owner | Priority | Status |
|---|---|---|---|
| Invoke Security Evaluator for S9 (F-013, F-014) | Harness Orchestrator | P1 | Open |
| Invoke Architecture Evaluator for S4+S8 milestone | Harness Orchestrator | P1 | Open |
| Produce HARNESS_QUALITY_REPORT.md | Harness Evaluator | P1 | Open |
| Resolve slog migration P0 defect | backend-engineer | P0 | Open |
| Add gosec to CI pipeline (P0 defect) | devops-engineer | P0 | Open |
| Add goleak to remaining driver tests | backend-engineer | P1 | Open |
