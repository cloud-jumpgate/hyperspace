# Model Selection Policy

**Version:** 1.1  
**Owner:** CTO  
**Status:** Mandatory — all agents must comply  
**Effective:** 2026-04-16

---

## Purpose

This policy defines which Claude model each agent type uses. Model selection is not left to individual discretion — it is an architectural decision with direct cost and quality implications. Choosing the wrong model in either direction wastes either money (over-provisioning) or quality (under-provisioning). This policy codifies the right choice for each role, with explicit rationale.

The guiding principle: **reasoning depth should match task stakes**. Orchestrators and evaluators need the deepest reasoning because they make decisions that propagate across the entire system. Workers need speed and quality, not maximum reasoning depth. Simple mechanical tasks need throughput and low cost.

---

## Model Reference

| Model | Context Window | Max Output | Best For | Cost Profile |
|---|---|---|---|---|
| `claude-opus-4-7` | 1M tokens | 128k tokens | Complex reasoning, agentic coding, architecture, independent evaluation, strategic decisions — step-change improvement in multi-step coherence over 4.6 | Highest ($5/$25 per MTok) |
| `claude-sonnet-4-6` | 1M tokens | 64k tokens | Implementation, documentation, process work, code generation — 79.6% SWE-bench, fast latency | Medium ($3/$15 per MTok) |
| `claude-haiku-4-5-20251001` | 200k tokens | 64k tokens | Mechanical tasks, formatting, simple lookups, linting analysis — near-frontier quality at lowest cost | Lowest ($1/$5 per MTok) |

---

## Policy Table

| Agent Role | Model | Rationale |
|---|---|---|
| Engineering Orchestrator | `claude-opus-4-7` | Top-level coordination; every task routing decision propagates to all workers. Errors here multiply. |
| CTO | `claude-opus-4-7` | Technology strategy, ADRs, and build-vs-buy decisions require full reasoning chains. Wrong decisions are expensive to reverse. |
| Software Architect | `claude-opus-4-7` | Architecture decisions are the hardest to change later. Must reason about failure modes, trade-offs, and long-term consequences simultaneously. |
| Harness Orchestrator | `claude-opus-4-7` | Plans harness work and coordinates the entire harness team. Equivalent to CTO for the harness domain. |
| Harness Architect | `claude-opus-4-7` | Designs harness structures and system architectures. Same stakes as Software Architect. |
| Harness Evaluator | `claude-opus-4-7` | Independent evaluation of harness quality. Evaluation is only meaningful if the evaluator is at least as capable as the producer. |
| Code Evaluator | `claude-opus-4-7` | Evaluating code correctness, architecture conformance, and security posture independently. Must not be outmatched by the code it evaluates. |
| Security Evaluator | `claude-opus-4-7` | Security analysis requires the deepest reasoning. A missed vulnerability found in production costs orders of magnitude more than the model cost difference. |
| Architecture Evaluator | `claude-opus-4-7` | Architecture conformance checking against complex specifications. |
| Backend Engineer | `claude-sonnet-4-6` | Implementation work: writing well-structured Go/Python/TypeScript code, tests, and migrations. Sonnet delivers excellent code quality at reasonable cost. |
| Frontend Engineer | `claude-sonnet-4-6` | Component implementation, state management, accessibility. Implementation quality is high with Sonnet. |
| DevOps Engineer | `claude-sonnet-4-6` | Terraform, Docker, CI/CD pipeline implementation. Infrastructure-as-code generation. |
| Data Engineer | `claude-sonnet-4-6` | Schema design, query optimisation, ETL pipeline implementation. |
| Security Engineer (implementation) | `claude-sonnet-4-6` | Writing security middleware, auth flows, input validation. Note: Security *Evaluator* role uses Opus. |
| QA Engineer | `claude-sonnet-4-6` | Writing test suites, integration tests, scenario runners. |
| AI/ML Engineer | `claude-sonnet-4-6` | RAG pipelines, LLM integration, embedding configuration. |
| Tech Writer | `claude-sonnet-4-6` | API documentation, runbooks, onboarding guides, ADR writing. |
| Repository Engineer | `claude-sonnet-4-6` | Repository scaffolding, CLAUDE.md, Makefile, CI/CD templates. |
| Documentation Engineer | `claude-sonnet-4-6` | Process documentation, guides, onboarding materials. |
| Process Engineer | `claude-sonnet-4-6` | Sprint ceremonies, checklists, deployment procedures. |
| Artifact Engineer | `claude-sonnet-4-6` | Reusable artifact creation: templates, scaffold generators. |
| Engineering PM | `claude-sonnet-4-6` | Sprint planning, DORA metrics, estimation. |
| Linting / Formatting Analysis | `claude-haiku-4-5-20251001` | Mechanical analysis of lint output, formatting violations. No reasoning depth needed. |
| Simple Lookups | `claude-haiku-4-5-20251001` | Looking up a value in a file, summarising a short log, extracting a specific field. |
| Formatting Checks | `claude-haiku-4-5-20251001` | Confirming code style compliance, checking commit message format. |
| Status Summarisation | `claude-haiku-4-5-20251001` | Summarising `progress.json` for dashboard display. Mechanical transformation, not reasoning. |
| Scenario Result Parsing | `claude-haiku-4-5-20251001` | Parsing test output into structured pass/fail records. |

---

## Rules

### Rule 1: Evaluators Must Use Opus

Any agent performing **independent evaluation** — meaning it is checking outputs it did not produce — must use `claude-opus-4-7`. Evaluating with a weaker model than the producer creates a systematic quality gap: the producer can generate errors the evaluator cannot detect.

### Rule 2: Orchestrators Must Use Opus

Orchestrators make task routing decisions that propagate to all downstream agents. An error at the orchestration level compounds across the entire session. The cost delta between Opus and Sonnet for orchestration is negligible relative to the rework cost of a mis-routed task.

### Rule 3: Workers Default to Sonnet

Workers implement, document, and produce artifacts. Sonnet delivers production-quality output for these tasks at significantly lower cost than Opus. Do not use Opus for worker tasks without explicit justification logged to `harness_telemetry.jsonl`.

### Rule 4: Haiku for Mechanical Tasks Only

Haiku is appropriate only for tasks where the correct output is unambiguous and deterministic from the input. If the task requires trade-off analysis, contextual judgment, or interpretation of ambiguous information, it is not a Haiku task. Misclassifying a reasoning task as mechanical is the most common misuse of Haiku.

### Rule 5: Model Upgrades Require Justification

If a worker agent needs to upgrade to Opus for a specific task (e.g., a particularly complex implementation requiring deep reasoning), the upgrade must be logged:

```json
{
  "session": "2026-04-13T14:00Z",
  "project": "probe-manager",
  "feature": "F007",
  "agent": "backend",
  "event": "model_upgrade",
  "from_model": "claude-sonnet-4-6",
  "to_model": "claude-opus-4-7",
  "justification": "Concurrent data structure design for lock-free aggregator requires deep reasoning about memory ordering guarantees"
}
```

### Rule 6: Context Budget Guidelines by Model

Opus 4.7 and Sonnet 4.6 support 1M token context windows; Haiku 4.5 supports 200k. Cost and latency scale with input tokens regardless of window size. Apply progressive disclosure tiers (see `PROGRESSIVE_DISCLOSURE_PROTOCOL.md`) to stay within budget:

| Model | Context Window | Target Input Budget | Maximum Input Budget |
|---|---|---|---|
| `claude-opus-4-7` | 1M tokens | 50k tokens | 200k tokens |
| `claude-sonnet-4-6` | 1M tokens | 30k tokens | 100k tokens |
| `claude-haiku-4-5-20251001` | 200k tokens | 10k tokens | 30k tokens |

These are cost-efficiency targets, not hard limits. The 1M window in Opus 4.7 is available for deep planning sessions requiring full codebase context — use it deliberately, not by default. Log any session where Opus input exceeds 100k tokens to `harness_telemetry.jsonl`.

---

## Enforcement

The Harness Evaluator checks model selection compliance as part of every harness quality report. Non-compliant model usage must be flagged in `HARNESS_QUALITY_REPORT.md` with the specific agent and task where the violation occurred.

The Engineering Orchestrator is responsible for specifying the correct model when assigning tasks to sub-agents.

---

## Revision History

| Version | Date | Change | Author |
|---|---|---|---|
| 1.1 | 2026-04-16 | Upgrade all Opus roles from `claude-opus-4-6` to `claude-opus-4-7` (released 2026-04-16). Update Model Reference table with 1M context windows for Opus 4.7 and Sonnet 4.6. Update context budget table. Haiku 4.5 and Sonnet 4.6 unchanged. | CTO |
| 1.0 | 2026-04-13 | Initial policy | CTO |
