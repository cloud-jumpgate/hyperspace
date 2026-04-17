# Progressive Disclosure Protocol

**Version:** 1.0  
**Owner:** CTO / Harness Architect  
**Status:** Mandatory  
**Effective:** 2026-04-13

---

## Purpose

Context window budget is a finite engineering resource. Filling an agent's context with everything we know about the project is as wrong as filling every function call with every variable in scope — it degrades signal with noise. Progressive disclosure is the discipline of giving agents exactly the context they need for the task at hand, no more and no less.

This protocol defines three tiers of context, rules for which tier each agent receives, how agents request additional context, and how to write the summaries that enable each tier.

The protocol solves a real problem observed in the current harness: agents given full project context on every task exhibit context noise, where relevant information is diluted by irrelevant history, producing inconsistent decisions. Agents given insufficient context hallucinate missing details. The goal is precision.

---

## The Three Tiers

### Tier 1 — Micro Context

**Always included. Every agent. Every task.**  
**Budget: < 500 tokens**

Micro context contains only the minimum information needed to orient an agent to its role and immediate task. An agent with only Tier 1 context knows what it is, what it must do right now, and where to find more information if needed. Nothing else.

**Contents:**

```markdown
## Micro Context (Tier 1)

**Project:** [project name]
**Sprint Goal:** [one sentence — what this sprint delivers]
**Agent Role:** [exact role name, e.g., "Backend Engineer"]
**Immediate Task:** [specific task description — one paragraph maximum]
**Task ID:** [F-ID from progress.json]
**Status Required:** [e.g., "Complete implementation and update progress.json"]
**Context Tier:** 1 of 3 — request Tier 2 if you need architecture details
```

**Who receives Tier 1 only:**
- Haiku-model agents performing mechanical tasks (linting, formatting, status summarisation)
- Worker agents on tasks where the task brief is self-contained (e.g., "fix this specific bug described below")

**Who receives Tier 1 as a baseline (plus higher tiers):**
- All other agents — Tier 1 is always the opening block of any context package

---

### Tier 2 — Meso Context

**Included when the task requires architectural or codebase understanding.**  
**Budget: < 2000 tokens (including Tier 1)**

Meso context adds the architectural summary, relevant component descriptions, current tech debt affecting this task, and recent decisions. It does not include the full architecture document or historical ADR log.

**Contents:**

```markdown
## Meso Context (Tier 2)

### Architecture Summary
[3–5 sentence summary of the system: what it is, main components, key constraints]

### Relevant Components
[Only the components this agent will touch or depend on]
- **[Component Name]** (`path/to/component`): [one sentence description, current state]
- **[Component Name]** (`path/to/component`): [one sentence description, current state]

### Recent Decisions Affecting This Task
- [ADR-NNN]: [Decision summary — one sentence, link to full ADR]
- [ADR-NNN]: [Decision summary — one sentence, link to full ADR]

### Tech Debt in Scope
- [TD-NNN] [Severity]: [Description] — [notes if relevant to current task]

### Session Context
- Last session summary: [one paragraph from session_handoff.md]
- Current test status: [passing / N tests failing]
- Do not touch: [specific files or functions flagged as off-limits]
```

**Who receives Tier 2:**
- Worker agents (Backend, Frontend, DevOps, Data Engineer, QA) on implementation tasks
- Documentation Engineer writing technical docs
- Process Engineer designing workflows for a specific system
- Repository Engineer scaffolding a new project

**Trigger for Tier 2 inclusion:** The task description references a specific component, requires knowledge of the system architecture, or involves changes to existing code.

---

### Tier 3 — Macro Context

**Included for planning, architecture, and evaluation tasks.**  
**Budget: < 5000 tokens (including Tiers 1 and 2)**

Macro context adds the full system architecture (or a structured summary of it), all ADRs, the full knowledge base index with pointers to relevant resources, and cross-project dependencies. This is the full picture — agents with Tier 3 can make authoritative architectural decisions.

**Contents:**

```markdown
## Macro Context (Tier 3)

### Full System Architecture
[Either embed SYSTEM_ARCHITECTURE.md sections directly, or reference and summarise all 7 mandatory sections]

### Full ADR Log
[All ADRs with their current status — see decision_log.md]

### Knowledge Base Index
[Relevant domain entries from knowledge_base/INDEX.md]

### Cross-Project Dependencies
| This Project Uses | From Project | Interface | Notes |
|---|---|---|---|
| [dependency] | [project] | [API / shared DB / library] | [notes] |

| Project Using This | Consumer | Interface | Notes |
|---|---|---|---|
| [consumer] | [project] | [API / shared DB / library] | [notes] |

### Harness State
- Active sprint: [sprint name / ID]
- Features in progress: [list from progress.json]
- Blocked features: [list with blocker description]
- Satisfaction score (last eval): [N%]
```

**Who receives Tier 3:**
- CTO / Engineering Orchestrator (always)
- Harness Orchestrator (always)
- Software Architect (always)
- Harness Architect (always)
- All Evaluator agents (Code Evaluator, Security Evaluator, Architecture Evaluator, Harness Evaluator)
- Worker agents explicitly working on architecture, planning, or cross-component design tasks

**Trigger for Tier 3 inclusion:** The task involves architectural decisions, cross-component changes, evaluation, or sprint planning.

---

## Default Tier Assignment

| Agent | Default Tier | Can Escalate To |
|---|---|---|
| Engineering Orchestrator | 3 | — (already maximum) |
| CTO | 3 | — |
| Software Architect | 3 | — |
| Harness Orchestrator | 3 | — |
| Harness Architect | 3 | — |
| Harness Evaluator | 3 | — |
| Code Evaluator | 3 | — |
| Security Evaluator | 3 | — |
| Architecture Evaluator | 3 | — |
| Backend Engineer | 2 | 3 (architecture/planning tasks) |
| Frontend Engineer | 2 | 3 (architecture/planning tasks) |
| DevOps Engineer | 2 | 3 (infrastructure design tasks) |
| Data Engineer | 2 | 3 (schema design tasks) |
| Security Engineer (impl.) | 2 | 3 (threat modelling tasks) |
| QA Engineer | 2 | 3 (test strategy tasks) |
| AI/ML Engineer | 2 | 3 (pipeline design tasks) |
| Tech Writer | 2 | 3 (architecture docs tasks) |
| Repository Engineer | 2 | 3 (new project setup) |
| Documentation Engineer | 2 | 3 (cross-project docs) |
| Process Engineer | 2 | 3 (new process design) |
| Artifact Engineer | 2 | 3 (new artifact design) |
| Engineering PM | 2 | 3 (sprint planning) |
| Haiku agents | 1 | 2 (if task is ambiguous) |

---

## Escalation Protocol

An agent requests additional context tier when:
1. Its task references components or decisions not covered in its current tier
2. It encounters an ambiguity that architectural knowledge would resolve
3. It needs to make a decision with cross-component implications
4. It identifies a dependency or constraint not captured in its current context

**Escalation request format:**

An agent that needs additional context appends this structured block to its output before pausing:

```
CONTEXT_ESCALATION_REQUEST:
  Current tier: [1 / 2]
  Requesting tier: [2 / 3]
  Reason: [one sentence — what specific information is missing and why it is needed]
  Unblocked when: [what specific information would unblock this task]
  Task will resume: [immediately / after architecture review / at next session]
```

The Orchestrator receives the escalation request and either:
- Provides the requested context tier in the next message
- Resolves the ambiguity directly if it can do so without providing full tier context
- Escalates to the Architect if the request requires architectural decision-making

**Agents must not assume or hallucinate missing context.** If the information is not in the provided context tier and escalation is needed, the agent escalates rather than guessing.

---

## Writing Summaries for Each Tier

### How to Write a Good Tier 1 Summary

The Tier 1 summary is written by the Orchestrator when assigning a task. Rules:
- Sprint goal must be one sentence — not a paragraph, not a list
- Immediate task must be specific enough that the agent knows exactly what to do without asking questions
- Include the F-ID so the agent can update `progress.json` correctly
- If the task brief is ambiguous, the Orchestrator resolves the ambiguity before assigning — not after

### How to Write a Good Tier 2 Summary

The Tier 2 architecture summary and component descriptions are generated from `SYSTEM_ARCHITECTURE.md` by the Harness Architect and stored in `CONTEXT_SUMMARY.md` (see below). Rules:
- Architecture summary: 3–5 sentences covering what the system does, main components, and key non-negotiable constraints
- Component descriptions: only include components the agent will interact with
- Recent decisions: only ADRs from the last 90 days, or ADRs directly relevant to the task
- Tech debt: only items in the current sprint or flagged as relevant to the task

### How to Write a Good Tier 3 Summary

Tier 3 is the full picture, but still structured. The Macro Context section must not be a wall of text — it must be organized under the mandatory headings so agents can navigate it. The Architecture section should reference `SYSTEM_ARCHITECTURE.md` by section number rather than reproducing the full document inline when the context budget is tight.

---

## CONTEXT_SUMMARY.md Per Project

Every project has a `CONTEXT_SUMMARY.md` at its root. This is the auto-generated progressive context document. It is updated by the Harness Architect after each significant change to the architecture or codebase state.

```markdown
# [Project Name] — Context Summary

**Auto-generated by:** Harness Architect  
**Last updated:** [date]  
**Source documents:** SYSTEM_ARCHITECTURE.md, progress.json, decision_log.md

---

## TIER 1 BLOCK

**Project:** [project name]  
**Current sprint goal:** [one sentence]  
**Full architecture:** See SYSTEM_ARCHITECTURE.md  
**Current state:** See progress.json  

---

## TIER 2 BLOCK

### Architecture in 5 Sentences
[Written by Harness Architect — concise system summary]

### Component Registry
| Component | Path | Status | Notes |
|---|---|---|---|
| [name] | [path] | [stable / in progress / broken] | [notes] |

### Active ADRs (last 90 days)
- ADR-[NNN]: [Decision] — [Status]

### Current Tech Debt in Sprint
- [TD-NNN] [Severity]: [Description]

### Last Session Summary
[From session_handoff.md — most recent entry]

---

## TIER 3 BLOCK

### System Architecture Reference
[Full SYSTEM_ARCHITECTURE.md or structured summary]

### Full ADR Log
[All ADRs from decision_log.md]

### Knowledge Base Pointers
[Relevant entries from knowledge_base/INDEX.md]

### Cross-Project Dependencies
[Table from Section 3 of SYSTEM_ARCHITECTURE.md]
```

---

## Context Window Budget Guidelines

Although all three Claude models share a 200k token context window, the cost and speed implications of context size vary. Respect these guidelines:

| Model | Tier 1 Budget | Tier 2 Budget | Tier 3 Budget | Hard Max |
|---|---|---|---|---|
| `claude-opus-4-7` | 500 tokens | 2000 tokens | 5000 tokens | 100k tokens |
| `claude-sonnet-4-6` | 500 tokens | 2000 tokens | 5000 tokens | 80k tokens |
| `claude-haiku-4-5-20251001` | 500 tokens | 2000 tokens | N/A (no Tier 3 for Haiku) | 30k tokens |

**Budget notes:**
- These budgets are for the context package provided to the agent, not the full conversation
- Tool outputs, code files, and test results add to the total — budget the context package to leave room for these
- If a Tier 3 context package exceeds 5000 tokens, summarise the architecture section rather than embedding it in full — link to the source document
- Opus's higher cost makes context budget discipline more financially significant; a 50k-token Opus context costs approximately 10x a 5k-token context at the same tier

---

## Compliance

The Harness Evaluator checks:
- All agent task assignments specify the tier provided
- Worker agents on mechanical tasks are not receiving Tier 3 unnecessarily
- Escalation requests are logged in `harness_telemetry.jsonl`
- `CONTEXT_SUMMARY.md` exists and is updated after each architectural change
