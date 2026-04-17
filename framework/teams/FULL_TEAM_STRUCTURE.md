# Full Team Structure

**Version:** 1.0  
**Owner:** CTO / Engineering Orchestrator  
**Status:** Active  
**Effective:** 2026-04-13

---

## Overview

This document defines the complete agent team structure for the Software Development & Engineering Department, including all three sub-teams, their hierarchical relationships, handoff protocols, shared context mechanisms, and governance model.

The department operates as a simulated multi-agent organisation. The Engineering Orchestrator is the apex coordinator. Three specialist teams report to it: the Harness Engineering Team (builds the factory), the Software Engineering Team (builds the product), and the Evaluator Team (provides independent quality assurance across both).

---

## Full Team Hierarchy

```
Engineering Orchestrator (claude-opus-4-7)
│
├── Harness Engineering Team
│   ├── Harness Orchestrator (claude-opus-4-7)
│   ├── Harness Architect (claude-opus-4-7)
│   ├── Repository Engineer (claude-sonnet-4-6)
│   ├── Documentation Engineer (claude-sonnet-4-6)
│   ├── Process Engineer (claude-sonnet-4-6)
│   ├── Artifact Engineer (claude-sonnet-4-6)
│   └── Harness Evaluator (claude-opus-4-7)
│
├── Software Engineering Team
│   ├── Software Architect (claude-opus-4-7)
│   ├── Backend Engineer (claude-sonnet-4-6)
│   ├── Frontend Engineer (claude-sonnet-4-6)
│   ├── DevOps Engineer (claude-sonnet-4-6)
│   ├── Data Engineer (claude-sonnet-4-6)
│   ├── Security Engineer (claude-sonnet-4-6)
│   ├── QA Engineer (claude-sonnet-4-6)
│   ├── AI/ML Engineer (claude-sonnet-4-6)
│   ├── Tech Writer (claude-sonnet-4-6)
│   └── Engineering PM (claude-sonnet-4-6)
│
└── Evaluator Team
    ├── Code Evaluator (claude-opus-4-7)
    ├── Security Evaluator (claude-opus-4-7)
    └── Architecture Evaluator (claude-opus-4-7)
```

---

## Engineering Orchestrator

**Model:** `claude-opus-4-7`  
**Role:** Top-level coordinator for the entire department  
**Reports to:** Human (user)

The Engineering Orchestrator is the CTO in its coordination role. It:
- Receives all user requests and translates them into work for the three sub-teams
- Decides which team(s) work on a given request
- Manages cross-team dependencies and conflicts
- Gates all deliveries to the user (nothing is delivered without the Orchestrator's approval)
- Maintains the ENGINEERING_MEMORY_BANK.md

**Decision authority:**
- Which team handles a given task
- Whether to run the Harness Engineering Team before the Software Engineering Team (always yes for new projects)
- Whether to run the Evaluator Team (always yes before delivery)
- Whether to accept or reject delivery (gate function)
- Technical debt prioritisation across all teams

---

## Team Roles and Responsibilities

### Harness Engineering Team

Full definitions in `framework/teams/HARNESS_ENGINEERING_TEAM.md`.

| Agent | Model | Primary Responsibility |
|---|---|---|
| Harness Orchestrator | Opus | Plan harness work, coordinate harness team, make harness architecture decisions |
| Harness Architect | Opus | Design system architecture, create SYSTEM_ARCHITECTURE.md, define feature decomposition |
| Repository Engineer | Sonnet | Repository structure, CLAUDE.md, Makefile, CI/CD scaffold |
| Documentation Engineer | Sonnet | Process docs, runbooks, API docs, ADR entries |
| Process Engineer | Sonnet | Sprint ceremonies, deployment checklists, quality gates |
| Artifact Engineer | Sonnet | Code templates, Makefile targets, Docker/CI templates, init.sh, scenario runner |
| Harness Evaluator | Opus | Independent evaluation of harness quality, HARNESS_QUALITY_REPORT.md |

**When this team runs:**
- New project kickoff (always first)
- End of every sprint (Harness Evaluator runs)
- Harness improvement request from Engineering Orchestrator
- After post-incident requiring process or architecture changes

### Software Engineering Team

Full definitions in `ENGINEERING_DEPARTMENT_FRAMEWORK.md`.

| Agent | Model | Primary Responsibility |
|---|---|---|
| Software Architect | Opus | System design, ADRs, API contracts, data modelling, failure mode analysis |
| Backend Engineer | Sonnet | API implementation, business logic, auth flows, database interaction |
| Frontend Engineer | Sonnet | React/Next.js components, state management, accessibility |
| DevOps Engineer | Sonnet | Docker, Kubernetes, Terraform, CI/CD, monitoring, cloud infrastructure |
| Data Engineer | Sonnet | Database schema, query optimisation, migrations, ETL pipelines |
| Security Engineer | Sonnet | Auth implementation, input validation, security middleware (implementation only) |
| QA Engineer | Sonnet | Test strategy, unit/integration/e2e tests, scenario runner |
| AI/ML Engineer | Sonnet | LLM integration, RAG pipelines, embeddings, ML infrastructure |
| Tech Writer | Sonnet | API docs, READMEs, runbooks, architecture docs |
| Engineering PM | Sonnet | Sprint planning, estimation, DORA metrics, retrospectives |

**When this team runs:**
- After Harness Engineering Team issues PASS verdict
- During implementation sprints
- For feature work, bug fixes, and performance work

**Prerequisite:** Harness Engineering Team PASS before any implementation begins.

### Evaluator Team

| Agent | Model | Primary Responsibility |
|---|---|---|
| Code Evaluator | Opus | Independent evaluation of code correctness, architecture conformance, test coverage |
| Security Evaluator | Opus | Independent security evaluation: OWASP, auth, input validation, dependency audit, red-team |
| Architecture Evaluator | Opus | Independent evaluation of architecture conformance against SYSTEM_ARCHITECTURE.md |

**When this team runs:**
- After every sprint implementation (before delivery gate)
- Before any production deployment
- When the Engineering Orchestrator requests an independent review
- On a schedule: Architecture Evaluator runs every 4 sprints regardless

**Critical isolation rule:** Evaluators must not have been involved in creating the artifacts they evaluate. The Code Evaluator evaluates code it did not write. The Security Evaluator evaluates security controls it did not implement. This isolation is the source of the Evaluator Team's value.

---

## Handoff Protocols

### Protocol 1: User Request → Engineering Orchestrator

**Trigger:** User submits a request  
**Duration:** Single Orchestrator session

```
1. Engineering Orchestrator reads:
   - ENGINEERING_MEMORY_BANK.md (persistent state)
   - progress.json for all active projects
   - session_handoff.md for all active projects
   
2. Orchestrator classifies the request:
   - New project → Protocol 2 (Project Kickoff)
   - Feature request on existing project → Protocol 3 (Sprint Assignment)
   - Bug fix → Protocol 4 (Bug Fix Sprint)
   - Review/evaluation request → Protocol 5 (Evaluation)
   - Architecture question → Orchestrator answers directly or routes to Software Architect
   
3. Orchestrator plans and assigns work
4. Orchestrator monitors until delivery gate
5. Orchestrator delivers to user
```

### Protocol 2: Project Kickoff

**Trigger:** New project request  
**Sequence:** Harness Engineering Team → Software Engineering Team (Architect first) → Evaluator Team

```
Step 1: Engineering Orchestrator assigns project brief to Harness Orchestrator

Step 2: Harness Engineering Team kickoff (all in parallel where possible):
  - Harness Architect: SYSTEM_ARCHITECTURE.md, SPEC.md, progress.json, decision_log.md, scenarios/
  - Repository Engineer: repository structure, CLAUDE.md, Makefile, CI/CD (depends on Architect complete)
  - Documentation Engineer: runbook templates, ADR template (depends on Architect complete)
  - Process Engineer: PROCESSES.md, sprint_protocol.md (depends on Architect complete)
  - Artifact Engineer: init.sh, scenario runner, project scaffold (depends on Architect complete)

Step 3: Harness Evaluator runs kickoff evaluation → HARNESS_QUALITY_REPORT.md
  - If FAIL: Harness Orchestrator assigns remediation; return to Step 2
  - If PASS: proceed to Step 4

Step 4: Engineering Orchestrator notifies Software Engineering Team
  Software Architect reads SYSTEM_ARCHITECTURE.md and SPEC.md
  Software Architect proposes any architectural modifications
  If modifications accepted: Harness Architect updates SYSTEM_ARCHITECTURE.md

Step 5: First implementation sprint begins (Protocol 3)
```

### Protocol 3: Sprint Assignment (Implementation)

**Trigger:** Sprint planning or feature assignment  
**Sequence:** Orchestrator planning → Implementer execution → Evaluator gate → Delivery

```
Step 1: Engineering Orchestrator selects next feature from progress.json (status: not_started or failing)

Step 2: Orchestrator writes sprint contract to sprint_contracts/[F-ID].md

Step 3: Architecture Evaluator reviews sprint contract against SYSTEM_ARCHITECTURE.md
  - If sprint contract conflicts with architecture: escalate to CTO
  - If aligned: proceed

Step 4: Orchestrator assigns implementation to appropriate Software Engineering Team agent(s)
  Agent receives: Tier 2 context package, sprint contract, "do not touch" list from shared_knowledge.md

Step 5: Implementing agent runs session init protocol:
  - Reads session_state.json, session_handoff.md, progress.json
  - Runs full test suite (must be green before starting)
  - Reads sprint contract
  - Implements feature
  - Runs full test suite (must be green before declaring complete)
  - Updates progress.json status to in_progress
  - Writes session_handoff.md
  - Appends to harness_telemetry.jsonl

Step 6: Code Evaluator runs independent evaluation:
  - Receives: sprint contract, code diff, test results
  - Does NOT receive: implementing agent's reasoning
  - Runs: tests, linting, architecture conformance check against SYSTEM_ARCHITECTURE.md
  - Returns: PASS or FAIL with specific defects

Step 7: Security Evaluator runs security evaluation (for auth, data access, integration features):
  - Evaluates against security checklist in knowledge_base/SECURITY.md
  - Returns: PASS or FAIL with specific vulnerabilities

Step 8: If any evaluator returns FAIL:
  - Orchestrator routes specific defect list back to implementing agent
  - Return to Step 5
  - Log rework event to harness_telemetry.jsonl

Step 9: If all evaluators return PASS:
  - Implementing agent updates progress.json status to passing
  - Orchestrator approves delivery gate
  - Harness Evaluator appends satisfaction score to harness_telemetry.jsonl
```

### Protocol 4: Bug Fix Sprint

```
Step 1: Bug is logged to ENGINEERING_MEMORY_BANK.md tech debt register with F-ID

Step 2: Orchestrator writes sprint contract for the bug fix:
  - Must pass: specific reproduction scenario returns expected behavior
  - Must not regress: all existing tests continue to pass

Step 3: Security Evaluator assesses whether the bug has security implications
  - If yes: security fix sprint takes priority over feature sprints

Step 4: Implementing agent fixes the bug following session init protocol

Step 5: Code Evaluator confirms the sprint contract criteria pass and no regressions

Step 6: Update progress.json and append telemetry event
```

### Protocol 5: Evaluation Request

```
Step 1: Engineering Orchestrator specifies evaluation scope

Step 2: Assign to appropriate evaluator(s):
  - Code quality: Code Evaluator
  - Security posture: Security Evaluator
  - Architecture conformance: Architecture Evaluator
  - Harness quality: Harness Evaluator

Step 3: Evaluators run independently; do not share reasoning with each other during evaluation

Step 4: Engineering Orchestrator aggregates evaluation reports

Step 5: Engineering Orchestrator routes findings to appropriate implementing agents for remediation

Step 6: Follow-up evaluation confirms remediation
```

---

## Shared Context Files

All three teams read from and write to the same shared context files. This table defines exactly who reads and writes each file:

| File | Location | Harness Team Access | SW Engineering Access | Evaluator Access |
|---|---|---|---|---|
| SYSTEM_ARCHITECTURE.md | `[project]/` | Architect: write; others: read | All: read; Architect: propose changes | All: read |
| SPEC.md | `[project]/` | Architect: write | All: read | All: read |
| AGENT_TEAM.md | `[project]/` | Architect: write | All: read | All: read |
| CONTEXT_SUMMARY.md | `[project]/` | Architect: write | All: read | All: read |
| decision_log.md | `[project]/` | Architect+Doc Eng: write | Agents propose; Orchestrator approves | All: read |
| progress.json | `[project]/` | Architect: initialise | Implementing agents: update status | All: read |
| session_state.json | `[project]/` | Architect: initialise | Implementing agents: update | All: read |
| session_handoff.md | `[project]/` | Architect: create template | Implementing agents: write per session | All: read |
| shared_knowledge.md | `[project]/` | Architect: initialise | All agents: append | All: read |
| harness_telemetry.jsonl | `[project]/` | Evaluator: read+analyze | All agents: append | Evaluator: read+analyze |
| sprint_contracts/ | `[project]/` | Orchestrator: write | Implementing: read only | Evaluators: read |
| scenarios/ | `[project]/` | Orchestrator: write | Evaluators only: read | Evaluators: read |
| HARNESS_QUALITY_REPORT.md | `[project]/` | Evaluator: write | Eng Orchestrator: read | Harness Evaluator: write |
| ENGINEERING_MEMORY_BANK.md | `[harness_root]/` | Harness team: read | Eng Orchestrator: write | All: read |
| harness_telemetry.jsonl | `[harness_root]/` | Harness Evaluator: analyze | Eng Orchestrator: read | Harness Evaluator: analyze |

---

## Governance Model

### Decision Authority Matrix

| Decision Type | Authority | Consultation Required |
|---|---|---|
| Product architecture | CTO / Software Architect | Security Evaluator (for security implications) |
| Technology selection | CTO | Software Architect, relevant implementers |
| Harness design | Harness Orchestrator | Harness Architect |
| Feature scope for sprint | Engineering Orchestrator | Engineering PM |
| Go/No-go for implementation start | Engineering Orchestrator | Harness Evaluator (must be PASS) |
| Go/No-go for delivery | Engineering Orchestrator | Code Evaluator, Security Evaluator |
| ADR acceptance | CTO | Harness Architect, relevant implementers |
| Technical debt prioritisation | CTO | Engineering PM, relevant implementers |
| Process changes | Harness Orchestrator | Process Engineer |
| Model selection policy changes | CTO | — |

### Conflict Resolution

**Conflict between teams:** Engineering Orchestrator decides. CTO perspective informs the decision.

**Conflict between framework rules:** The more specific rule takes precedence over the more general rule. If rules are at the same level of specificity, CTO decides and documents the decision as an ADR.

**Conflict between an ADR and a new requirement:** New ADR must be proposed, reviewed by CTO, and accepted before the existing ADR is superseded. The implementing agent does not resolve ADR conflicts unilaterally.

**Conflict between Evaluator PASS and Orchestrator judgment:** Evaluator PASS is necessary but not sufficient for delivery. The Orchestrator can reject a delivery that passed evaluation if there are non-technical reasons (product direction, scope). The Orchestrator cannot override a FAIL verdict to approve delivery — they must route to remediation.

### Quality Gates Summary

| Gate | Enforced By | Must Pass Before |
|---|---|---|
| Architecture exists | Harness Evaluator | Any implementation begins |
| Harness PASS verdict | Harness Evaluator | Software Engineering Team begins |
| Sprint contract signed | Harness Orchestrator | Implementation begins |
| Baseline tests green | Session init protocol | New feature work begins |
| Code Evaluator PASS | Code Evaluator | Feature marked passing |
| Security Evaluator PASS | Security Evaluator | Security-sensitive feature marked passing |
| Delivery gate | Engineering Orchestrator | User receives deliverable |
| Architecture Evaluator PASS | Architecture Evaluator | Every 4 sprints; before any production deploy |

### 20% Rule for Harness Work

At least 20% of total engineering capacity in every sprint is allocated to harness improvement and technical debt paydown. This is enforced by the Engineering Orchestrator when planning sprints. Technical debt that is not scheduled is not managed — it is merely postponed.

Implementation:
- Engineering PM tracks capacity allocation in sprint planning
- Harness Evaluator reports identify improvement candidates
- Harness Orchestrator converts improvement candidates into sprint tasks
- Engineering Orchestrator includes harness tasks alongside feature tasks in every sprint

---

## DORA Metrics Tracking

The Engineering Orchestrator tracks DORA metrics across all teams:

| Metric | How Measured | Source | Target |
|---|---|---|---|
| Deployment Frequency | Events in harness_telemetry.jsonl with event=deploy | harness_telemetry.jsonl | Daily |
| Lead Time for Changes | Time between feature F-ID creation and status=passing | progress.json timestamps | < 1 day |
| Mean Time to Recovery | Time between incident detection and resolution | incident response logs | < 1 hour |
| Change Failure Rate | Features requiring rework / total features | harness_telemetry.jsonl rework events | < 15% |

These metrics are updated in ENGINEERING_MEMORY_BANK.md at each sprint retrospective.
