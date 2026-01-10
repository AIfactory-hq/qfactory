# ADR-0003: Control Plane and Execution Plane Separation

## Status

Accepted

## Context

We need to define how qfactory components are organized architecturally. Key concerns:
- Security: Code execution must be isolated
- Scalability: Different components scale differently
- Reliability: Failures should be contained
- Auditability: Clear data ownership and write paths

## Decision

We will separate qfactory into two distinct planes:

### Control Plane
- Accepts and validates requests
- Manages workflow state in Temporal
- Enforces policies and gates
- **Owns all persistent data** (artifacts, evidence, metadata)
- Collects and stores evidence
- Provides observability and audit

Components: `control-plane-api`, `ui`, PostgreSQL

### Execution Plane
- Executes discrete tasks
- Runs in sandboxed environments
- Produces artifacts
- Reports progress
- **Writes data only through control plane APIs**

Components: `orchestrator-worker`, `runner`, MCP tools

### Data Ownership

The control plane owns all persistent data. The execution plane writes:
- Artifacts via `POST /api/v1/artifacts`
- Evidence via `POST /api/v1/evidence`
- Logs via structured logging (aggregated by control plane)

This ensures:
- Clear security boundary
- All mutations are audited
- Execution plane can be network-isolated (except control plane access)
- Simplified RBAC (execution plane has limited API scopes)

### Temporal as Bridge
Temporal connects the planes:
- Control plane submits workflows
- Execution plane workers consume activities
- State durably persisted in Temporal
- Both planes can fail independently

## Consequences

### Positive
- **Security**: Execution isolated from control, data access via APIs
- **Scalability**: Scale workers independently
- **Reliability**: Control plane survives worker crashes
- **Auditability**: Clear boundary for logging/monitoring, all writes audited

### Negative
- **Complexity**: More components to operate
- **Latency**: Cross-plane communication overhead
- **Debugging**: Traces span multiple systems

## Alternatives Considered

### Monolithic Architecture
- Pro: Simpler deployment
- Con: Execution not isolated, scaling limitations

### Direct Database Access from Execution Plane
- Pro: Lower latency
- Con: Security risk, audit complexity, RBAC complexity
