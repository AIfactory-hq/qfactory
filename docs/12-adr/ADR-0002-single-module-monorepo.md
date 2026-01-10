# ADR-0002: Single-Module Go Monorepo

## Status

Accepted

## Context

We need to decide on repository structure for qfactory. Options include:
- Multi-repo (one repo per service)
- Monorepo with multiple Go modules
- Monorepo with single Go module

## Decision

We will use a **single Go module monorepo** with the structure:

```
qfactory/
├── go.mod                 # Single module: github.com/satish-gonella/qfactory
├── apps/
│   ├── control-plane-api/
│   ├── orchestrator-worker/
│   ├── runner/
│   └── ui/
├── pkg/                   # Shared packages
├── services/              # Internal services
└── mcp/                   # MCP tool definitions
```

All Go code shares one `go.mod` file. The UI (Next.js) has its own `package.json`.

## Consequences

### Positive
- **Atomic changes**: Cross-cutting changes in single commit
- **Simple dependency management**: One go.mod to maintain
- **Easy refactoring**: Move code between packages freely
- **Consistent tooling**: Same linting, testing across all code
- **Single version**: All components versioned together

### Negative
- **Longer CI times**: Must be mitigated with caching and selective builds
- **Larger clone**: All code downloaded even if only working on one service
- **Release coupling**: All services release together (can be positive)

## Alternatives Considered

### Multi-repo
- Pro: Independent deployment, smaller repos
- Con: Dependency management nightmare, cross-repo changes painful

### Multi-module monorepo
- Pro: Independent versioning per service
- Con: Complex replace directives, confusing for contributors
