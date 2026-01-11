# qfactory

qfactory is an enterprise-grade Software Factory platform that orchestrates AI-assisted software development workflows with deterministic execution, verified quality gates, and full artifact provenance—designed to operate in both connected and air-gapped environments without requiring any outbound network access.

## Key Guarantees

- **Bounded Execution**: All workflows have explicit time and resource limits with stop-loss termination
- **Quality Gates**: Four-tier verification (PR0-PR3) with mandatory evidence bundles
- **Artifact Provenance**: Complete chain-of-custody from intent to deployment
- **Air-Gapped Capable**: Full functionality with zero outbound network dependencies
- **Not a Chatbot**: Artifact-only handoffs between execution stages; no conversational agent loops

## Repository Structure

```
qfactory/
├── apps/
│   ├── control-plane-api/    # REST/gRPC API for workflow management
│   ├── orchestrator-worker/  # Temporal workers for workflow orchestration
│   ├── runner/               # Sandboxed execution environment
│   └── ui/                   # Next.js control plane dashboard
├── services/                 # Shared service implementations
├── mcp/                      # Model Context Protocol tool definitions
├── pkg/
│   ├── contracts/            # Shared type definitions and interfaces
│   ├── events/               # Event schemas and publishers
│   ├── logging/              # Structured logging utilities
│   └── errors/               # Error types and handling
├── docs/
│   ├── diagrams/             # Mermaid architecture diagrams
│   └── 12-adr/               # Architecture Decision Records
├── infra/
│   └── docker-compose/       # Local development infrastructure
└── scripts/                  # Development and CI scripts
```

## Local Development Quickstart

Prerequisites: Docker, Docker Compose, Go 1.22+, Node.js 18+

```bash
# 1. Start infrastructure (Postgres, Redis, Qdrant, Temporal)
./scripts/dev-up.sh

# 2. Verify all services are healthy
./scripts/check.sh

# 3. In terminal 1: Start the control-plane API
./scripts/run-api.sh

# 4. In terminal 2: Start the orchestrator worker
./scripts/run-worker.sh

# 5. In terminal 3: Start the UI dashboard
./scripts/run-ui.sh

# 6. Create a workflow run
curl -X POST http://localhost:8090/workflows \
  -H "Content-Type: application/json" \
  -d '{"type":"demo","requirement":"test run"}'

# 7. Watch events via SSE (replace RUN_ID with id from step 6)
curl -N http://localhost:8090/runs/RUN_ID/events

# 8. Check run status
curl http://localhost:8090/runs/RUN_ID

# Stop infrastructure when done
./scripts/dev-down.sh
```

### Service URLs

| Service | URL |
|---------|-----|
| Control Plane API | http://localhost:8090 |
| qfactory Dashboard | http://localhost:3000 |
| Temporal UI | http://localhost:8088 |
| Qdrant Dashboard | http://localhost:6333/dashboard |

### API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | /workflows | Create and start a new workflow run |
| GET | /runs | List all workflow runs (newest first) |
| GET | /runs/{id} | Get workflow run status |
| GET | /runs/{id}/events | SSE stream of run events |
| POST | /runs/{id}/gates/pr1 | Execute PR1 (unit test) gate |
| GET | /runs/{id}/evidence | Get evidence manifest |
| GET | /runs/{id}/evidence.zip | Download evidence bundle |

### PR1 Gate (v0.2)

The PR1 gate runs `go test ./...` with a configurable timeout (default 60s, set via `QF_PR1_TIMEOUT_SECONDS`). Evidence is written to `evidence/<run_id>/` including stdout, stderr, and timing.

### Dashboard UI (v0.2)

The Next.js dashboard provides a visual interface for monitoring workflow runs:

- **Runs List** (`/runs`): View all runs with status, timestamps, and PR1 gate results
- **Run Detail** (`/runs/[id]`): Pipeline stages, gate execution, live SSE events

Environment variables:
- `NEXT_PUBLIC_API_BASE_URL`: API endpoint (default: `http://localhost:8090`)

## PR Levels (Quality Gates)

- **PR0 (Lint/Format)**: Code compiles, formatting passes, no lint errors
- **PR1 (Unit)**: All unit tests pass, coverage thresholds met, contracts validated
- **PR2 (Integration)**: Service integration tests pass, API contract verification, security scan clean
- **PR3 (System)**: End-to-end smoke tests pass, performance baselines met, evidence bundle complete

## Documentation

See [docs/](docs/) for complete documentation including architecture, workflows, security, and ADRs.

## License

Proprietary - All Rights Reserved
