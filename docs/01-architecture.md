# qfactory Architecture

## Overview

qfactory implements a strict separation between the Control Plane (coordination and data ownership) and Execution Plane (work execution), connected through Temporal workflows and well-defined APIs.

## Design Principles

1. **Bounded Execution**: Every operation has explicit time and resource limits
2. **Artifact-Only Handoffs**: No conversational state between stages
3. **Diff Discipline**: Changes are expressed as diffs, never full rewrites
4. **Evidence-First**: All actions produce auditable evidence
5. **Air-Gap Ready**: No architectural dependency on outbound connectivity
6. **Control Plane Owns Data**: Execution plane writes through control plane APIs

## Control Plane vs Execution Plane

### Control Plane
- Accepts and validates requests via API
- Manages workflow state in Temporal
- Enforces quality gates and policies
- **Owns all persistent data** (artifacts, evidence, metadata)
- Provides observability and audit
- Exposes Artifact API and Evidence API for execution plane

Components:
- `control-plane-api`: REST/gRPC API service
- `ui`: Next.js dashboard for workflow management
- PostgreSQL: Durable metadata, artifacts, and configuration
- Temporal: Workflow orchestration and persistence

### Execution Plane
- Executes discrete, bounded tasks
- Runs in sandboxed environments
- Produces artifacts and evidence
- Reports progress metrics
- **Writes data only through control plane APIs**

Components:
- `orchestrator-worker`: Temporal workers for workflow activities
- `runner`: Sandboxed execution environment for code operations
- MCP tools: Standardized interface for actions and inspections

### Data Flow Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     CONTROL PLANE                           │
│  ┌──────────────┐   ┌──────────────┐   ┌──────────────┐    │
│  │ Workflow API │   │ Artifact API │   │ Evidence API │    │
│  └──────┬───────┘   └──────┬───────┘   └──────┬───────┘    │
│         │                  │                  │             │
│         └────────────┬─────┴──────────────────┘             │
│                      ▼                                      │
│              ┌──────────────┐                               │
│              │  PostgreSQL  │                               │
│              └──────────────┘                               │
└─────────────────────────────────────────────────────────────┘
                       ▲
                       │ API calls (artifacts, evidence, logs)
                       │
┌─────────────────────────────────────────────────────────────┐
│                    EXECUTION PLANE                          │
│  ┌──────────────┐   ┌──────────────┐   ┌──────────────┐    │
│  │   Worker     │   │   Runner     │   │  MCP Tools   │    │
│  └──────────────┘   └──────────────┘   └──────────────┘    │
└─────────────────────────────────────────────────────────────┘
```

This architecture ensures:
- Clear security boundary between planes
- All data mutations go through audited APIs
- Execution plane can be network-isolated except for control plane access
- Simplified RBAC (execution plane has limited API scopes)

## Why Temporal

Temporal provides:
1. **Durable Execution**: Workflows survive process restarts
2. **Visibility**: Full history of workflow execution
3. **Retry Policies**: Configurable retry with exponential backoff
4. **Timeouts**: Activity, workflow, and schedule-to-close timeouts
5. **Versioning**: Safe workflow evolution without breaking running instances

## Why MCP (Tool Plane)

MCP provides a standardized tool plane for actions and inspections:
1. **File Operations**: Read, write, diff, glob, grep
2. **Build Commands**: Compile, test, lint, format
3. **Code Analysis**: AST parsing, semantic search, impact analysis
4. **Security Scans**: SAST, dependency audit, secret detection

MCP is **not** the model interface. The model runtime is a separate abstraction. Some MCP tools may invoke the model runtime (e.g., for code generation), but MCP primarily handles deterministic operations.

## Why Diff Discipline

Full file rewrites are:
- Error-prone (lose unrelated changes)
- Hard to review (no clear change scope)
- Conflict-prone (merge issues)

Diffs are:
- Precise (only intended changes)
- Reviewable (clear scope)
- Mergeable (standard tooling)

## Data Stores

### Temporal Persistence
- Workflow state and history
- Activity results and retries
- Timer and schedule state

### PostgreSQL (Control Plane Owned)
- Durable metadata (projects, configurations)
- Quality gate definitions
- Artifacts and evidence bundles
- Audit logs

### Redis
- Ephemeral state (locks, rate limits)
- Session data
- Real-time metrics aggregation

### Qdrant
- Semantic search over codebase
- Similar code detection
- Documentation embeddings

## Stop-Loss and Termination

Every workflow has:
1. **Time Budget**: Maximum wall-clock time
2. **Token Budget**: Maximum LLM tokens consumed
3. **Iteration Limit**: Maximum retry/loop iterations
4. **Progress Threshold**: Minimum progress per interval

If any budget is exhausted or progress stalls, the workflow terminates with a detailed report of work completed.

## Artifact-Only Handoffs

Between workflow stages:
- Input: Structured artifacts (specs, code, configs)
- Output: Structured artifacts (code, tests, evidence)
- No: Conversation history, chat context, agent memory

This ensures:
- Reproducibility (same inputs = same outputs)
- Auditability (clear input/output boundaries)
- Parallelizability (no shared mutable state)
