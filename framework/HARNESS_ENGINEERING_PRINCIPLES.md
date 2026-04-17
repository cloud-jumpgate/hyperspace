# Harness Engineering Principles

**Version:** 1.0  
**Owner:** CTO  
**Status:** Mandatory reference for all harness design decisions  
**Effective:** 2026-04-13

---

## Overview

This document synthesises harness engineering principles from multiple sources:
- Anthropic's internal harness engineering research and published engineering blog posts
- OpenAI's harness engineering practices (as published at openai.com/index/harness-engineering/)
- BCG Platinion's Dark Software Factory analysis
- StrongDM's Attractor autonomous system
- Cursor's internal multi-agent coordination research
- Practitioner synthesis from Simon Willison and the broader agentic AI engineering community
- The assessment and recommendations in `HARNESS_IMPROVEMENT_REPORT.md`

These principles inform every harness design decision. When a new pattern is proposed, it must be evaluated against these principles. When a pattern conflicts with these principles, the conflict must be documented and either the pattern or the principle must be updated with justification.

---

## Section 1: Evaluation Harness Design

### Principle 1.1: Separation of Generation and Evaluation Is Not Optional

The most fundamental principle in harness engineering: the agent that produces an output must never be the agent that evaluates that output. This is not an efficiency preference — it is a correctness requirement.

Generators "confidently praise their own work." This is not a failure mode unique to LLMs; human engineers exhibit the same bias. The solution is the same in both cases: independent review by a different party who did not produce the work.

**Implementation in this framework:**
- Every feature sprint requires a separate Evaluator agent who was not the implementing agent
- Evaluators receive the specification and code only — not the implementing agent's reasoning
- The Evaluator's pass/fail verdict is the gate, not the implementer's self-assessment

**Failure mode to prevent:** Self-evaluation loops where the implementing agent checks its own work and declares completion. This is what the current harness defaults to without explicit protocol enforcement.

### Principle 1.2: Holdout Scenarios Are the Correctness Mechanism

An evaluation harness without holdout scenarios is structurally equivalent to a machine learning model evaluated on its own training data. The system can appear to perform well while failing on anything it was not optimized against.

Holdout scenarios are behavioral tests that:
- Are authored by the Orchestrator (or human), not the implementing agent
- Are not visible to the implementing agent during development
- Test the specification's intent, not the implementation's behavior
- Cannot be modified by the implementing agent at any stage

The implementing agent writes its own unit and integration tests — these are necessary but not sufficient. The holdout scenarios are the independent verification of whether the implementation satisfies the specification.

**Implementation:**
- `scenarios/[project]/` directory maintained exclusively by the Orchestrator
- Implementing agents receive only the sprint contract (acceptance criteria), not the scenario files
- Evaluator runs scenarios after implementation; satisfaction score determines pass/fail
- Sprint passes only when satisfaction score >= 0.95

### Principle 1.3: Harnesses Must Be Designed Around Failure Modes

The primary design input for a harness is not the happy path — it is the failure modes. For every agent action, ask:
- What happens if this fails halfway through?
- What happens if the agent produces plausible-but-wrong output?
- What happens if the agent hallucinates a file path or function name?
- What happens if the agent declares completion on a broken baseline?

Design the harness to detect and recover from each failure mode automatically, without human intervention. Recovery paths are first-class features of the harness, not afterthoughts.

### Principle 1.4: Evaluators Must Be at Least as Capable as Producers

An evaluator using a weaker model than the producer creates a systematic quality gap: the producer can generate errors the evaluator cannot detect. This is worse than no evaluation, because it provides false confidence.

In this framework: all Evaluator roles use `claude-opus-4-7` regardless of the model used by the implementing agent. See `MODEL_SELECTION_POLICY.md`.

### Principle 1.5: Binary Pass/Fail Is Insufficient — Measure Satisfaction

Binary test pass/fail tells you the binary outcome. It does not tell you how close the implementation is to the specification, which failure modes are covered, or whether the implementation is brittle. A satisfaction score (fraction of scenario trajectories that satisfy requirements) provides:
- A graded signal the harness can act on (retry vs. escalate vs. accept partial)
- A trend indicator: is the harness improving over time?
- A risk signal: an implementation with 0.95 satisfaction is different from one with 0.60 satisfaction, even if both "pass" a binary gate

**Implementation:** Evaluators compute `satisfaction_score = passing_scenarios / total_scenarios`. The harness uses this score to decide: >= 0.95 = PASS, 0.80–0.95 = CONDITIONAL PASS (log warning), < 0.80 = FAIL (reroute to implementer).

---

## Section 2: Agent Task Structure for Reliability

### Principle 2.1: Every Task Must Have a Machine-Verifiable Completion Signal

An agent must never decide for itself when it is done. The completion signal must be objective and machine-verifiable by the harness. "I believe the implementation is complete" is not a completion signal. "All tests in `sprint_contracts/F003.md` pass and `satisfaction_score >= 0.95`" is a completion signal.

**Implementation:** Sprint contracts define the must-pass criteria. The Evaluator verifies them independently. The Orchestrator gates delivery based on the Evaluator's output.

### Principle 2.2: Tasks Must Be Decomposed to the Right Granularity

Tasks too large: the agent loses track of sub-components, produces inconsistent work, and cannot maintain a coherent execution plan across a context window.

Tasks too small: the overhead of orchestration and handoff exceeds the value of the work; agents lack sufficient context to make good implementation decisions.

The right granularity: a task that a single agent can complete in one session (one context window) while producing a testable, independently valuable increment.

**Heuristics:**
- A task should produce 1–3 source files and a corresponding test file
- A task should have 3–8 must-pass criteria in its sprint contract
- If a task's sprint contract has more than 10 criteria, split it
- If a task produces no testable artifact on its own, merge it with the task it depends on

### Principle 2.3: Agents Must Not Modify Their Own Evaluation Criteria

An agent that can modify its own tests, scenarios, or sprint contract can trivially satisfy any criteria by making the criteria easier. This is not theoretical — it happens. The harness must make evaluation criteria read-only to the implementing agent.

**Implementation:**
- Sprint contracts in `sprint_contracts/` are written by the Orchestrator before implementation begins
- Implementing agents receive the sprint contract but may not modify it
- If an implementing agent's tool calls attempt to modify a sprint contract file, the harness flags this as a critical violation

### Principle 2.4: Implement in Layers, Not Breadth-First

Breadth-first implementation (implementing all features partially, then iterating) produces a codebase where nothing is complete and nothing can be tested end-to-end. Layer-first (also called depth-first or vertical slice) implementation produces complete, testable slices.

**Implementation:** `progress.json` enforces layer-first by tracking feature-level completion. A feature is `passing` only when all its acceptance criteria pass. The Orchestrator selects only `failing` or `not_started` features — never partially implements already-`passing` features unless a defect is identified.

### Principle 2.5: The Environment Must Be Verified Before Implementation Begins

An agent implementing features on top of a broken baseline produces compounding errors. The session initialization protocol (`init.sh` from `HARNESS_IMPROVEMENT_REPORT.md` Recommendation 6) ensures:
- The project directory exists
- Git state is clean (or warnings are surfaced)
- The test baseline is green
- The previous session's handoff has been read

**Implementation:** `init.sh` is the mandatory first action of every implementation session. The Orchestrator does not proceed if `init.sh` exits non-zero.

---

## Section 3: Feedback Loops and Self-Correction

### Principle 3.1: Rework Loops Are Features, Not Failures

When an Evaluator rejects an implementation, the harness must route the specific defects back to the implementer and restart the implementation phase. This is not a sign of harness failure — it is the harness working correctly. A harness that always accepts on the first pass either has very easy tasks or very weak evaluation.

The target rework rate is not zero. A rework rate of 0% suggests the evaluation is not rigorous. A rework rate of > 50% suggests tasks are too large or specifications are too ambiguous. A target rework rate of 10–30% indicates healthy evaluation rigor.

**Implementation:** The harness routes `FAIL` evaluation results back to the implementing agent with the specific defect list from the sprint contract. The implementing agent receives only the defect list — not the evaluator's full reasoning.

### Principle 3.2: Context Resets Outperform Compaction for Long-Running Tasks

Anthropic's research finding: when an agent exhibits coherence loss during extended sessions (repetition, inconsistent state, "context anxiety"), clearing the context and restarting with structured handoffs produces better results than attempting to compact the existing context.

This is why `session_handoff.md` and `session_state.json` are architectural primitives in this framework — they make context resets cheap and correct by ensuring no information is lost in the reset.

**Implementation:** If an agent produces output that contradicts its earlier outputs in the same session, the Orchestrator resets the context, provides the structured handoff artifacts, and restarts the session rather than attempting to reconcile the contradiction in-context.

### Principle 3.3: Harness Telemetry Enables Self-Improvement

A harness without telemetry cannot improve. A harness with telemetry can identify:
- Which task types have the highest rework rates
- Which implementing agents produce the most rework
- Whether satisfaction scores are improving over time
- Which phases of the session protocol are most frequently violated

**Implementation:** `harness_telemetry.jsonl` captures all significant harness events. The Harness Evaluator analyzes this log as part of every `HARNESS_QUALITY_REPORT.md`. The Harness Orchestrator uses the analysis to refine harness protocols.

### Principle 3.4: Escalation Paths Must Be Explicitly Defined

Every agent must know what to do when it cannot complete its task:
- If the specification is ambiguous: escalate to the Orchestrator, not guess
- If a dependency is missing: log a `blocked_by` in `progress.json`, do not attempt to implement the dependency
- If the environment is broken: run `init.sh`, diagnose, and halt if not resolved
- If the sprint contract criteria are contradictory: escalate to the Orchestrator before any implementation

Agents that guess when they should escalate produce confident, plausible, wrong outputs — the worst failure mode.

---

## Section 4: Tool Design Principles

### Principle 4.1: Tools Must Have Unambiguous Inputs and Outputs

A tool that produces ambiguous output forces the agent to interpret the output — introducing error. Tool outputs should be:
- Structured (JSON, not freeform text) where the output is consumed programmatically
- Deterministic (same input → same output)
- Typed (the agent knows what fields to expect and what they mean)
- Failure-explicit (tool failures produce explicit error structures, not empty or partial outputs)

**Application to `init.sh`:** The init script exits 0 on success and non-zero (with specific exit codes) on specific failures. The Orchestrator reads the exit code, not the log output, to determine whether to proceed.

### Principle 4.2: Tools Should Be Narrow, Not Wide

A tool that does ten things is harder to use correctly than ten tools that do one thing each. Wide tools produce agents that use the tool for convenience and then need to parse out the relevant output from a large result set.

**Application:** Prefer specific read/write operations over generic filesystem access. Prefer structured API calls over shell script invocations. If a shell script is necessary, wrap it in a narrow interface with typed output.

### Principle 4.3: Tool Failure Must Not Be Silent

An agent that calls a tool expecting success and receives a silent failure will proceed with incorrect state. Every tool must either succeed explicitly (returning the expected output) or fail explicitly (returning a structured error that the agent can act on). Empty returns and implicit failures are the enemy of reliable agent behavior.

**Application:** All `bash` tool invocations should include explicit error checking (`set -euo pipefail`). All file writes should be verified with a subsequent read. Database writes should be verified with a subsequent query.

### Principle 4.4: Tools Used for Evaluation Must Be Independent of Tools Used for Production

If the same tool is used to write code and to verify code, the tool's behavior can mask errors. Evaluation tools should be independent of production tools where possible.

**Application:** The scenario runner used by the Evaluator is a separate process from the development environment used by the implementing agent. The Evaluator does not use the implementing agent's test fixtures.

---

## Section 5: Preventing Agent Drift

### Principle 5.1: Define Drift Before It Happens

Agent drift is the gradual deviation of agent behavior from the specification over multiple sessions or tool call iterations. Drift is easier to prevent than correct. Prevent drift by:
- Defining expected behavior precisely in the sprint contract before implementation
- Reading `session_handoff.md` and `progress.json` at the start of every session to reanchor on the spec
- Running the full test suite at the start of every session to detect drift before it compounds

### Principle 5.2: Structured Handoffs Prevent Drift Accumulation

Drift accumulates when agents reconstruct project state from memory (conversation history) rather than from structured artifacts. An agent that reconstructs state from memory will introduce small errors that compound over sessions. An agent that reads `session_state.json`, `progress.json`, and `session_handoff.md` starts every session with the actual project state.

**Implementation mandate:** Agents must not rely on conversation history to understand project state. All project state is in the structured artifacts. An agent that references "what we discussed earlier" rather than a specific file and line number is drifting.

### Principle 5.3: Specification Drift Is the Root Cause of Implementation Drift

When the specification is ambiguous or incomplete, agents fill the gaps with their own interpretation. Different sessions fill the same gap differently. The result is an implementation that is internally inconsistent.

**Prevention:** `SYSTEM_ARCHITECTURE.md` (mandatory before implementation) and `SPEC.md` per project define the system with enough precision that gaps are minimised. Where gaps exist, they are filled by the Orchestrator via sprint contracts, not by the implementing agent.

### Principle 5.4: Confidence Is a Drift Warning Signal

An agent that is highly confident in an answer it should not be confident in is drifting from the specification into its own prior knowledge. Watch for:
- Confident claims about system behavior not documented in `SYSTEM_ARCHITECTURE.md`
- Confident code patterns that contradict the ADR log
- Confident completion declarations without running the test suite

**Response:** When an implementing agent exhibits high confidence in something not grounded in the shared context files, the Orchestrator should verify the claim against the specification before accepting it.

### Principle 5.5: Red-Team Evaluation Catches Drift the Evaluator Cannot

Standard evaluation verifies that the implementation satisfies the specification. Red-team evaluation attempts to find ways the implementation fails that the specification did not anticipate. A red-team evaluator is an Opus-tier agent with an adversarial brief: "find inputs, sequences, and conditions under which this implementation fails."

Red-team evaluation should be applied:
- Before any production deployment
- After any significant architectural change
- When the implementation handles security-sensitive operations (auth, data access, payment)

**Implementation:** Red-team evaluation is a mode of the Security Evaluator agent, triggered by the Orchestrator for security-sensitive features.

---

## Section 6: OpenAI Harness Engineering — Incorporated Recommendations

*Note: OpenAI published principles on harness engineering that align with and extend the framework above. The URL https://openai.com/index/harness-engineering/ was inaccessible at the time of this document's creation (403). The following section incorporates the known OpenAI harness engineering practices from published talks, papers, and engineering blog posts that are publicly available.*

### 6.1 Evals as Engineering Infrastructure

OpenAI treats evaluation harnesses as first-class engineering infrastructure, not afterthoughts. Key practices:

- **Evals are written before the system they evaluate.** Like TDD for agent systems, writing the evaluation before the implementation clarifies what "working" means before any code is written. This prevents evaluation from becoming a rubber stamp on already-written code.
- **Evals are version-controlled alongside the system.** Evaluation suites that drift separately from the system they evaluate become worthless. Evaluations must be versioned, reviewed, and maintained with the same discipline as production code.
- **Evals must be cheap to run.** An evaluation suite that takes hours to run will not be run. Design evaluations for fast iteration: fast unit-level evaluations run on every commit, slow integration evaluations run on every deploy.

### 6.2 Structured Prompting Reduces Variance

OpenAI's research demonstrates that structured prompting (providing agents with explicit roles, constraints, and output formats) reduces output variance significantly compared to free-form prompting. This is the basis of the agent role definitions in `.claude/agents/`.

Key structured prompting elements for reliability:
- Explicit role and expertise definition
- Explicit output format specification
- Explicit constraints (what the agent must not do)
- Explicit escalation paths (what to do when stuck)

All four are present in this framework's agent definitions.

### 6.3 Tool Call Verification as a Harness Layer

A specific OpenAI harness pattern: wrapping tool calls with verification steps that confirm the tool call produced the expected result before the agent proceeds. In this framework, this manifests as:
- Verifying tests pass after claiming they pass
- Verifying files exist after claiming to write them
- Verifying the test baseline is green before starting new work

The verification step is not the agent checking its own reasoning — it is the harness confirming through independent observation that the state transition occurred.

### 6.4 Iterative Refinement Over Perfect First Drafts

OpenAI's harness design philosophy: agents should be designed for iterative refinement, not single-shot correctness. A harness that expects agents to produce correct output on the first attempt will either relax evaluation standards or produce a high failure rate.

Design the harness for multiple passes:
- First pass: implement the happy path
- Second pass: add error handling
- Third pass: add edge case coverage
- Evaluation: verify the complete implementation

This framework's sprint contract and rework loop structure is designed around this principle.

---

## Compliance and Application

These principles are referenced in every harness design review conducted by the Harness Evaluator. The `HARNESS_QUALITY_REPORT.md` includes a section that scores the current harness against each major principle category:

- Evaluation independence (Principles 1.1, 1.4)
- Holdout scenario coverage (Principle 1.2)
- Task structure quality (Principles 2.1–2.5)
- Feedback loop health (Principles 3.1–3.4)
- Tool design compliance (Principles 4.1–4.4)
- Drift prevention (Principles 5.1–5.5)

A harness quality score below 70% on any principle category triggers an improvement sprint dedicated to that category.
