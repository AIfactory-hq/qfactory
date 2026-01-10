# ADR-0004: MCP as Tool Plane Interface

## Status

Accepted

## Context

qfactory needs a standardized interface for operations performed during workflows:
- File operations (read, write, diff, search)
- Build operations (compile, test, lint, format)
- Code analysis (AST parsing, dependency analysis, impact analysis)
- Security scanning (SAST, dependency audit, secret detection)
- (Optionally) AI-assisted operations that invoke model runtime

We need this interface to be:
- Consistent across all operations
- Auditable with structured inputs/outputs
- Composable in workflows
- Extensible for custom tools

## Decision

We will use the **Model Context Protocol (MCP)** as the **tool plane interface** for actions and inspections.

### Key Clarification

**MCP is NOT the model interface.** MCP is the tool plane that provides:
- File tools: read, write, diff, glob, grep
- Build tools: compile, test, lint, format
- Analysis tools: AST queries, dependency graphs, impact analysis
- Security tools: SAST, dependency audit, secret detection
- Search tools: semantic search (uses embeddings)

**Model runtime is a separate abstraction** (see ADR-0006). Some MCP tools MAY invoke the model runtime:
- `generate_code` tool wraps model runtime for code generation
- `generate_tests` tool wraps model runtime for test generation

But the majority of MCP tools are **deterministic** and do not involve AI models.

### Architecture

```
Workflow Activity
      ↓
MCP Tool Dispatcher
      ↓
┌─────────────────────────────────────────────┐
│  Deterministic Tools    │  AI-Assisted Tools │
│  - file_read            │  - generate_code   │
│  - file_write           │  - generate_tests  │
│  - build_compile        │                    │
│  - security_sast        │        ↓           │
│  - analyze_ast          │  Model Runtime     │
└─────────────────────────────────────────────┘
```

### Tool Contract

Every tool has:
- Unique name and category
- JSON Schema for input
- JSON Schema for output
- Timeout configuration
- Deterministic flag (true for non-AI tools)

### Benefits

1. **Schema Validation**: Inputs and outputs validated against schema
2. **Auditable**: All tool calls logged with full input/output
3. **Composable**: Tools can be combined in workflows
4. **Extensible**: Custom tools registered with dispatcher
5. **Testable**: Deterministic tools are easy to test

## Consequences

### Positive
- Clean abstraction for all workflow operations
- Consistent auditing across all tool calls
- Easy to add new tools
- Tools are independently testable

### Negative
- Additional layer of abstraction
- Must maintain tool schemas
- MCP specification may evolve

## Alternatives Considered

### Direct Function Calls
- Pro: Simple, no abstraction
- Con: No schema validation, inconsistent auditing

### Separate Interface per Tool Category
- Pro: Category-specific optimizations
- Con: Inconsistent patterns, multiple abstractions to learn
