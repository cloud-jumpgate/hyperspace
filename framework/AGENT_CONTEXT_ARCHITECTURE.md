# Agent Context Architecture

**Version:** 1.0  
**Owner:** CTO / Harness Architect  
**Status:** Mandatory  
**Effective:** 2026-04-13

---

## Purpose

This document defines how context is managed across all agents in the framework. It addresses the central harness engineering challenge identified in the assessment: each session currently starts cold, with no structured mechanism for agents to share state, pick up from previous sessions, or avoid conflicting on shared artifacts.

The architecture is built on two principles:
1. **Private context is ephemeral.** Each agent's reasoning, intermediate thoughts, and tool call history exist only within its session. They are not shared.
2. **Shared context is durable and structured.** All information that must persist across sessions or be visible to multiple agents lives in specific, named files in specific formats. No information that matters persists only in conversation history.

---

## Context Model Overview

```
┌────────────────────────────────────────────────────────────────────┐
│  PRIVATE CONTEXT (per agent, ephemeral)                            │
│  - Role definition (from agent definition file)                    │
│  - Current task brief (from orchestrator)                          │
│  - Tool call outputs (bash, read, write — this session only)       │
│  - Reasoning chain (this session only)                             │
│  - Context tier package (Tier 1/2/3 per PROGRESSIVE_DISCLOSURE)    │
└────────────────────────────┬───────────────────────────────────────┘
                             │ reads at start
                             │ writes at end
                             ▼
┌────────────────────────────────────────────────────────────────────┐
│  SHARED CONTEXT (structured files, durable, multi-agent)           │
│                                                                    │
│  session_state.json     — machine-readable project state           │
│  shared_knowledge.md    — accumulated domain knowledge             │
│  decision_log.md        — all ADRs, timestamped                    │
│  progress.json          — feature-level status tracking            │
│  session_handoff.md     — narrative handoff between sessions        │
│  harness_telemetry.jsonl — append-only event log                   │
│  sprint_contracts/      — pre-negotiated acceptance criteria       │
└────────────────────────────────────────────────────────────────────┘
```

---

## Private Context

### What Is Private

Each agent's private context includes:

- **Role definition:** The agent's system prompt / role file from `.claude/agents/[role].md`. This is static and version-controlled.
- **Task brief:** The specific task assigned by the orchestrator for this session. Includes the F-ID, sprint contract reference, and context tier package.
- **Tool outputs:** Results of bash commands, file reads, and other tool calls made during this session. These are ephemeral — they are not written to shared context automatically.
- **Reasoning chain:** The agent's chain-of-thought during the session. This is intentionally not shared — evaluator agents must evaluate outputs, not reasoning, to prevent the evaluator from being anchored on the producer's perspective.

### What Private Context Is Not

- Private context is not a shared state mechanism. Information in private context that needs to persist must be explicitly written to a shared context file.
- Private context does not accumulate across sessions. When a session ends, private context is gone. This is by design — the harness must not rely on conversation history for correctness.

### Context Isolation for Evaluators

Evaluator agents (Code Evaluator, Security Evaluator, Architecture Evaluator, Harness Evaluator) receive a deliberately restricted private context:

- They receive: the specification (SYSTEM_ARCHITECTURE.md, sprint contract), the code/artifact to evaluate, and test results.
- They do not receive: the producing agent's reasoning, tool call history, or intermediate outputs.
- They do not receive: the task brief given to the producing agent.

This isolation is essential. An evaluator that has seen the producer's reasoning is anchored on that reasoning and will evaluate the output through the producer's lens, systematically missing the errors that a fresh perspective would catch.

---

## Shared Context Files

### `session_state.json`

Machine-readable snapshot of current project state. Written at session end, read at session start.

**Location:** `[project_root]/session_state.json`

**Format:**

```json
{
  "project": "probe-manager",
  "version": "1.0.0",
  "last_session": {
    "timestamp": "2026-04-13T14:30:00Z",
    "agent": "backend",
    "session_id": "sess_20260413_001",
    "outcome": "COMPLETED",
    "features_completed": ["F003"],
    "features_started": ["F004"],
    "test_status": "all_passing",
    "commit": "abc1234"
  },
  "environment": {
    "go_version": "1.24.0",
    "postgres_version": "16.2",
    "test_command": "go test ./... -count=1",
    "run_command": "go run ./cmd/server",
    "docker_compose": "docker-compose.yml"
  },
  "warnings": [
    "F004 (Prometheus metrics) has unresolved dependency on F003 — do not start until F003 is PASSING"
  ],
  "blocked_features": [],
  "active_sprint": "sprint_2",
  "sprint_contract_ref": "sprint_contracts/sprint_2.md"
}
```

**Write rules:**
- Written by every agent that modifies project state at session end
- Never overwritten with an older timestamp — always check `last_session.timestamp` before writing
- If two agents write concurrently (parallel sessions), the later write takes precedence but must log a conflict event to `harness_telemetry.jsonl`

**Read rules:**
- Read by every agent at session start, before any work
- If `session_state.json` does not exist, this is the first session — the agent creates it after completing any initial setup
- If `last_session.outcome` is not `COMPLETED`, the agent must investigate the incomplete session before proceeding

---

### `shared_knowledge.md`

Accumulated project-specific knowledge: patterns discovered, bugs found and fixed, external service behaviors documented, gotchas and constraints. This is the project's living memory, separate from the ADR log.

**Location:** `[project_root]/shared_knowledge.md`

**Format:**

```markdown
# [Project Name] — Shared Knowledge

**Last updated:** [timestamp by last writer]  
**Maintained by:** All agents (append-only)

---

## Discovered Patterns

### [Pattern Title]
**Discovered:** [date] by [agent]  
**Context:** [when this pattern applies]  
**Pattern:** [what to do]  
**Rationale:** [why]  
**Example:** [code snippet or reference if applicable]

---

## External Service Behaviors

### [Service Name]
**Behavior:** [documented behavior]  
**Discovered:** [date] by [agent]  
**Evidence:** [test, observation, or documentation link]

---

## Known Constraints

- [Constraint]: [description] — discovered [date] by [agent]

---

## Bugs Found and Fixed

| Date | Agent | Bug | Fix Location | Notes |
|---|---|---|---|---|
| [date] | [agent] | [description] | [file:line] | [notes] |

---

## Do Not Touch

| File / Function | Reason | Added By | Date |
|---|---|---|---|
| [path] | [reason] | [agent] | [date] |
```

**Write rules:**
- Append-only. Agents add new entries; they do not modify or delete existing entries.
- Every entry must include: date, agent, and sufficient context for a new agent to understand it without background.
- If a "Do Not Touch" entry becomes outdated, add a new entry noting the resolution — do not delete the original.

**Read rules:**
- Agents read `shared_knowledge.md` as part of Tier 2 context when working on components covered by this knowledge.

---

### `decision_log.md`

All Architecture Decision Records (ADRs), timestamped, append-only. This is the authoritative log of decisions that are hard to reverse.

**Location:** `[project_root]/decision_log.md`

**Format:** Standard ADR format as defined in `SYSTEM_ARCHITECTURE_TEMPLATE.md`, Section 7.

**Write rules:**
- Only the Harness Architect, Software Architect, or CTO/Orchestrator may write new ADRs.
- Worker agents propose ADRs by outputting them in their session output with the label `[PROPOSED ADR]` — they do not write directly to `decision_log.md`.
- The Orchestrator reviews proposed ADRs and either accepts (writes to `decision_log.md`) or rejects (documents the rejection with reasoning).
- ADRs are never deleted. Superseded ADRs are marked `Status: Superseded by ADR-NNN` and the superseding ADR is written as a new entry.

**Read rules:**
- Included in Tier 3 context for all Opus-tier agents.
- Referenced by Tier 2 agents via the "Recent Decisions Affecting This Task" section of `CONTEXT_SUMMARY.md`.

---

### `progress.json`

Machine-readable feature-level status tracking. The canonical record of what is done, what is in progress, and what is not started.

**Location:** `[project_root]/progress.json`

**Format:** Defined in `HARNESS_IMPROVEMENT_REPORT.md` Recommendation 1. Key fields:

```json
{
  "project": "probe-manager",
  "last_updated": "2026-04-13T14:30:00Z",
  "last_session_summary": "one-sentence summary of last session",
  "features": [
    {
      "id": "F001",
      "name": "Feature name",
      "status": "passing | failing | in_progress | not_started | blocked",
      "sprint": "sprint_1",
      "owner": "backend",
      "tests": ["TestFunctionName"],
      "blocking": [],
      "blocked_by": [],
      "notes": "free text"
    }
  ]
}
```

**Write rules:**
- Written by the implementing agent at session end.
- Status transitions follow the state machine: `not_started` → `in_progress` → `failing` or `passing`. `blocked` can be set from any state.
- An agent must not mark a feature `passing` without running the tests and confirming they pass.
- An agent must not mark a feature `passing` if the sprint contract evaluator has not issued a PASS.

**Read rules:**
- Read at session start by every agent before any work begins.
- The Orchestrator selects the next feature to work on from `progress.json` — not from memory or conversation history.

---

### `session_handoff.md`

Narrative handoff between sessions. Human-readable complement to the machine-readable `session_state.json` and `progress.json`.

**Location:** `[project_root]/session_handoff.md`

**Format:** Defined in `HARNESS_IMPROVEMENT_REPORT.md` Recommendation 1.

**Write rules:**
- Written by the implementing agent at the end of every session.
- Overwrites the previous handoff (the handoff reflects the most recent session only — history is in git).
- Must include: what was done, what state the code is in, what the next session must do first, and any warnings.

**Read rules:**
- Read at session start by every agent.
- Included in Tier 2 context as "Last Session Summary."

---

### `harness_telemetry.jsonl`

Append-only structured log of all harness events. Used for harness self-improvement analysis.

**Location:** `[project_root]/harness_telemetry.jsonl` (or `[harness_root]/harness_telemetry.jsonl` for cross-project metrics)

**Format:** JSON Lines (one JSON object per line). See `HARNESS_IMPROVEMENT_REPORT.md` Recommendation 5 for event schema.

**Write rules:**
- Append-only. Never delete or modify existing lines.
- Written by every agent that produces a significant harness event: sprint start, implementation complete, evaluation result, delivery gate decision.
- Must include: `session`, `project`, `feature` (F-ID), `agent`, `event`, and `result` fields.

**Read rules:**
- Read by the Harness Evaluator when producing `HARNESS_QUALITY_REPORT.md`.
- Read by the Harness Orchestrator during sprint retrospectives.

---

### `sprint_contracts/`

Pre-negotiated acceptance criteria per feature sprint. Written before implementation begins; immutable once signed.

**Location:** `[project_root]/sprint_contracts/[F-ID].md`

**Format:** Defined in `HARNESS_IMPROVEMENT_REPORT.md` Recommendation 3.

**Write rules:**
- Written by the Orchestrator before any implementation begins.
- Reviewed and countersigned by the Evaluator agent.
- Immutable after both parties sign. Implementers may not modify sprint contracts.
- If a sprint contract must be changed (e.g., requirement change from a human), the original contract is archived with a `SUPERSEDED` flag and a new contract is written.

**Read rules:**
- Provided to the implementing agent as part of the task brief.
- Provided to the evaluating agent as the evaluation standard.
- Read by the Harness Evaluator when producing quality reports.

---

## Handoff Protocol

When an agent completes its work for a session, it must complete the following handoff sequence before declaring done:

### Step 1: Run Full Test Suite

```bash
make test
# or
go test ./... -count=1 -race
# or
pytest --tb=short
```

If tests fail, the agent does not proceed to handoff — it fixes the failures first.

### Step 2: Commit All Changes

```bash
git add [specific files — not git add -A]
git commit -m "feat(F-ID): description of what was done

- Specific change 1
- Specific change 2

Tests: all passing
Sprint contract: [PASS / IN PROGRESS / BLOCKED]"
```

### Step 3: Update `progress.json`

For each feature worked on this session:
- Change `status` to reflect current state: `passing` if tests pass and sprint contract evaluator approved, `failing` if tests exist but fail, `in_progress` if implementation is partial.
- Add any new tests to the `tests` array.
- Update `notes` with relevant context.
- Update `last_updated` timestamp.

### Step 4: Update `session_state.json`

Update `last_session` with:
- Current timestamp
- This session's outcome
- Features completed and started
- Test status
- Commit hash

### Step 5: Write `session_handoff.md`

Follow the format in `HARNESS_IMPROVEMENT_REPORT.md` Recommendation 1.

### Step 6: Append to `harness_telemetry.jsonl`

Write at minimum a session-end event:

```json
{"session": "ISO_TIMESTAMP", "project": "PROJECT", "feature": "F-ID", "agent": "ROLE", "event": "session_end", "result": "COMPLETED|INCOMPLETE", "test_pass_rate": 1.0, "features_completed": ["F-ID"]}
```

---

## Conflict Resolution

When two agents write conflicting information to shared context, the following rules apply:

### Conflict Type 1: Concurrent `session_state.json` writes

**Prevention:** Sequential agent execution within a project. Two agents should not run simultaneously on the same project. The Orchestrator enforces this.

**Resolution if it occurs:** The agent with the later timestamp wins. The agent whose write was overwritten must log a `CONFLICT` event to `harness_telemetry.jsonl` with both versions. The Orchestrator reviews at next session start.

### Conflict Type 2: `progress.json` status disagreement

**Example:** Agent A marks F003 as `passing`; Evaluator marks it as `failing`.

**Resolution:** The Evaluator's judgment always takes precedence. Feature status set by an Evaluator may not be overridden by the implementing agent. The implementing agent must resolve the specific defects identified by the Evaluator and resubmit.

### Conflict Type 3: `shared_knowledge.md` contradictory entries

**Example:** Agent A documents that "retries should use exponential backoff"; Agent B later documents "retries should be linear with jitter."

**Resolution:** Neither entry is deleted. Agent B's entry must explicitly reference and explain why it supersedes Agent A's entry. The Harness Architect reviews on the next architecture pass and writes a definitive entry that supersedes both. The contradictory entries are marked `[SUPERSEDED by entry dated DATE]`.

### Conflict Type 4: ADR contradiction

**Example:** ADR-005 says "use GORM"; a proposed ADR says "switch to raw SQL."

**Resolution:** Proposed ADRs that contradict existing accepted ADRs must explicitly acknowledge the contradiction and provide the context, decision, and consequences analysis for the change. The CTO/Orchestrator decides whether to accept the new ADR (which supersedes the old one) or reject it. The old ADR is never deleted.

---

## Session Start Protocol

Every agent must follow this sequence at the start of every session, before any implementation work:

```
1. Read session_state.json — understand current project state
2. Read session_handoff.md — understand what the previous session left
3. Read progress.json — identify the feature to work on
4. Run the test suite — confirm the baseline is green
5. If baseline is not green: stop, diagnose, fix before any new work
6. Read the sprint contract for the target feature (sprint_contracts/[F-ID].md)
7. Begin implementation
```

If `session_state.json` does not exist, this is the first session. The agent reads `SYSTEM_ARCHITECTURE.md` and creates `session_state.json`, `progress.json`, and `session_handoff.md` as part of project initialization.
