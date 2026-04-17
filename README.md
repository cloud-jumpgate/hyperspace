# [PROJECT_NAME]

> **SETUP INSTRUCTION:** Replace all `[PROJECT_NAME]`, `[SHORT_DESCRIPTION]`, `[LANGUAGE]`, `[FRAMEWORK]` placeholders before first use.

[SHORT_DESCRIPTION — one paragraph explaining what this project does, for whom, and why it exists.]

---

## Quick Start

### Prerequisites

- [LANGUAGE] [VERSION]+
- Docker and Docker Compose
- [ANY_OTHER_PREREQUISITES]

### Setup

```bash
# Clone the repository
git clone [REPO_URL]
cd [PROJECT_NAME]

# Copy environment template
cp .env.example .env
# Edit .env with your values

# Start dependencies
make docker-run

# Run migrations (if applicable)
make migrate

# Verify setup
make test
```

### Running Locally

```bash
make run
```

### Running Tests

```bash
make test        # Full test suite
make lint        # Linters and formatters
```

---

## Architecture

Full system architecture: [`SYSTEM_ARCHITECTURE.md`](SYSTEM_ARCHITECTURE.md)

Key components:

| Component | Description |
|---|---|
| [COMPONENT_1] | [Description] |
| [COMPONENT_2] | [Description] |

---

## Project Structure

```
[PROJECT_NAME]/
├── CLAUDE.md                    # Framework rules (read by Claude Code automatically)
├── SPEC.md                      # Functional and non-functional requirements
├── SYSTEM_ARCHITECTURE.md       # Full system architecture with diagrams
├── AGENT_TEAM.md                # Agent team composition for this project
├── CONTEXT_SUMMARY.md           # Progressive disclosure context (Tier 1/2/3)
├── progress.json                # Feature-level status tracking
├── session_state.json           # Current session state (machine-readable)
├── session_handoff.md           # Narrative handoff between sessions
├── decision_log.md              # All ADRs, timestamped
├── shared_knowledge.md          # Accumulated domain knowledge (append-only)
├── harness_telemetry.jsonl      # Event log (append-only)
├── framework/                   # Engineering framework documents
│   ├── MODEL_SELECTION_POLICY.md
│   ├── PROGRESSIVE_DISCLOSURE_PROTOCOL.md
│   ├── AGENT_CONTEXT_ARCHITECTURE.md
│   ├── HARNESS_ENGINEERING_PRINCIPLES.md
│   ├── SYSTEM_ARCHITECTURE_TEMPLATE.md
│   └── teams/
│       ├── FULL_TEAM_STRUCTURE.md
│       └── HARNESS_ENGINEERING_TEAM.md
├── .claude/
│   └── agents/                  # Agent role definitions
├── knowledge_base/              # Domain knowledge (seeded when project activates)
│   ├── INDEX.md
│   ├── DOMAIN_KNOWLEDGE.md
│   ├── SECURITY.md
│   ├── ARCHITECTURE_PATTERNS.md
│   └── EXTERNAL_RESOURCES.md
├── sprint_contracts/            # Pre-negotiated acceptance criteria per feature
├── scenarios/                   # Holdout validation scenarios (Evaluator-only)
├── src/                         # Application source code
├── tests/                       # Test suite
├── Makefile                     # Standard developer commands
├── Dockerfile                   # Multi-stage production build
├── docker-compose.yml           # Local development environment
└── .env.example                 # Required environment variables (no values)
```

---

## Environment Variables

See [`.env.example`](.env.example) for all required variables with descriptions.

---

## Contributing

This project operates under the Engineering Department agent framework. Before contributing:

1. Read [`CLAUDE.md`](CLAUDE.md) — the framework rules
2. Read [`SYSTEM_ARCHITECTURE.md`](SYSTEM_ARCHITECTURE.md) — understand the design
3. Read [`SPEC.md`](SPEC.md) — understand the requirements
4. Check [`progress.json`](progress.json) — understand current sprint status

All changes require:
- Tests passing (`make test`)
- Linting passing (`make lint`)
- Code Evaluator PASS verdict before merge
