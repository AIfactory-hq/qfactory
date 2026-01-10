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

Prerequisites: Docker, Docker Compose, Go 1.22+

```bash
# Start infrastructure (Postgres, Redis, Qdrant, Temporal)
./scripts/dev-up.sh

# Verify all services are healthy
./scripts/check.sh

# Stop infrastructure
./scripts/dev-down.sh
```

## PR Levels (Quality Gates)

- **PR0 (Lint/Format)**: Code compiles, formatting passes, no lint errors
- **PR1 (Unit)**: All unit tests pass, coverage thresholds met, contracts validated
- **PR2 (Integration)**: Service integration tests pass, API contract verification, security scan clean
- **PR3 (System)**: End-to-end smoke tests pass, performance baselines met, evidence bundle complete

## Documentation

See [docs/](docs/) for complete documentation including architecture, workflows, security, and ADRs.

## License

Proprietary - All Rights Reserved
