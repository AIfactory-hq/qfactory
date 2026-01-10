# qfactory MCP Tool Plane

## Overview

The Model Context Protocol (MCP) Tool Plane provides a standardized interface for **actions and inspections** within qfactory workflows. MCP tools handle deterministic operations like file manipulation, build commands, code analysis, and security scanning.

**Important Distinction:** MCP is the tool plane interface, not the model interface. The model runtime is a separate abstraction (see ADR-0006). Some MCP tools may invoke the model runtime for AI-assisted operations, but the majority of MCP tools perform deterministic, auditable actions.

## MCP Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Temporal Workflow                         │
├─────────────────────────────────────────────────────────────┤
│                    MCP Tool Dispatcher                       │
├──────────────┬──────────────┬──────────────┬────────────────┤
│  File        │  Build       │  Analysis    │  Security      │
│  Tools       │  Tools       │  Tools       │  Tools         │
├──────────────┴──────────────┴──────────────┴────────────────┤
│                 (Optional) Model Runtime                     │
│         For AI-assisted tools like code generation           │
└─────────────────────────────────────────────────────────────┘
```

## Tool Categories

### File Tools (Deterministic)

**`file_read`**
- Input: File path, optional line range
- Output: File content
- Auditable: Path, bytes read, timestamp

**`file_write`**
- Input: File path, content
- Output: Success/failure, checksum
- Auditable: Path, checksum, timestamp

**`file_diff`**
- Input: File path, unified diff
- Output: Patched content or error
- Auditable: Path, diff stats, timestamp

**`file_glob`**
- Input: Pattern, root directory
- Output: List of matching paths
- Auditable: Pattern, match count

**`file_grep`**
- Input: Pattern, file/directory, options
- Output: Matching lines with context
- Auditable: Pattern, match count

### Build Tools (Deterministic)

**`build_compile`**
- Input: Target, build flags
- Output: Success/failure, build log
- Auditable: Command, duration, exit code

**`build_test`**
- Input: Test target, flags
- Output: Test results (pass/fail per test)
- Auditable: Command, duration, results summary

**`build_lint`**
- Input: Target files, linter config
- Output: Lint findings
- Auditable: Linter, findings count, severities

**`build_format`**
- Input: Target files, formatter
- Output: Formatted content or diff
- Auditable: Formatter, files changed

### Analysis Tools (Deterministic)

**`analyze_ast`**
- Input: File path, query
- Output: AST nodes matching query
- Auditable: Language, query, match count

**`analyze_deps`**
- Input: Module/package
- Output: Dependency tree
- Auditable: Root module, depth, count

**`analyze_impact`**
- Input: Changed files
- Output: Affected files/modules
- Auditable: Input files, affected count

### Security Tools (Deterministic)

**`security_sast`**
- Input: Target directory, ruleset
- Output: Findings in SARIF format
- Auditable: Scanner, findings by severity

**`security_deps`**
- Input: Manifest file
- Output: Vulnerable dependencies
- Auditable: Scanner, CVE count

**`security_secrets`**
- Input: Target directory
- Output: Potential secret locations
- Auditable: Scanner, findings count

### Search Tools (May Use Embeddings)

**`semantic_search`**
- Input: Query text, scope, limit
- Output: Ranked file/snippet list with relevance scores
- Backend: Qdrant vector search
- Note: Uses embedding model for query vectorization

**`find_similar_code`**
- Input: Code snippet
- Output: Similar code locations
- Backend: Qdrant vector search

### AI-Assisted Tools (Use Model Runtime)

**`generate_code`**
- Input: Specification, target language, context files
- Output: Code as unified diff
- Validation: Syntax check, lint pass
- Note: Invokes model runtime, tracks token usage

**`generate_tests`**
- Input: Source file, coverage targets
- Output: Test file content
- Validation: Tests compile
- Note: Invokes model runtime

These tools are wrappers that:
1. Prepare context for the model
2. Invoke model runtime with token budget
3. Validate and format model output
4. Return structured result

## Tool Schema

Every MCP tool has:

```go
type ToolDefinition struct {
    Name        string              // Unique identifier
    Description string              // Human-readable purpose
    Category    ToolCategory        // file, build, analysis, security, ai
    InputSchema JSONSchema          // Validated input structure
    OutputSchema JSONSchema         // Validated output structure
    Timeout     time.Duration       // Maximum execution time
    Deterministic bool              // True for non-AI tools
}
```

## Audit Trail

Every tool call produces:

```go
type ToolCallRecord struct {
    ID            string
    ToolName      string
    Category      string
    Input         json.RawMessage
    Output        json.RawMessage
    Duration      time.Duration
    Timestamp     time.Time
    WorkflowID    string
    ActivityID    string
    // Only for AI-assisted tools:
    TokensUsed    int
    ModelID       string
}
```

All records are:
- Written to structured logs
- Stored via Evidence API for audit
- Indexed by workflow for tracing

## Error Handling

Tool errors are categorized:

| Category | Retry | Example |
|----------|-------|---------|
| Transient | Yes | Timeout, temp file issue |
| NotFound | No | File doesn't exist |
| Validation | No | Invalid input schema |
| Permission | No | Access denied |
| Budget | No | Token limit exceeded (AI tools) |

## Extension Points

Custom tools can be added by implementing:

1. Tool definition with schemas
2. Execution handler
3. Registration with dispatcher

Tools are loaded from:
- Built-in tool registry
- Plugin directory (for custom tools)
- Configuration file (for tool parameters)
