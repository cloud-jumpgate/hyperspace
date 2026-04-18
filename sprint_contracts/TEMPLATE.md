# Sprint Contract — S[N]: [Sprint Name]

**Version:** 2.0
**Sprint:** S[N]
**Project:** Hyperspace
**Written by:** Engineering Orchestrator
**Date:** [DATE]
**Status:** Draft / Signed / Active / Complete / Failed

> **PURPOSE:** This contract defines what "done" means for sprint S[N]. It is verified independently by the Code Evaluator (and Security Evaluator if applicable). The implementing agent must not modify this document. Any change to acceptance criteria requires a new contract version approved by the Engineering Orchestrator.

---

## 1. Sprint Description

[One paragraph describing the sprint goal and what features it delivers. Written in plain language.]

---

## 2. Features in This Sprint

| Feature ID | Feature Name | Implementing Agent | Model |
|---|---|---|---|
| F-NNN | [Feature Name] | [agent] | claude-sonnet-4-6 |

---

## 3. Implementing Agent

| Attribute | Value |
|---|---|
| Agent | [e.g., backend-engineer] |
| Model | [e.g., claude-sonnet-4-6] |
| Context Tier | [e.g., Tier 2] |
| Sprint | S[N] |
| Must Not Touch | SYSTEM_ARCHITECTURE.md, SPEC.md, CLAUDE.md, decision_log.md, framework/ |

---

## 4. Acceptance Criteria (Must Pass)

All of the following must be true for the Evaluator to return PASS.

### 4.1 Feature F-NNN: [Feature Name]

#### Compilation and structure

- [ ] **F-NNN-C01**: [Specific compilation/structure criterion]

#### Functional criteria

- [ ] **F-NNN-F01**: [Specific, testable statement of behaviour. Written so that a second agent can independently verify it without ambiguity.]
- [ ] **F-NNN-F02**: [Specific, testable statement.]

#### Tests

- [ ] **F-NNN-T01**: [Test criterion -- specific test name, what it verifies, expected outcome]
- [ ] **F-NNN-T02**: [Test criterion]

### 4.2 Non-Functional Criteria (All Features)

- [ ] **NF-01**: `golangci-lint run` exits 0 -- zero lint errors
- [ ] **NF-02**: `make build` exits 0 -- entire module compiles cleanly
- [ ] **NF-03**: `go test -race ./...` exits 0 -- no races
- [ ] **NF-04**: Test coverage >= 85%

### 4.3 Security Criteria (if applicable)

> Applicable if this feature involves authentication, data access, external integrations, or user input.

- [ ] **SEC-01**: [Specific security criterion]
- [ ] **SEC-02**: [Specific security criterion]

---

## 5. Documentation Deliverables (Mandatory -- not deferrable to a later sprint)

- [ ] Package-level doc comments for all exported types
- [ ] `shared_knowledge.md` appended with any non-obvious discoveries
- [ ] `knowledge_base/` updated if new domain knowledge found
- [ ] `decision_log.md` updated if any ADR-worthy decisions made
- [ ] Example code updated if public API changed

---

## 6. Definition of Done

The sprint is marked complete and features are set to `code_complete` in `progress.json` ONLY when:

1. All acceptance criteria above are met and verified
2. All documentation deliverables above are complete
3. Tests pass with required coverage
4. Linter exits 0
5. Code Evaluator returns PASS
6. `progress.json` updated to `code_complete` by implementing agent (then to `evaluator_pass` after Evaluator PASS)

---

## 7. Holdout Scenarios

> These scenarios are stored in `scenarios/hyperspace/S[N]/` and are NOT visible to the implementing agent. The Evaluator runs these after implementation.

| Scenario ID | Description | Expected Outcome |
|---|---|---|
| S[N]-H01 | [Scenario name] | [Expected behaviour] |
| S[N]-H02 | [Scenario name] | [Expected behaviour] |
| S[N]-H03 | [Edge/error case] | [Expected behaviour] |

Minimum satisfaction score to pass: **0.95** (>= 95% of scenarios must pass)

---

## 8. Out of Scope

The following are explicitly out of scope for this sprint contract. They must not be implemented as part of this sprint (even if the implementing agent thinks they are related):

- [Item 1]
- [Item 2]

---

## 9. Evaluator Instructions

**Code Evaluator receives:**
- This sprint contract
- The git diff for all changes in the sprint
- Test results output

**Code Evaluator does NOT receive:**
- The implementing agent's reasoning or intermediate work
- Any context beyond the above

**Satisfaction score calculation:**
```
satisfaction_score = passing_scenarios / total_scenarios

>= 0.95  -> PASS
0.80-0.94 -> CONDITIONAL PASS (log warning to harness_telemetry.jsonl)
< 0.80   -> FAIL (route back to implementing agent with defect list)
```

---

## 10. Rework Protocol

If the Evaluator returns FAIL:

1. Evaluator writes specific defect list in HARNESS_QUALITY_REPORT.md
2. Engineering Orchestrator routes defect list to implementing agent
3. Implementing agent addresses defects (session init protocol applies)
4. Evaluator re-evaluates -- full evaluation, not just the defect items
5. Event logged to harness_telemetry.jsonl: `{"event": "rework", "sprint": "S[N]", "reason": "..."}`

---

## 11. Sign-Off

| Role | Agent | Date | Signature |
|---|---|---|---|
| Engineering Orchestrator | engineering-orchestrator | [DATE] | APPROVED / PENDING |
| Harness Evaluator (kickoff check) | harness-evaluator | [DATE] | APPROVED / PENDING |
