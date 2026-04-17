# Knowledge Base Index — [PROJECT_NAME]

**Status:** Empty — seed when project activates
**Owner:** Harness Architect
**Last Updated:** [DATE]

> This index is the master reference for all domain knowledge relevant to this project. It is updated by the Harness Architect whenever an agent discovers new knowledge worth preserving. Agents reference this index to find relevant resources before beginning implementation tasks.

---

## How to Use This Index

1. Before starting any implementation task, check this index for relevant resources
2. If you discover knowledge not captured here, note it in `session_handoff.md`
3. The Harness Architect will add it to the appropriate knowledge_base/ file and update this index
4. Never add knowledge directly to this index without also creating or updating the source file

---

## Resource Categories

| Category | File | Description | Status |
|---|---|---|---|
| Domain Knowledge | `DOMAIN_KNOWLEDGE.md` | Project-specific business domain concepts | Empty — seed on activation |
| Security | `SECURITY.md` | Security references, checklists, patterns | Empty — seed on activation |
| Architecture Patterns | `ARCHITECTURE_PATTERNS.md` | Patterns relevant to this project's tech stack | Empty — seed on activation |
| External Resources | `EXTERNAL_RESOURCES.md` | Links to papers, standards, docs, APIs | Empty — seed on activation |

---

## Seeding Instructions

When this project activates, the Harness Architect seeds each knowledge base file with:

### DOMAIN_KNOWLEDGE.md
- Business domain terminology specific to this project
- Key entities and their relationships (in plain language)
- Domain constraints and invariants
- Links to any domain specification documents

### SECURITY.md
- Language-specific security checklist (Go / Python / TypeScript)
- OWASP references relevant to this project's risk surface
- Authentication/authorisation pattern documentation
- Known vulnerable patterns to avoid
- Dependency vulnerability scanning instructions

### ARCHITECTURE_PATTERNS.md
- Design patterns used in this project with rationale
- Anti-patterns explicitly rejected (and why)
- Links to reference implementations
- Technology-specific best practices

### EXTERNAL_RESOURCES.md
- Links to all external APIs this project integrates with
- Relevant RFC / standards documents
- Research papers that informed architectural decisions
- Case studies from similar systems

---

## Resource Addition Protocol

Any agent that discovers a resource worth adding:

1. Notes it in `session_handoff.md` under "New Domain Knowledge Discovered"
2. Does NOT add it directly to knowledge_base/ files
3. The Harness Architect reviews the note and adds the resource at the next harness maintenance cycle
4. The Harness Architect updates this index
