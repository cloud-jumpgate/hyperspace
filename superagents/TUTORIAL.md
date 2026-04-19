# Hyperspace — Agent Harness Tutorial

This document explains how to use the process automation scripts that enforce
the CLAUDE.md session protocol and sprint lifecycle gates. All agents operating
in this repository must run these scripts at the times indicated.

---

## Directory Layout

```
superagents/
  projectstructure/
    init.sh             — Session initialisation gate (run at every session start)
    sprint-boundary.sh  — Sprint end and sprint start gate (run at sprint boundaries)
  TUTORIAL.md           — This file
```

---

## Sprint Lifecycle Scripts

### `init.sh` — Session Start Gate

Run at the **beginning of every agent session**, before touching any code or
producing any output. It is a hard gate: if it exits non-zero, the session
must not begin.

```bash
# From the project root:
./superagents/projectstructure/init.sh S15
```

The script accepts one optional argument: the current sprint identifier
(e.g. `S15`). When provided, sprint-specific gates (1, 7, 8, 9, 10) are
active. When omitted, sprint-specific gates are skipped but the remaining
gates still run.

#### Gates

| Gate | Type | Description |
|------|------|-------------|
| Gate 1 | HARD | `sprint_contracts/S[N].md` exists and is non-empty |
| Gate 2 | HARD | `HARNESS_QUALITY_REPORT.md` exists with a PASS or CONDITIONAL PASS verdict |
| Gate 3 | HARD | `go test -race ./...` (or `make test`) exits 0 — baseline tests are green |
| Gate 4 | WARN | `session_state.json` exists; created with bootstrap content if missing |
| Gate 5 | HARD | `progress.json` exists and `.features` array has at least one entry |
| Gate 6 | WARN | `harness_telemetry.jsonl` exists; created as empty file if missing |
| Gate 7 | WARN | `harness_telemetry.jsonl` contains a `sprint_start` event for S[N]; auto-appended if missing |
| Gate 8 | HARD | At least one feature in `progress.json` has `code_evaluator_verdict == "PASS"` |
| Gate 9 | WARN | `harness_telemetry.jsonl` contains a Harness Evaluator event (warn only — evaluator may be pending) |
| Gate 10 | HARD | Features with `security_required == true` that are `code_complete` or `evaluator_pass` must have `security_evaluator_verdict` set |

HARD gates increment `FAILURES`; if `FAILURES > 0` the script exits 1 and
the session must not begin. WARN gates increment `WARNINGS` but do not block.

---

### `sprint-boundary.sh --end S[N]` — Sprint End Gate

Run when a sprint finishes, **before** any work on the next sprint begins.

```bash
# From the project root:
./superagents/projectstructure/sprint-boundary.sh --end S14
```

#### Sprint End Checks (6 checks)

| Check | Type | Description |
|-------|------|-------------|
| 1 | HARD | All features in `progress.json` with `sprint == "S[N]"` have `code_evaluator_verdict` set (not null). If any are null, the Code Evaluator agent must be invoked. |
| 2 | HARD | `HARNESS_QUALITY_REPORT.md` was modified within the last 14 days. If stale, invoke the Harness Evaluator agent. |
| 3 | HARD | `harness_telemetry.jsonl` contains a `sprint_end` event for S[N]. If missing, append one before closing the sprint. |
| 4 | HARD | `sprint_contracts/S[N+1].md` exists and contains the string "Documentation Deliverables". If missing or incomplete, invoke the Harness Architect agent. |
| 5 | HARD | `session_state.json` exists and was modified within the last 24 hours. If stale, the implementing agent must run session end protocol. |
| 6 | HARD | `session_handoff.md` exists and was modified within the last 24 hours. If stale, the implementing agent must run session end protocol. |

---

### `sprint-boundary.sh --start S[N]` — Sprint Start Gate

Run at the **start of a new sprint**, before the Engineering Orchestrator
assigns any work to implementing agents.

```bash
# From the project root:
./superagents/projectstructure/sprint-boundary.sh --start S15
```

#### Sprint Start Checks (8 checks)

| Check | Type | Description |
|-------|------|-------------|
| 1 | HARD | `sprint_contracts/S[N].md` exists and is > 100 bytes. If missing or too small, invoke the Harness Architect agent. |
| 2 | HARD | `sprint_contracts/S[N].md` contains a "Documentation Deliverables" section. If missing, invoke the Harness Architect agent. |
| 3 | HARD | `HARNESS_QUALITY_REPORT.md` contains "PASS" or "CONDITIONAL PASS". If not, invoke the Harness Evaluator agent. |
| 4 | HARD | `progress.json` contains a `.features` array with at least one entry. If empty, the Harness Architect must populate it. |
| 5 | HARD | All features from `S[N-1]` in `progress.json` have `code_evaluator_verdict` set. If any are null, invoke the Code Evaluator agent to close out the prior sprint. |
| 6 | HARD | `go test -race ./...` exits 0. If tests are red, fix them before any implementation begins. |
| 7 | HARD | `session_state.json` and `session_handoff.md` both exist. If either is missing, the implementing agent must write them. |
| 8 | HARD | `harness_telemetry.jsonl` contains a `sprint_start` event for S[N]. If missing, append one before work begins. |

---

## What To Do When a Gate Fails

Every gate failure prints an **ACTION** line naming the specific agent to
invoke and the exact step required. The general escalation ladder is:

| Failing Check | Agent to Invoke | Escalation Path |
|---------------|-----------------|-----------------|
| No sprint contract | Harness Architect | Engineering Orchestrator → Harness Architect |
| No HARNESS_QUALITY_REPORT.md or stale | Harness Evaluator | Engineering Orchestrator → Harness Evaluator |
| Red tests | Implementing Agent | Fix tests; re-run gate before any new work |
| Missing code_evaluator_verdict | Code Evaluator | Engineering Orchestrator → Code Evaluator |
| Missing security_evaluator_verdict | Security Evaluator | Engineering Orchestrator → Security Evaluator |
| Missing session_state.json / session_handoff.md | Implementing Agent | Run session end protocol from CLAUDE.md |
| Missing harness_telemetry.jsonl events | Implementing Agent | Append event manually or via init.sh |

If you cannot resolve a gate failure yourself, escalate to the Engineering
Orchestrator. Include the full script output in your escalation message.

---

## Recommended Sprint Workflow

```
Sprint N-1 ends:
  ./superagents/projectstructure/sprint-boundary.sh --end S[N-1]

Sprint N begins — before assigning work:
  ./superagents/projectstructure/sprint-boundary.sh --start S[N]

Each agent session within Sprint N:
  ./superagents/projectstructure/init.sh S[N]
  # ... do work ...
  # run session end protocol from CLAUDE.md
```

All three scripts must exit 0 before implementation work proceeds. A
non-zero exit is a hard stop.

---

## Adding a New Gate

1. Add the gate logic to `init.sh` or `sprint-boundary.sh` following the
   existing numbered-gate pattern.
2. Increment `GATE_FAILURES` for HARD gates; emit a WARN message only for
   soft gates.
3. Update the gate table in this file.
4. Log an ADR in `decision_log.md` if the gate enforces a new policy.
