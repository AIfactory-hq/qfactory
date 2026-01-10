# qfactory Roadmap

## Current Phase: Foundation (v0.1)

### Objectives
- Core infrastructure setup
- Basic workflow execution
- Development environment

### Deliverables
- [x] Repository structure and Go module
- [x] Docker Compose development environment
- [ ] Basic Temporal workflow spine
- [ ] Control plane API skeleton
- [ ] SSE event streaming

## Phase 2: Core Workflows (v0.2)

### Objectives
- Complete workflow implementations
- Quality gates PR0-PR2
- Evidence bundle generation
- Basic UI

### Deliverables
- [ ] Project scaffold workflow
- [ ] Feature implementation workflow
- [ ] PR0 gate implementation
- [ ] PR1 gate (unit testing)
- [ ] PR2 gate (integration)
- [ ] Evidence bundle packaging
- [ ] Next.js dashboard MVP
- [ ] Semantic search integration

## Phase 3: MCP Tool Plane (v0.3)

### Objectives
- MCP tool framework
- Deterministic tools (file, build, analysis, security)
- Tool auditing and tracing

### Deliverables
- [ ] MCP tool dispatcher framework
- [ ] File tools (read, write, diff, glob, grep)
- [ ] Build tools (compile, test, lint, format)
- [ ] Analysis tools (AST, deps, impact)
- [ ] Security tools (SAST, deps audit, secrets)
- [ ] Tool audit logging

## Phase 4: Model Integration (v0.4)

### Objectives
- Pluggable model runtime
- AI-assisted MCP tools
- Token budget management

### Deliverables
- [ ] Model runtime abstraction interface
- [ ] Ollama integration
- [ ] vLLM integration
- [ ] Cloud API integration (Anthropic, OpenAI)
- [ ] AI-assisted code generation tool
- [ ] AI-assisted test generation tool
- [ ] Token tracking and budgets

## Phase 5: Production Readiness (v0.5)

### Objectives
- Security hardening
- Observability complete
- Performance optimization
- Documentation complete

### Deliverables
- [ ] Sandboxed execution
- [ ] Full observability stack
- [ ] Performance benchmarks
- [ ] Security audit
- [ ] Complete documentation
- [ ] Deployment guides

## Phase 6: Air-Gap Capability (v0.6)

### Objectives
- Full air-gapped operation
- Offline licensing
- Deployment tooling
- Migration guides

### Deliverables
- [ ] Air-gapped configuration mode
- [ ] Offline license system
- [ ] Dependency bundling tools
- [ ] Air-gap deployment guide
- [ ] Update mechanism for air-gap

## Phase 7: Enterprise Features (v1.0)

### Objectives
- Multi-tenancy
- Advanced RBAC
- Audit compliance
- Enterprise integrations

### Deliverables
- [ ] Multi-tenant isolation
- [ ] Fine-grained RBAC
- [ ] Compliance reporting
- [ ] SSO integration
- [ ] Webhook integrations
- [ ] Custom tool extensions
- [ ] PR3 gate implementation

## Future Considerations

### Potential Features
- Visual workflow designer
- Custom model fine-tuning
- Multi-repo support
- IDE extensions
- CI/CD integrations

### Research Areas
- Improved code understanding
- Better test generation
- Automated refactoring
- Documentation generation
- Performance prediction

## Version Numbering

- **0.x**: Development releases
- **1.0**: First production release
- **1.x**: Feature additions, backward compatible
- **2.0**: Major changes, possible breaking changes
