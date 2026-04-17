# Sprint Contract — [F-ID]: [Feature Name]

**Version:** 1.0
**Feature ID:** [F-ID]
**Sprint:** [S-ID]
**Project:** [PROJECT_NAME]
**Written by:** Engineering Orchestrator
**Date:** [DATE]
**Status:** Draft / Signed / Active / Complete / Failed

> **PURPOSE:** This contract defines what "done" means for [F-ID]. It is negotiated before implementation begins and verified independently by the Code Evaluator (and Security Evaluator if applicable). The implementing agent must not modify this document. Any change to acceptance criteria requires a new contract version approved by the Engineering Orchestrator.

---

## 1. Feature Description

[One paragraph describing what this feature does, from the user's perspective. Written in plain language.]

---

## 2. Implementing Agent

| Attribute | Value |
|---|---|
| Agent | [e.g., backend-engineer] |
| Model | [e.g., claude-sonnet-4-6] |
| Context Tier | [e.g., Tier 2] |
| Sprint | [S-ID] |
| Must Not Touch | [List of files/components off-limits for this feature] |

---

## 3. Acceptance Criteria (Must Pass)

All of the following must be true for the Evaluator to return PASS.

### 3.1 Functional Criteria

- [ ] **[Criterion ID]**: [Specific, testable statement of behaviour. Written so that a second agent can independently verify it without ambiguity.]
- [ ] **[Criterion ID]**: [Specific, testable statement.]
- [ ] **[Criterion ID]**: [Specific, testable statement.]

### 3.2 Non-Functional Criteria

- [ ] **Test coverage**: New code must have ≥ 80% line coverage
- [ ] **No regressions**: All existing tests continue to pass
- [ ] **Lint**: `make lint` exits 0
- [ ] **Build**: `make build` exits 0

### 3.3 Security Criteria (if applicable)

> Applicable if this feature involves authentication, data access, external integrations, or user input.

- [ ] **Input validation**: All user-supplied inputs validated before use
- [ ] **No secrets in code**: No credentials, keys, or secrets in source files
- [ ] **Auth enforced**: All new endpoints require appropriate authentication
- [ ] **Security Evaluator PASS**: Security Evaluator must return PASS before feature is marked passing

---

## 4. Definition of Done

The feature is marked `passing` in `progress.json` ONLY when:

1. All acceptance criteria above are met
2. Code Evaluator returns PASS
3. Security Evaluator returns PASS (if applicable)
4. Engineering Orchestrator approves delivery gate
5. `progress.json` updated to `passing` by implementing agent

---

## 5. Holdout Scenarios

> These scenarios are stored in `scenarios/[PROJECT_NAME]/[F-ID]/` and are NOT visible to the implementing agent. The Evaluator runs these after implementation.

| Scenario ID | Description | Expected Outcome |
|---|---|---|
| [F-ID]-S01 | [Scenario name] | [Expected behaviour] |
| [F-ID]-S02 | [Scenario name] | [Expected behaviour] |
| [F-ID]-S03 | [Edge/error case] | [Expected behaviour] |

Minimum satisfaction score to pass: **0.95** (≥ 95% of scenarios must pass)

---

## 6. Out of Scope

The following are explicitly out of scope for this sprint contract. They must not be implemented as part of this feature (even if the implementing agent thinks they are related):

- [Item 1]
- [Item 2]

---

## 7. Evaluator Instructions

**Code Evaluator receives:**
- This sprint contract
- The git diff for this feature
- Test results (`make test` output)

**Code Evaluator does NOT receive:**
- The implementing agent's reasoning or intermediate work
- Any context beyond the above

**Security Evaluator receives (if applicable):**
- This sprint contract
- The git diff for this feature
- `knowledge_base/SECURITY.md`

**Satisfaction score calculation:**
```
satisfaction_score = passing_scenarios / total_scenarios

>= 0.95  → PASS
0.80–0.94 → CONDITIONAL PASS (log warning to harness_telemetry.jsonl)
< 0.80   → FAIL (route back to implementing agent with defect list)
```

---

## 8. Rework Protocol

If the Evaluator returns FAIL:

1. Evaluator writes specific defect list in HARNESS_QUALITY_REPORT.md
2. Engineering Orchestrator routes defect list to implementing agent
3. Implementing agent addresses defects (session init protocol applies)
4. Evaluator re-evaluates — full evaluation, not just the defect items
5. Event logged to harness_telemetry.jsonl: `{"event": "rework", "f_id": "[F-ID]", "reason": "..."}`

---

## 9. Sign-Off

| Role | Agent | Date | Signature |
|---|---|---|---|
| Engineering Orchestrator | engineering-orchestrator | [DATE] | APPROVED / PENDING |
| Harness Evaluator (kickoff check) | harness-evaluator | [DATE] | APPROVED / PENDING |
