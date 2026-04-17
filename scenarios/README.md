# Scenarios — [PROJECT_NAME]

## Purpose

This directory contains **holdout validation scenarios** — the correctness mechanism for the agent framework.

Holdout scenarios function exactly like ML test holdout sets: implementing agents cannot see these scenarios while writing code. They can only learn whether their implementation passes them after submission. This prevents:
- Agents writing code that games the tests
- Self-referential validation loops
- Agents declaring completion based on self-assessment

**Ownership:** The Engineering Orchestrator (or human) authors all scenarios. Implementing agents never modify this directory.

---

## Directory Structure

```
scenarios/
├── README.md                    (this file)
├── [F-ID]/                      (one directory per feature)
│   ├── README.md                (scenario description and context)
│   ├── S01_happy_path.md        (primary happy-path scenario)
│   ├── S02_edge_case.md         (edge case scenario)
│   ├── S03_error_case.md        (error/failure scenario)
│   └── fixtures/                (test data, payloads, expected outputs)
│       ├── input_01.json
│       └── expected_01.json
└── runner/
    └── run_scenarios.py         (scenario runner — see Artifact Engineer outputs)
```

---

## Scenario File Format

Each scenario is a markdown file with a machine-readable header:

```markdown
# Scenario [F-ID]-S[NN]: [Scenario Name]

**Feature:** [F-ID]
**Type:** happy_path | edge_case | error_case | security | performance
**Author:** engineering-orchestrator
**Date:** [DATE]
**Status:** active

## Setup

[Description of preconditions — what state the system must be in before this scenario runs.]

## Action

[What happens — the specific input, API call, or event that triggers the behaviour under test.]

## Expected Outcome

[Specific, unambiguous expected result. Written so that the Evaluator can verify it without interpretation.]

## Verification Steps

1. [Specific check 1]
2. [Specific check 2]
3. [Specific check 3]

## Pass Criteria

This scenario PASSES if and only if all verification steps return the expected outcome.
This scenario FAILS if any verification step deviates from the expected outcome.
```

---

## Satisfaction Score Calculation

After the Evaluator runs all scenarios for a feature:

```
satisfaction_score = passing_scenarios / total_scenarios
```

| Score | Verdict | Action |
|---|---|---|
| >= 0.95 | PASS | Feature marked passing in progress.json |
| 0.80–0.94 | CONDITIONAL PASS | Log warning; proceed with documented risk |
| < 0.80 | FAIL | Route defect list to implementing agent; rework required |

---

## Rules for Scenario Authors (Orchestrator / Human)

1. Write scenarios before implementation begins — never after
2. Scenarios must test the specification's intent, not the implementation's specific approach
3. Include at least one happy-path, one edge case, and one error/failure scenario per feature
4. Expected outcomes must be specific and unambiguous — the Evaluator must be able to verify without interpretation
5. Never share scenario files with implementing agents before evaluation
6. Scenarios are immutable once signed — amendments create new scenarios (S[NN]_v2) with a note superseding the original
