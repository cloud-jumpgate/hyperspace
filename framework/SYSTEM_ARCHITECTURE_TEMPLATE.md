# System Architecture Template

**Version:** 1.0  
**Owner:** CTO / Harness Architect  
**Status:** Mandatory  
**Rule: Every project MUST have a SYSTEM_ARCHITECTURE.md using this template before implementation begins.**

---

## Purpose

This template defines the required structure for every `SYSTEM_ARCHITECTURE.md` in this engineering department. It is not optional. The Harness Architect creates this document before the first line of code is written. The Harness Evaluator checks for its presence and completeness at every project kickoff and sprint review.

Architecture documentation serves two masters: humans who need to understand the system, and agents who need machine-readable context to make correct decisions without hallucinating details. Both sets of needs are served by this template.

---

## Mandatory Sections

Every `SYSTEM_ARCHITECTURE.md` must contain all of the following sections. Optional sub-sections are marked `[OPTIONAL]`.

---

## How Agents Generate Excalidraw Diagrams

Excalidraw diagrams are embedded as JSON within markdown fenced code blocks tagged `excalidraw`. The JSON conforms to the Excalidraw file format (https://github.com/excalidraw/excalidraw/blob/master/packages/excalidraw/types.ts).

**Agent instructions for generating Excalidraw JSON:**

1. Every diagram needs a `type`, `version` (always `2`), `source` (`excalidraw`), and `elements` array.
2. Each element has: `id` (unique string), `type` (rectangle/ellipse/diamond/arrow/text/line), `x`, `y`, `width`, `height`, `angle` (usually 0), `strokeColor`, `backgroundColor`, `fillStyle`, `strokeWidth`, `strokeStyle`, `roughness` (0=clean), `opacity`, `text` (for text elements), `fontSize`, `fontFamily`.
3. Arrows need `startBinding` and `endBinding` with the `elementId` they connect to, plus `points` array.
4. Use consistent colour coding: `#1971c2` (blue) for external systems, `#2f9e44` (green) for services/containers, `#e03131` (red) for databases/storage, `#f08c00` (orange) for queues/async, `#868e96` (grey) for users/actors.
5. Keep diagrams focused: 5–12 elements per diagram. Split complex systems into multiple diagrams rather than creating one unreadable diagram.
6. After generating JSON, validate it is parseable before embedding.

---

## Template

```markdown
# [Project Name] — System Architecture

**Version:** [semver]  
**Status:** [Draft / Review / Approved]  
**Last Updated:** [date]  
**Author:** [Harness Architect agent]  
**Approved By:** [CTO / Engineering Orchestrator]

---

## 1. Overview

### 1.1 Purpose

One paragraph: what this system does, for whom, and why it exists. No jargon. A new engineer should understand the purpose after reading this paragraph.

### 1.2 System Context (C4 Level 1)

A C4 Level 1 diagram shows the system as a single box, surrounded by the users and external systems it interacts with. It answers: who uses this? what does it connect to?

[EXCALIDRAW DIAGRAM — System Context]

\`\`\`excalidraw
{
  "type": "excalidraw",
  "version": 2,
  "source": "excalidraw",
  "elements": [
    {
      "id": "user-actor",
      "type": "ellipse",
      "x": 50, "y": 200, "width": 120, "height": 80,
      "strokeColor": "#868e96", "backgroundColor": "#f1f3f5",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0,
      "opacity": 100, "angle": 0
    },
    {
      "id": "user-label",
      "type": "text",
      "x": 70, "y": 230,
      "text": "User / Client",
      "fontSize": 14, "fontFamily": 1,
      "strokeColor": "#212529", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 100, "height": 20
    },
    {
      "id": "system-box",
      "type": "rectangle",
      "x": 300, "y": 160, "width": 200, "height": 120,
      "strokeColor": "#1971c2", "backgroundColor": "#d0ebff",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0,
      "opacity": 100, "angle": 0
    },
    {
      "id": "system-label",
      "type": "text",
      "x": 340, "y": 205,
      "text": "[System Name]",
      "fontSize": 16, "fontFamily": 1,
      "strokeColor": "#1971c2", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 140, "height": 24
    },
    {
      "id": "external-system",
      "type": "rectangle",
      "x": 600, "y": 160, "width": 160, "height": 80,
      "strokeColor": "#868e96", "backgroundColor": "#f8f9fa",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0,
      "opacity": 100, "angle": 0
    },
    {
      "id": "external-label",
      "type": "text",
      "x": 630, "y": 192,
      "text": "[External System]",
      "fontSize": 14, "fontFamily": 1,
      "strokeColor": "#495057", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 130, "height": 20
    },
    {
      "id": "arrow-user-to-system",
      "type": "arrow",
      "x": 170, "y": 240, "width": 130, "height": 0,
      "strokeColor": "#495057", "backgroundColor": "transparent",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0,
      "opacity": 100, "angle": 0,
      "points": [[0, 0], [130, 0]],
      "startBinding": {"elementId": "user-actor", "gap": 5, "focus": 0},
      "endBinding": {"elementId": "system-box", "gap": 5, "focus": 0}
    },
    {
      "id": "arrow-system-to-external",
      "type": "arrow",
      "x": 500, "y": 200, "width": 100, "height": 0,
      "strokeColor": "#495057", "backgroundColor": "transparent",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0,
      "opacity": 100, "angle": 0,
      "points": [[0, 0], [100, 0]],
      "startBinding": {"elementId": "system-box", "gap": 5, "focus": 0},
      "endBinding": {"elementId": "external-system", "gap": 5, "focus": 0}
    }
  ],
  "appState": {"viewBackgroundColor": "#ffffff"}
}
\`\`\`

### 1.3 Non-Functional Requirements

| Requirement | Target | Measurement Method |
|---|---|---|
| Request latency (p99) | [e.g., < 100ms] | Prometheus histogram |
| Availability | [e.g., 99.9%] | Uptime monitoring |
| Throughput | [e.g., 1000 RPS] | Load test |
| Data durability | [e.g., zero loss on crash] | Integration test |
| Recovery time objective | [e.g., < 5 minutes] | Runbook drill |
| Recovery point objective | [e.g., < 1 hour] | Backup schedule |

---

## 2. Components (C4 Level 2)

### 2.1 Container Diagram

A C4 Level 2 diagram shows the internal containers (services, databases, message queues) and how they communicate. It answers: how is the system decomposed? how do the pieces talk?

[EXCALIDRAW DIAGRAM — Container Diagram]

\`\`\`excalidraw
{
  "type": "excalidraw",
  "version": 2,
  "source": "excalidraw",
  "elements": [
    {
      "id": "api-service",
      "type": "rectangle",
      "x": 200, "y": 100, "width": 180, "height": 80,
      "strokeColor": "#2f9e44", "backgroundColor": "#d3f9d8",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0,
      "opacity": 100, "angle": 0
    },
    {
      "id": "api-label",
      "type": "text",
      "x": 230, "y": 125,
      "text": "API Service\n[Go / HTTP]",
      "fontSize": 14, "fontFamily": 1,
      "strokeColor": "#2f9e44", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 130, "height": 40
    },
    {
      "id": "database",
      "type": "rectangle",
      "x": 200, "y": 280, "width": 180, "height": 80,
      "strokeColor": "#e03131", "backgroundColor": "#ffe3e3",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0,
      "opacity": 100, "angle": 0
    },
    {
      "id": "db-label",
      "type": "text",
      "x": 240, "y": 305,
      "text": "PostgreSQL\n[Database]",
      "fontSize": 14, "fontFamily": 1,
      "strokeColor": "#e03131", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 120, "height": 40
    },
    {
      "id": "arrow-api-to-db",
      "type": "arrow",
      "x": 290, "y": 180, "width": 0, "height": 100,
      "strokeColor": "#495057", "backgroundColor": "transparent",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0,
      "opacity": 100, "angle": 0,
      "points": [[0, 0], [0, 100]],
      "startBinding": {"elementId": "api-service", "gap": 5, "focus": 0},
      "endBinding": {"elementId": "database", "gap": 5, "focus": 0}
    }
  ],
  "appState": {"viewBackgroundColor": "#ffffff"}
}
\`\`\`

### 2.2 Component Descriptions

For each container/service in the diagram:

| Component | Type | Technology | Responsibility | Owned By |
|---|---|---|---|---|
| [Name] | [API / Worker / DB / Queue / Cache] | [Go / Python / PostgreSQL / Redis] | [What it does] | [Team / Agent] |

### 2.3 Component Interfaces

For each service-to-service interaction:

| From | To | Protocol | Auth | Retry Policy | Notes |
|---|---|---|---|---|---|
| [Service A] | [Service B] | [HTTP/gRPC/async] | [HMAC/JWT/mTLS/none] | [exponential backoff / none] | |

---

## 3. Data Flow

### 3.1 Primary Request Flow

[EXCALIDRAW DIAGRAM — Sequence Diagram for primary flow]

\`\`\`excalidraw
{
  "type": "excalidraw",
  "version": 2,
  "source": "excalidraw",
  "elements": [
    {
      "id": "actor-box",
      "type": "rectangle",
      "x": 50, "y": 50, "width": 100, "height": 40,
      "strokeColor": "#868e96", "backgroundColor": "#f1f3f5",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0,
      "opacity": 100, "angle": 0
    },
    {
      "id": "actor-text",
      "type": "text",
      "x": 75, "y": 62,
      "text": "Client",
      "fontSize": 14, "fontFamily": 1,
      "strokeColor": "#495057", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 60, "height": 20
    },
    {
      "id": "service-box",
      "type": "rectangle",
      "x": 300, "y": 50, "width": 120, "height": 40,
      "strokeColor": "#2f9e44", "backgroundColor": "#d3f9d8",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0,
      "opacity": 100, "angle": 0
    },
    {
      "id": "service-text",
      "type": "text",
      "x": 325, "y": 62,
      "text": "API Service",
      "fontSize": 14, "fontFamily": 1,
      "strokeColor": "#2f9e44", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 90, "height": 20
    },
    {
      "id": "lifeline-actor",
      "type": "line",
      "x": 100, "y": 90, "width": 0, "height": 300,
      "strokeColor": "#868e96", "backgroundColor": "transparent",
      "fillStyle": "solid", "strokeWidth": 1, "strokeStyle": "dashed",
      "roughness": 0, "opacity": 100, "angle": 0,
      "points": [[0, 0], [0, 300]]
    },
    {
      "id": "lifeline-service",
      "type": "line",
      "x": 360, "y": 90, "width": 0, "height": 300,
      "strokeColor": "#868e96", "backgroundColor": "transparent",
      "fillStyle": "solid", "strokeWidth": 1, "strokeStyle": "dashed",
      "roughness": 0, "opacity": 100, "angle": 0,
      "points": [[0, 0], [0, 300]]
    },
    {
      "id": "msg-1",
      "type": "arrow",
      "x": 100, "y": 140, "width": 260, "height": 0,
      "strokeColor": "#1971c2", "backgroundColor": "transparent",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0,
      "opacity": 100, "angle": 0,
      "points": [[0, 0], [260, 0]]
    },
    {
      "id": "msg-1-label",
      "type": "text",
      "x": 160, "y": 120,
      "text": "1. POST /endpoint",
      "fontSize": 12, "fontFamily": 1,
      "strokeColor": "#1971c2", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 150, "height": 20
    },
    {
      "id": "msg-2",
      "type": "arrow",
      "x": 360, "y": 200, "width": -260, "height": 0,
      "strokeColor": "#2f9e44", "backgroundColor": "transparent",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0,
      "opacity": 100, "angle": 0,
      "points": [[0, 0], [-260, 0]]
    },
    {
      "id": "msg-2-label",
      "type": "text",
      "x": 160, "y": 180,
      "text": "2. 201 Created",
      "fontSize": 12, "fontFamily": 1,
      "strokeColor": "#2f9e44", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 120, "height": 20
    }
  ],
  "appState": {"viewBackgroundColor": "#ffffff"}
}
\`\`\`

Describe the flow in numbered steps that match the diagram:

1. Client sends `POST /endpoint` with [auth mechanism]
2. Service validates [what]
3. Service writes to [where]
4. Service returns [what]

### 3.2 Error Flows

Document every non-happy-path flow that has different system behavior:

| Scenario | Trigger | System Response | User-Visible Effect |
|---|---|---|---|
| Auth failure | Invalid HMAC signature | Return 401 | Client must re-authenticate |
| Database unavailable | Postgres connection failure | Return 503, retain in-memory | Client retries |
| Input too large | Body > 1 MiB | Return 400 | Client reduces payload size |

### 3.3 Data Model (ERD)

[EXCALIDRAW DIAGRAM — Entity Relationship Diagram]

\`\`\`excalidraw
{
  "type": "excalidraw",
  "version": 2,
  "source": "excalidraw",
  "elements": [
    {
      "id": "entity-a",
      "type": "rectangle",
      "x": 100, "y": 100, "width": 200, "height": 160,
      "strokeColor": "#e03131", "backgroundColor": "#ffe3e3",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0,
      "opacity": 100, "angle": 0
    },
    {
      "id": "entity-a-header",
      "type": "text",
      "x": 140, "y": 110,
      "text": "entity_a",
      "fontSize": 16, "fontFamily": 1,
      "strokeColor": "#e03131", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 120, "height": 24
    },
    {
      "id": "entity-a-fields",
      "type": "text",
      "x": 110, "y": 145,
      "text": "id: uuid PK\ncreated_at: timestamptz\nfield_1: text NOT NULL\nfield_2: integer",
      "fontSize": 12, "fontFamily": 1,
      "strokeColor": "#212529", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 180, "height": 80
    },
    {
      "id": "entity-b",
      "type": "rectangle",
      "x": 450, "y": 100, "width": 200, "height": 160,
      "strokeColor": "#e03131", "backgroundColor": "#ffe3e3",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0,
      "opacity": 100, "angle": 0
    },
    {
      "id": "entity-b-header",
      "type": "text",
      "x": 490, "y": 110,
      "text": "entity_b",
      "fontSize": 16, "fontFamily": 1,
      "strokeColor": "#e03131", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 120, "height": 24
    },
    {
      "id": "entity-b-fields",
      "type": "text",
      "x": 460, "y": 145,
      "text": "id: uuid PK\nentity_a_id: uuid FK\ncreated_at: timestamptz\nvalue: numeric",
      "fontSize": 12, "fontFamily": 1,
      "strokeColor": "#212529", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 180, "height": 80
    },
    {
      "id": "rel-arrow",
      "type": "arrow",
      "x": 300, "y": 180, "width": 150, "height": 0,
      "strokeColor": "#495057", "backgroundColor": "transparent",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0,
      "opacity": 100, "angle": 0,
      "points": [[0, 0], [150, 0]],
      "startBinding": {"elementId": "entity-a", "gap": 5, "focus": 0},
      "endBinding": {"elementId": "entity-b", "gap": 5, "focus": 0}
    },
    {
      "id": "rel-label",
      "type": "text",
      "x": 340, "y": 160,
      "text": "1 : N",
      "fontSize": 12, "fontFamily": 1,
      "strokeColor": "#495057", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 40, "height": 20
    }
  ],
  "appState": {"viewBackgroundColor": "#ffffff"}
}
\`\`\`

For each table, include:
- Table name
- All columns with type and constraints
- Indexes (including rationale for each)
- Foreign key relationships

---

## 4. Infrastructure

### 4.1 Deployment Architecture

[EXCALIDRAW DIAGRAM — Infrastructure/Deployment Diagram]

\`\`\`excalidraw
{
  "type": "excalidraw",
  "version": 2,
  "source": "excalidraw",
  "elements": [
    {
      "id": "cloud-boundary",
      "type": "rectangle",
      "x": 80, "y": 80, "width": 600, "height": 400,
      "strokeColor": "#1971c2", "backgroundColor": "#e7f5ff",
      "fillStyle": "solid", "strokeWidth": 2, "strokeStyle": "dashed",
      "roughness": 0, "opacity": 30, "angle": 0
    },
    {
      "id": "cloud-label",
      "type": "text",
      "x": 100, "y": 90,
      "text": "AWS / GCP / Azure",
      "fontSize": 16, "fontFamily": 1,
      "strokeColor": "#1971c2", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 180, "height": 24
    },
    {
      "id": "service-container",
      "type": "rectangle",
      "x": 200, "y": 160, "width": 160, "height": 80,
      "strokeColor": "#2f9e44", "backgroundColor": "#d3f9d8",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0,
      "opacity": 100, "angle": 0
    },
    {
      "id": "service-container-label",
      "type": "text",
      "x": 230, "y": 188,
      "text": "Service\n[ECS / Lambda / Pod]",
      "fontSize": 12, "fontFamily": 1,
      "strokeColor": "#2f9e44", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 140, "height": 36
    },
    {
      "id": "managed-db",
      "type": "rectangle",
      "x": 450, "y": 160, "width": 160, "height": 80,
      "strokeColor": "#e03131", "backgroundColor": "#ffe3e3",
      "fillStyle": "solid", "strokeWidth": 2, "roughness": 0,
      "opacity": 100, "angle": 0
    },
    {
      "id": "managed-db-label",
      "type": "text",
      "x": 475, "y": 188,
      "text": "RDS PostgreSQL\n[Managed DB]",
      "fontSize": 12, "fontFamily": 1,
      "strokeColor": "#e03131", "backgroundColor": "transparent",
      "fillStyle": "solid", "roughness": 0, "opacity": 100, "angle": 0,
      "width": 120, "height": 36
    }
  ],
  "appState": {"viewBackgroundColor": "#ffffff"}
}
\`\`\`

### 4.2 Environment Matrix

| Component | Local Dev | Staging | Production |
|---|---|---|---|
| [Service] | docker compose | [platform] | [platform] |
| [Database] | docker postgres | RDS / Cloud SQL | RDS / Cloud SQL |
| [Secrets] | .env file | [secret manager] | [secret manager] |
| [Monitoring] | None | Prometheus | Prometheus + Grafana |

### 4.3 Resource Requirements

| Component | CPU | Memory | Storage | Scaling |
|---|---|---|---|---|
| [Service] | [e.g., 0.5 vCPU] | [e.g., 512 MB] | Stateless | [Horizontal / Vertical / Lambda] |
| [Database] | [e.g., 2 vCPU] | [e.g., 4 GB] | [e.g., 100 GB SSD] | Vertical |

### 4.4 Infrastructure as Code

All infrastructure must be defined in code. Specify:
- IaC tool: [Terraform / Pulumi / CDK / CloudFormation]
- Repository location: `infra/` directory in project root
- State backend: [S3 + DynamoDB / Terraform Cloud / etc.]

---

## 5. Security

### 5.1 Threat Model Summary

| Threat | Vector | Mitigation | Residual Risk |
|---|---|---|---|
| Unauthorized API access | Missing/invalid auth | HMAC-SHA256 per-client key | Low |
| Replay attacks | Valid but replayed request | Timestamp skew window (MaxSkewSeconds) | Low |
| Input injection | Malicious payload | Input validation + parameterized queries | Low |
| Secrets exposure | Hardcoded credentials | Environment variables + secret manager | Low |
| DoS via large payloads | 1 GB body | MaxBytesReader (1 MiB) | Low |

### 5.2 Authentication & Authorisation

- **Authentication mechanism:** [HMAC-SHA256 / JWT / mTLS / API Key / OAuth2]
- **Authorisation model:** [RBAC / ABAC / none]
- **Token/key rotation:** [Manual / Automated; frequency]
- **Session management:** [Stateless / Redis sessions; TTL]

### 5.3 Data Classification

| Data Type | Classification | Encryption at Rest | Encryption in Transit | Retention |
|---|---|---|---|---|
| [e.g., Latency metrics] | [Public / Internal / Confidential / Restricted] | [Yes / No] | TLS 1.3 | [e.g., 90 days] |
| [e.g., API keys] | Confidential | Yes (secret manager) | TLS 1.3 | Until rotated |

### 5.4 Security Controls Checklist

- [ ] All inputs validated server-side (no trust of client-side validation)
- [ ] All SQL uses parameterised queries (zero string concatenation)
- [ ] Secrets in environment variables or secret manager (never in code)
- [ ] HTTPS/TLS enforced for all external communication
- [ ] Dependency audit scheduled (weekly for production services)
- [ ] Rate limiting configured for all public endpoints
- [ ] Security headers set (Content-Security-Policy, X-Frame-Options, etc.)
- [ ] Error messages do not leak internal state to clients

---

## 6. API Contracts

### 6.1 API Specification

Link to the authoritative API specification:
- **Format:** [OpenAPI 3.0 / GraphQL Schema / Protobuf]
- **Location:** `api/openapi.yaml` or `api/schema.graphql` or `api/proto/`
- **Versioning strategy:** [URI versioning `/v1/` / Header versioning / None]

### 6.2 Endpoint Summary

| Method | Path | Auth | Request Body | Response | Notes |
|---|---|---|---|---|---|
| POST | /v1/[resource] | [HMAC / JWT / None] | [Schema ref] | 201 / 400 / 401 | |
| GET | /v1/[resource]/:id | [JWT] | None | 200 / 404 | |
| GET | /healthz | None | None | 200 / 503 | Health check |

### 6.3 Breaking Change Policy

- **Major version bump** required for: removing endpoints, removing required fields, changing field types
- **Minor version bump** for: adding optional fields, adding new endpoints
- **Deprecation notice period:** [e.g., 90 days before removing a v1 endpoint]
- **Client migration support:** [e.g., v1 and v2 run concurrently for 90 days]

---

## 7. Decision Log (ADRs)

All architectural decisions must be recorded here using the standard ADR format. The Harness Architect writes the initial ADRs; agents propose new ADRs when encountering decisions not covered by existing ones.

### ADR Format

```
## ADR-[NNN]: [Short Decision Title]

**Date:** [date]  
**Status:** [Proposed / Accepted / Deprecated / Superseded by ADR-NNN]  
**Decider:** [Agent role or human]

### Context
What situation or problem prompted this decision?

### Decision
What was decided?

### Alternatives Considered
| Option | Pros | Cons | Why Rejected |
|---|---|---|---|

### Consequences
- Positive: [what this enables]
- Negative: [what this constrains]
- Risks: [what could go wrong]

### Reversibility
[Easy / Hard / Effectively irreversible] — [how to reverse it if needed]
```

---

## Compliance Checklist

The Harness Evaluator verifies the following before approving a `SYSTEM_ARCHITECTURE.md`:

- [ ] Section 1 Overview is present and complete
- [ ] C4 Level 1 diagram is present with valid Excalidraw JSON
- [ ] C4 Level 2 diagram is present with valid Excalidraw JSON
- [ ] Section 3 Data Flow documents the primary flow with a sequence diagram
- [ ] Error flows table is populated (not empty)
- [ ] ERD is present for all stateful components
- [ ] Section 4 Infrastructure documents all environments
- [ ] Section 5 Security threat model is populated
- [ ] Security controls checklist has been evaluated (not just pasted in)
- [ ] Section 6 API Contracts lists all public endpoints
- [ ] Section 7 has at least one ADR covering the most significant architectural decision
- [ ] All Excalidraw JSON is valid (parseable) before embedding
```

---

## Enforcement

The Harness Evaluator includes a check in every `HARNESS_QUALITY_REPORT.md`:

```
ARCHITECTURE_DOCUMENT_CHECK:
  - SYSTEM_ARCHITECTURE.md exists: [YES / NO — BLOCKER]
  - All 7 mandatory sections present: [YES / NO — BLOCKER]  
  - Excalidraw diagrams present and valid: [YES / NO — WARNING]
  - ADR count: [N] — minimum 1 required
  - Compliance checklist items incomplete: [N] — 0 required for PASS
```

A project with no `SYSTEM_ARCHITECTURE.md` may not begin implementation. This is enforced by the Harness Orchestrator, not advisory.
