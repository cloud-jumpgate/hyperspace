# [PROJECT_NAME] — Agent Team Composition

**Version:** 1.0
**Author:** Harness Architect
**Status:** Draft
**Last Updated:** [DATE]

> This document defines the specific agent team composition for this project. It is derived from `framework/teams/FULL_TEAM_STRUCTURE.md` and customised for this project's technology stack, risk profile, and complexity.

---

## Active Agents for This Project

### Harness Engineering Team (Always Active)

| Agent | Model | Active | Rationale |
|---|---|---|---|
| Harness Orchestrator | `claude-opus-4-7` | Yes | Required for all projects |
| Harness Architect | `claude-opus-4-7` | Yes | Required for all projects |
| Repository Engineer | `claude-sonnet-4-6` | Yes | Required for all projects |
| Documentation Engineer | `claude-sonnet-4-6` | Yes | Required for all projects |
| Process Engineer | `claude-sonnet-4-6` | Yes | Required for all projects |
| Artifact Engineer | `claude-sonnet-4-6` | Yes | Required for all projects |
| Harness Evaluator | `claude-opus-4-7` | Yes | Required for all projects |

### Software Engineering Team (Customise per Project)

| Agent | Model | Active | Rationale |
|---|---|---|---|
| Engineering Orchestrator | `claude-opus-4-7` | Yes | Top-level coordination |
| Software Architect | `claude-opus-4-7` | Yes | Architecture decisions |
| Backend Engineer | `claude-sonnet-4-6` | [Yes/No] | [Rationale] |
| Frontend Engineer | `claude-sonnet-4-6` | [Yes/No] | [Rationale] |
| DevOps Engineer | `claude-sonnet-4-6` | [Yes/No] | [Rationale] |
| Data Engineer | `claude-sonnet-4-6` | [Yes/No] | [Rationale] |
| Security Engineer | `claude-sonnet-4-6` | [Yes/No] | [Rationale] |
| QA Engineer | `claude-sonnet-4-6` | [Yes/No] | [Rationale] |
| AI/ML Engineer | `claude-sonnet-4-6` | [Yes/No] | [Rationale] |
| Tech Writer | `claude-sonnet-4-6` | [Yes/No] | [Rationale] |
| Engineering PM | `claude-sonnet-4-6` | [Yes/No] | [Rationale] |

### Evaluator Team (Always Active)

| Agent | Model | Trigger |
|---|---|---|
| Code Evaluator | `claude-opus-4-7` | After every sprint implementation |
| Security Evaluator | `claude-opus-4-7` | After any auth/data/integration feature |
| Architecture Evaluator | `claude-opus-4-7` | Every 4 sprints; every production deploy |

---

## Agent Assignments by Feature

| Feature | F-ID | Implementing Agent | Evaluating Agent | Notes |
|---|---|---|---|---|
| [Feature Name] | F-001 | [Agent] | Code Evaluator | |
| [Feature Name] | F-002 | [Agent] | Code Evaluator + Security Evaluator | [Security-sensitive] |

---

## Knowledge Base Resources for This Team

> Populate when project activates. List the specific knowledge_base/ files most relevant to each agent.

| Agent | Primary Knowledge Resources |
|---|---|
| Software Architect | `knowledge_base/ARCHITECTURE_PATTERNS.md`, `knowledge_base/EXTERNAL_RESOURCES.md` |
| Backend Engineer | `knowledge_base/DOMAIN_KNOWLEDGE.md`, `knowledge_base/SECURITY.md` |
| Security Engineer | `knowledge_base/SECURITY.md` |
| [Agent] | [Resources] |
