# qfactory Product Backlog

## Legend

**Priority**: P0 (Critical), P1 (High), P2 (Medium), P3 (Low)
**Size**: XS (hours), S (days), M (week), L (weeks), XL (month+)

---

## Infrastructure

| ID | Priority | Size | Item | Status |
|----|----------|------|------|--------|
| INF-001 | P0 | S | Docker Compose development environment | Done |
| INF-002 | P0 | S | Repository structure and Go module | Done |
| INF-003 | P1 | M | Kubernetes manifests for deployment | Backlog |
| INF-004 | P1 | S | CI/CD pipeline configuration | Backlog |
| INF-005 | P2 | M | Helm chart for deployment | Backlog |
| INF-006 | P2 | S | Terraform for cloud resources | Backlog |

## Control Plane API

| ID | Priority | Size | Item | Status |
|----|----------|------|------|--------|
| API-001 | P0 | M | Project CRUD endpoints | Backlog |
| API-002 | P0 | M | Workflow submission endpoint | Backlog |
| API-003 | P0 | S | Workflow status endpoint | Backlog |
| API-004 | P0 | S | SSE event streaming endpoint | Backlog |
| API-005 | P1 | S | Evidence bundle retrieval | Backlog |
| API-006 | P1 | M | Authentication middleware | Backlog |
| API-007 | P2 | M | API rate limiting | Backlog |
| API-008 | P2 | S | OpenAPI specification | Backlog |

## Workflows

| ID | Priority | Size | Item | Status |
|----|----------|------|------|--------|
| WF-001 | P0 | S | Demo workflow spine (5 stages) | Backlog |
| WF-002 | P1 | L | Project scaffold workflow | Backlog |
| WF-003 | P1 | L | Feature implementation workflow | Backlog |
| WF-004 | P1 | M | Integration test workflow | Backlog |
| WF-005 | P1 | M | Quality gate workflow | Backlog |
| WF-006 | P1 | M | Evidence bundle workflow | Backlog |
| WF-007 | P2 | M | Refactoring workflow | Backlog |
| WF-008 | P2 | M | Bug fix workflow | Backlog |

## Quality Gates

| ID | Priority | Size | Item | Status |
|----|----------|------|------|--------|
| QG-001 | P0 | S | PR0 gate implementation | Backlog |
| QG-002 | P0 | M | PR1 gate implementation | Backlog |
| QG-003 | P1 | M | PR2 gate implementation | Backlog |
| QG-004 | P3 | M | PR3 gate implementation | Backlog |
| QG-005 | P1 | S | Gate configuration system | Backlog |
| QG-006 | P2 | S | Manual override mechanism | Backlog |

## MCP Tools

| ID | Priority | Size | Item | Status |
|----|----------|------|------|--------|
| MCP-001 | P0 | M | Tool dispatcher framework | Backlog |
| MCP-002 | P1 | S | File tools (read, write, diff) | Backlog |
| MCP-003 | P1 | S | Build tools (compile, test, lint) | Backlog |
| MCP-004 | P1 | M | Analysis tools (AST, deps) | Backlog |
| MCP-005 | P1 | M | Security tools (SAST, audit) | Backlog |
| MCP-006 | P2 | M | Semantic search tool | Backlog |
| MCP-007 | P2 | L | AI-assisted code generation tool | Backlog |
| MCP-008 | P2 | M | AI-assisted test generation tool | Backlog |

## Model Runtime

| ID | Priority | Size | Item | Status |
|----|----------|------|------|--------|
| MR-001 | P1 | M | Runtime abstraction interface | Backlog |
| MR-002 | P1 | M | Ollama integration | Backlog |
| MR-003 | P2 | M | vLLM integration | Backlog |
| MR-004 | P2 | M | Cloud API integration | Backlog |
| MR-005 | P1 | S | Token tracking | Backlog |
| MR-006 | P2 | S | Model selection logic | Backlog |

## UI

| ID | Priority | Size | Item | Status |
|----|----------|------|------|--------|
| UI-001 | P1 | M | Dashboard layout | Backlog |
| UI-002 | P1 | M | Project management views | Backlog |
| UI-003 | P1 | M | Workflow monitoring (Trust UI) | Backlog |
| UI-004 | P2 | M | Evidence bundle viewer | Backlog |
| UI-005 | P2 | S | Metrics dashboard | Backlog |
| UI-006 | P3 | L | Visual workflow designer | Backlog |

## Observability

| ID | Priority | Size | Item | Status |
|----|----------|------|------|--------|
| OBS-001 | P1 | S | Structured logging | Backlog |
| OBS-002 | P1 | S | Prometheus metrics | Backlog |
| OBS-003 | P1 | M | Distributed tracing | Backlog |
| OBS-004 | P2 | M | Grafana dashboards | Backlog |
| OBS-005 | P2 | S | Health check endpoints | Backlog |
| OBS-006 | P2 | S | Alert rules | Backlog |

## Security

| ID | Priority | Size | Item | Status |
|----|----------|------|------|--------|
| SEC-001 | P1 | M | JWT authentication | Backlog |
| SEC-002 | P1 | M | RBAC implementation | Backlog |
| SEC-003 | P1 | M | Sandboxed execution | Backlog |
| SEC-004 | P2 | S | Secrets management integration | Backlog |
| SEC-005 | P2 | S | Audit logging | Backlog |
| SEC-006 | P2 | M | Security scanning integration | Backlog |

## Air-Gap

| ID | Priority | Size | Item | Status |
|----|----------|------|------|--------|
| AG-001 | P2 | M | Air-gap configuration mode | Backlog |
| AG-002 | P2 | M | Offline license system | Backlog |
| AG-003 | P2 | M | Dependency bundling | Backlog |
| AG-004 | P2 | S | Air-gap deployment guide | Backlog |
| AG-005 | P3 | M | Air-gap update mechanism | Backlog |

---

## Recently Completed

| ID | Item | Completed |
|----|------|-----------|
| INF-001 | Docker Compose development environment | 2026-01-10 |
| INF-002 | Repository structure and Go module | 2026-01-10 |
