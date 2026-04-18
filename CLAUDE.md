# Hyperspace — Framework Rules

**Version:** 2.0
**Owner:** CTO / Engineering Orchestrator
**Status:** Mandatory — all agents operating in this repository must comply
**Effective:** 2026-04-17

---

## Critical Rules (Non-Negotiable)

1. **Model selection policy is mandatory.** Every agent uses the model specified in `framework/MODEL_SELECTION_POLICY.md`. Orchestrators and evaluators use `claude-opus-4-7`. Workers use `claude-sonnet-4-6`. Mechanical tasks use `claude-haiku-4-5-20251001`. Deviations must be logged to `harness_telemetry.jsonl` with justification.

2. **System Architecture is mandatory before implementation.** No implementation work begins on any project without an approved `SYSTEM_ARCHITECTURE.md` using the template at `framework/SYSTEM_ARCHITECTURE_TEMPLATE.md`. The Harness Evaluator must issue a PASS verdict before the Software Engineering Team begins. This rule has no exceptions.

3. **Progressive disclosure tiers must be respected.** Agents receive the context tier appropriate to their role as defined in `framework/PROGRESSIVE_DISCLOSURE_PROTOCOL.md`. Workers receive Tier 2 by default. Orchestrators and evaluators receive Tier 3. Agents must not hallucinate missing context — they must escalate via the escalation protocol.

4. **Knowledge base must be updated when new domain knowledge is encountered.** If an agent discovers a relevant URL, pattern, constraint, or tool behaviour not in the knowledge base, it proposes an addition in `session_handoff.md`. The Harness Architect reviews and adds it to `knowledge_base/`.

5. **Harness Engineering Team runs at project kickoff and sprint end.** No exceptions. The Harness Evaluator's `HARNESS_QUALITY_REPORT.md` must reach PASS before implementation begins and is produced at every sprint end.

6. **The Build Mandate applies to all code-producing agents.** Produce working code, not descriptions. Run it, test it, deliver it.

---

## Session Protocol

### Session Start (Mandatory — Before Any Work)

Every agent must execute this sequence at the start of every session:

```
1. Read session_state.json       — understand current project state
2. Read session_handoff.md       — understand what the previous session left
3. Read progress.json            — identify the feature to work on (status: not_started or failing)
4. Run the test suite            — confirm the baseline is green
   If baseline is NOT green: diagnose and fix before any new work
5. Read sprint_contracts/[S-ID].md for the current sprint
6. Begin implementation
```

If `session_state.json` does not exist, this is the first session. Create it before beginning any work.

### Session End (Mandatory — Before Declaring Done)

Every agent must execute this sequence at the end of every session:

```
1. Run full test suite           — all tests must pass
2. Commit all changes with a descriptive commit message
3. Update progress.json          — update status for features worked on this session
4. Write session_handoff.md      — document state, blockers, and next actions
5. Update session_state.json
6. Append to harness_telemetry.jsonl (at minimum a session_end event)
7. If new domain knowledge discovered: note in session_handoff.md for knowledge_base update
```

---

## Build Mandate

**This project PRODUCES WORKING CODE. Not descriptions. Not pseudocode. Working, tested code.**

Every agent that writes code must:
1. Create actual files — not describe what code could look like
2. Run the code — verify it compiles and executes without errors
3. Write tests and run them — code without tests is incomplete
4. Confirm tests pass — do not declare completion on a red test suite
5. Deliver working, tested artifacts

**Anti-patterns that are never acceptable:**
- "Here is what the code could look like..." — write the code
- "You could implement this by..." — implement it
- Removing or simplifying tests to make a test suite pass
- Declaring completion without running the test suite
- Marking a feature `passing` in `progress.json` without the Evaluator's PASS verdict

---

## Technology Constraints

| Attribute | Value |
|---|---|
| Language | Go 1.26 |
| Module | `github.com/cloud-jumpgate/hyperspace` |
| GitHub | `https://github.com/cloud-jumpgate/hyperspace` |
| QUIC Library | quic-go (latest stable) |
| Congestion Control | CUBIC / BBR / BBRv3 / DRL (ONNX Runtime via CGO) |
| Identity | SPIFFE/SPIRE (go-spiffe library) |
| AWS SDK | aws-sdk-go-v2 |
| OTel | go.opentelemetry.io/otel |
| Deployment Target | AWS EC2 c7gn.4xlarge (50 Gbps, arm64) |
| Minimum Test Coverage | 85% line coverage across all packages |
| Benchmark Target (aspirational) | P99 same-AZ RTT < 100 µs at 1 M msg/s |

### Go Standards

- **Go version:** 1.26+ (arm64 and amd64 targets)
- **Error handling:** always check and handle errors; never `_` an error from a production path
- **Atomic operations:** use `sync/atomic` for all shared counters — no mutex on hot paths
- **QUIC TLS config:** `MinVersion: tls.VersionTLS13` — never negotiate below TLS 1.3
- **mmap files:** always create with `os.O_RDWR|os.O_CREATE, 0600` — never world-readable
- **Frame length validation:** validate `frameLength` against max MTU before reading payload — prevents OOB reads
- **Logging:** `log/slog` structured logging with fields `session_id`, `stream_id`, `conn_id` — no `log.Printf` in production code paths
- **Linting:** `golangci-lint run` must exit 0 before any PR
- **Vulnerability scan:** `govulncheck ./...` before any production deploy
- **Security scan:** `gosec ./...` must pass with zero high-severity findings
- **Race detector:** `go test -race ./...` must pass — no data races
- **Goroutine leak:** production goroutines must honour `context.Context` cancellation; tests use `goleak` to verify
- **CGO:** permitted only in `pkg/cc/drl` (ONNX Runtime) and `pkg/ipc/memmap` (syscall wrappers) — nowhere else
- **Dependencies:** pinned to specific versions in `go.mod`; no `replace` directives in production

### Package Structure

```
cmd/hsd/                     — driver daemon entry point
cmd/hyperspace-stat/         — counters dumper CLI
cmd/hyperspace-probe/        — diagnostic / benchmark tool
pkg/client/                  — public client API (Client, Publication, Subscription, Image)
pkg/driver/conductor/        — control plane agent
pkg/driver/sender/           — outbound data plane agent
pkg/driver/receiver/         — inbound data plane agent
pkg/driver/pathmgr/          — path intelligence agent (PING/PONG probes)
pkg/driver/poolmgr/          — pool lifecycle agent (connections + cert rotation)
pkg/transport/quic/          — quic-go adapter (Connection interface)
pkg/transport/arbitrator/    — connection selection strategies
pkg/transport/probes/        — PING/PONG frame definitions
pkg/transport/pool/          — pool data structure
pkg/cc/                      — congestion control: cubic / bbr / bbrv3 / drl
pkg/logbuffer/               — log buffer, appender, reader, frame header
pkg/channel/                 — channel URI parser
pkg/counters/                — cnc.dat named counter array
pkg/ipc/ringbuffer/          — SPSC and MPSC ring buffers
pkg/ipc/broadcast/           — broadcast transmitter/receiver
pkg/ipc/memmap/              — mmap file utilities
pkg/discovery/               — AWS Cloud Map resolver
pkg/config/                  — AWS SSM Parameter Store config
pkg/identity/                — SPIFFE/SPIRE SVID fetch + AWS Secrets Manager cert load
pkg/obs/                     — OpenTelemetry + CloudWatch metrics
internal/atomic/             — AtomicBuffer (lock-free int64 array over mmap region)
examples/                    — basic_publisher, basic_subscriber, a2a_echo
```

### Sprint Map

| Sprint | Name | Features |
|---|---|---|
| S1 | Foundation | F-001 (Log Buffer), F-002 (Ring Buffers / IPC) |
| S2 | QUIC Transport | F-003 (QUIC Adapter), F-004 (Connection Pool), F-005 (Arbitrator) |
| S3 | Driver Core | F-008 (Conductor / Sender / Receiver) |
| S4 | Path Intelligence | F-006 (Path Manager + Probes) |
| S5 | Pool Intelligence | F-007 (Adaptive Pool Learner), F-009 (Pool Manager Agent) |
| S6 | Client Library | F-010 (Client, Publication, Subscription) |
| S7 | Congestion Control | F-011 (CUBIC / BBR / BBRv3 / DRL) |
| S8 | Observability | F-012 (Counters, Events, OTel, hyperspace-stat) |
| S9 | AWS + Identity | F-013 (Cloud Map / SSM / Secrets Manager), F-014 (SPIFFE/SPIRE) |
| S10 | Polish + CI/CD | README, examples, benchmark scaffolding, CI pipeline |

---

## Framework Document Index

| Document | Location | Purpose |
|---|---|---|
| Framework Rules (this file) | `CLAUDE.md` | Master rules; entry point for all agents |
| Model Selection Policy | `framework/MODEL_SELECTION_POLICY.md` | Which model each agent type uses |
| System Architecture Template | `framework/SYSTEM_ARCHITECTURE_TEMPLATE.md` | Template all SYSTEM_ARCHITECTURE.md files must follow |
| Progressive Disclosure Protocol | `framework/PROGRESSIVE_DISCLOSURE_PROTOCOL.md` | Context tiering rules for all agents |
| Agent Context Architecture | `framework/AGENT_CONTEXT_ARCHITECTURE.md` | Private vs shared context; all shared file formats |
| Harness Engineering Principles | `framework/HARNESS_ENGINEERING_PRINCIPLES.md` | SOTA principles all harness design must follow |
| Full Team Structure | `framework/teams/FULL_TEAM_STRUCTURE.md` | Complete org chart, handoff protocols, governance |
| Harness Engineering Team | `framework/teams/HARNESS_ENGINEERING_TEAM.md` | Full definitions for all 7 harness team members |

### Project Documents

| Document | Location | Created By |
|---|---|---|
| System Architecture | `SYSTEM_ARCHITECTURE.md` | Harness Architect |
| Specification | `SPEC.md` | Harness Architect |
| Agent Team | `AGENT_TEAM.md` | Harness Architect |
| Context Summary | `CONTEXT_SUMMARY.md` | Harness Architect |
| Progress Tracking | `progress.json` | Harness Architect (init); implementing agents (update) |
| Session State | `session_state.json` | Implementing agents |
| Session Handoff | `session_handoff.md` | Implementing agents |
| Decision Log | `decision_log.md` | Harness Architect + all agents |
| Shared Knowledge | `shared_knowledge.md` | All agents (append-only) |
| Harness Telemetry | `harness_telemetry.jsonl` | All agents (append-only) |
| Harness Quality Report | `HARNESS_QUALITY_REPORT.md` | Harness Evaluator |

### Knowledge Base

| Document | Location | Domain |
|---|---|---|
| Index | `knowledge_base/INDEX.md` | Master index of all resources |
| Domain Knowledge | `knowledge_base/DOMAIN_KNOWLEDGE.md` | Hyperspace-specific: QUIC, log buffers, DRL CC |
| Security | `knowledge_base/SECURITY.md` | SPIFFE/SPIRE, mTLS, Go security patterns |
| Architecture Patterns | `knowledge_base/ARCHITECTURE_PATTERNS.md` | Aeron DNA, pub/sub patterns, mmap IPC |
| External Resources | `knowledge_base/EXTERNAL_RESOURCES.md` | quic-go docs, SPIFFE spec, ONNX Runtime Go binding |

---

## Quality Gates

No deliverable passes without clearing all applicable gates:

| Gate | Verified By | Blocks |
|---|---|---|
| SYSTEM_ARCHITECTURE.md exists and is complete | Harness Evaluator | Implementation start |
| Harness quality PASS verdict | Harness Evaluator | Software Engineering Team start |
| Sprint contract written and signed | Harness Orchestrator | Feature implementation start |
| Baseline tests green (`go test -race ./...`) | Session init protocol | New feature work |
| `golangci-lint run` exits 0 | CI | PR merge |
| `govulncheck ./...` exits 0 | CI | Deploy |
| `gosec ./...` zero high-severity | CI | PR merge |
| Code Evaluator PASS | Code Evaluator | Feature marked passing |
| Security Evaluator PASS | Security Evaluator | S9, S14, and any auth/crypto feature |
| Architecture Evaluator PASS | Architecture Evaluator | Every 4 sprints; every production deploy |
| Delivery gate | Engineering Orchestrator | User receives deliverable |

---

## Common Commands

| Command | Purpose |
|---|---|
| `make test` | Run full test suite with race detector (`go test -race ./...`) |
| `make test-cover` | Run tests with coverage report (minimum 85%) |
| `make bench` | Run all benchmarks |
| `make lint` | Run `golangci-lint run` |
| `make vuln` | Run `govulncheck ./...` |
| `make sec` | Run `gosec ./...` |
| `make build` | Build all binaries: `hsd`, `hyperspace-stat`, `hyperspace-probe` |
| `make build-hsd` | Build hsd daemon binary only |
| `make run-hsd` | Run hsd daemon locally (requires local SPIRE stub or test certs) |
| `make docker-build` | Build Docker image for hsd |
| `make docker-run` | Run hsd in Docker |
| `make localstack-up` | Start Localstack for AWS integration tests |
| `make localstack-down` | Stop Localstack |
| `make help` | Print all targets with descriptions |

---

## Hyperspace-Specific Rules

1. **No mutex on hot paths.** Log buffer appender, ring buffer read/write, and counter updates must use `sync/atomic` — never `sync.Mutex` on the critical path.

2. **mmap files are 0600.** Any agent that creates a new mmap file must use permission `0600`. The Security Evaluator will FAIL any PR that creates world-readable mmap files.

3. **TLS 1.3 minimum is non-negotiable.** Any quic-go `tls.Config` without `MinVersion: tls.VersionTLS13` is a security defect. The Security Evaluator will FAIL without exception.

4. **Frame length validated before read.** Any code path that reads a payload from a QUIC frame or log buffer frame must validate `frameLength` against the maximum configured MTU before performing the read. OOB reads are treated as P0 bugs.

5. **CGO is restricted.** CGO is permitted only in `pkg/cc/drl` and `pkg/ipc/memmap`. Any agent introducing CGO in another package must file an ADR and obtain Architecture Evaluator PASS.

6. **Embedded driver mode for all integration tests.** Integration tests in `pkg/` must use `embedded.Driver`, not a real hsd process, unless the test is specifically a scenario test for the hsd binary itself. This rule keeps the CI fast.

7. **Goroutine lifecycle must be clean.** Every goroutine started by a driver agent must terminate when its `context.Context` is cancelled. Tests use `goleak` to verify. A goroutine leak is a P0 bug.

---

## Escalation Reference

| Situation | Escalate To | Protocol |
|---|---|---|
| Architecture ambiguity | Harness Architect → CTO | Context escalation request per PROGRESSIVE_DISCLOSURE_PROTOCOL.md |
| ADR needed | Harness Architect | Propose ADR in session_handoff.md |
| Security concern (any) | Security Evaluator | Immediate; do not proceed until resolved |
| CGO addition outside permitted packages | Architecture Evaluator | File ADR; halt implementation pending PASS |
| Evaluator returns FAIL | Engineering Orchestrator | Route specific defects back to implementing agent |
| Knowledge base gap | Harness Architect | Note in session_handoff.md |
| Human decision required | Engineering Orchestrator → User | Surface in session_handoff.md; halt dependent features |
| quic-go API change / breaking update | Backend Engineer → Harness Architect | Log in decision_log.md; assess impact on F-003, F-004, F-008 |
| HARD STOP triggered | Engineering Orchestrator | Agent halts immediately; logs event to harness_telemetry.jsonl |
| Sprint gate fails | Engineering Orchestrator → Harness Team | Fix gap before assigning implementation work |

---

## HARD STOPS (Non-Negotiable Agent Refusals)

**Every agent operating in this repository MUST refuse to proceed if any of the following conditions are true.**
These are not guidelines. These are blocking conditions. An agent that proceeds despite a HARD STOP is in violation of the framework.

### All Implementing Agents (Backend, DevOps, Security, QA)

1. **No sprint contract exists.** If `sprint_contracts/S[current].md` does not exist for the current sprint, STOP. Do not begin implementation. Escalate to the Engineering Orchestrator.

2. **Harness Evaluator has not issued PASS.** If `HARNESS_QUALITY_REPORT.md` does not exist or contains a FAIL verdict for the prior sprint, STOP. Do not begin implementation. Escalate to the Engineering Orchestrator.

3. **Baseline tests are not green.** If `go test -race ./...` fails, STOP. Diagnose and fix the failure before any new work. Do not implement new features on a broken baseline.

4. **Session state file is missing.** If `session_state.json` does not exist and this is not the first session, STOP. The project state is unknown. Escalate.

5. **Feature marked passing without Evaluator PASS.** Never mark any feature as `evaluator_pass` in `progress.json` without a Code Evaluator PASS verdict. If `progress.json` shows a feature as `evaluator_pass` but `code_evaluator_verdict` is not `PASS`, do not treat it as complete. Report the inconsistency.

6. **Writing sprint-level tracking to progress.json.** The `progress.json` file tracks at FEATURE level. Writing sprint-level-only status (e.g., `"S1": "complete"`) is a schema violation. Every feature must have its own entry.

7. **Session closed without updating artefacts.** Never close a session without updating `session_state.json`, appending to `harness_telemetry.jsonl`, and writing `session_handoff.md`. These three updates are mandatory at every session end.

8. **Calling external services in tests.** Tests must never call AWS services, SPIFFE/SPIRE endpoints, or any external service directly. Use injected interfaces and test doubles. If a test requires an external dependency, use Localstack or equivalent mocks.

9. **Adding CGO outside permitted packages.** CGO is only permitted in `pkg/cc/drl` and `pkg/ipc/memmap`. Adding CGO anywhere else requires an Architecture Evaluator ADR before proceeding.

10. **Sprint contract has no documentation deliverables.** If the sprint contract does not include a "Documentation Deliverables" section, STOP. The contract is incomplete. Escalate to the Harness Architect.

### Engineering Orchestrator (Additional)

11. **Do not assign implementation work without a signed sprint contract.** The sprint contract must exist in `sprint_contracts/` with a completed sign-off section before any implementing agent receives a task.

12. **Invoke the Harness Evaluator at every sprint boundary.** At the end of every sprint, before starting the next, the Harness Evaluator must produce or update `HARNESS_QUALITY_REPORT.md`. This is not optional. This is every sprint.

13. **Do not deliver to the user without Code Evaluator PASS.** The Code Evaluator must independently verify the implementation before the Engineering Orchestrator approves delivery.

14. **Do not skip the Business Requirements stage for new projects.** Every new project must have a validated `requirements.md` (or equivalent approved brief from CTO) before the Harness Architect begins.

---

## PRE-SPRINT GATE (Must Be Verified Before Any Sprint Begins)

Before the Engineering Orchestrator assigns any implementation work for sprint S[N], the following must all be true:

### Mandatory Pre-Sprint Checklist

- [ ] **Sprint contract exists:** `sprint_contracts/S[N].md` is present, non-empty, and follows the template
- [ ] **Documentation deliverables listed:** The sprint contract includes a "Documentation Deliverables" section with at least one deliverable
- [ ] **HARNESS_QUALITY_REPORT.md is current:** The report was updated within the last sprint cycle and contains PASS or CONDITIONAL PASS
- [ ] **progress.json is feature-level:** Every feature has an entry with `status`, `code_evaluator_verdict`, and `coverage_pct` fields
- [ ] **Previous sprint evaluated:** The Code Evaluator has issued a verdict for every feature in the previous sprint that was marked `in_progress` or `code_complete`
- [ ] **Baseline tests green:** `go test -race ./...` exits 0 on the current branch
- [ ] **Session artefacts current:** `session_state.json` and `session_handoff.md` were updated at the end of the previous sprint
- [ ] **Telemetry event logged:** `harness_telemetry.jsonl` has a `sprint_start` event for S[N]

### If Any Item Fails

The Engineering Orchestrator MUST NOT assign implementation work. Instead:
1. Invoke the Harness Evaluator to assess the gap
2. Invoke the relevant harness team member to fix the gap
3. Re-verify the checklist
4. Only proceed when all items are checked

---

## ENFORCEMENT CHECKLIST (Machine-Checkable Items)

These items can be verified by `init.sh` or the CI `harness-check` job. They are the automated enforcement layer.

### Per-Session (verified by init.sh)

| Check | Command / File | Pass Condition |
|---|---|---|
| Sprint contract exists | `test -s sprint_contracts/S${SPRINT}.md` | File exists and is non-empty |
| Harness verdict is PASS | `grep -q "PASS" HARNESS_QUALITY_REPORT.md` | File contains PASS or CONDITIONAL PASS |
| Baseline tests green | `go test -race ./...` | Exit code 0 |
| Session state exists | `test -f session_state.json` | File exists |
| progress.json has features | `jq '.features | length' progress.json` | Count > 0 (not sprint-level tracking) |
| Telemetry has sprint_start | `grep "sprint_start" harness_telemetry.jsonl` | Event present for current sprint |

### Per-PR (verified by CI harness-check job)

| Check | Verification | Pass Condition |
|---|---|---|
| HARNESS_QUALITY_REPORT.md exists | File existence check | File present in repo |
| HARNESS_QUALITY_REPORT.md recent | File modification date | Updated within last 14 days |
| progress.json feature-level | JSON schema check | Contains `features` array, not `sprints` array |
| session_state.json updated | File modification date | Updated within last 7 days |
| Sprint contracts non-empty | File size check | All S*.md files > 100 bytes |
| Knowledge base populated | File size check | All knowledge_base/*.md files > 1024 bytes |
| Evaluator verdict present | Content check | At least one feature has code_evaluator_verdict != null |

### Per-Sprint-Boundary (verified by Engineering Orchestrator)

| Check | Mechanism | Pass Condition |
|---|---|---|
| Harness Evaluator invoked | HARNESS_QUALITY_REPORT.md timestamp | Updated since last sprint ended |
| Sprint contract for next sprint | File existence | sprint_contracts/S[N+1].md exists |
| Documentation deliverables listed | Content check | Sprint contract has Documentation Deliverables section |
| Previous sprint features evaluated | progress.json | All code_complete features have code_evaluator_verdict |
| Telemetry event logged | harness_telemetry.jsonl | sprint_end event present for S[N] |
