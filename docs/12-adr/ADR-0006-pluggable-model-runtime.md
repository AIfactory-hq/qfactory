# ADR-0006: Pluggable Model Runtime

## Status

Accepted

## Context

qfactory uses AI models for certain operations (code generation, test generation, analysis). We need to support:
- Cloud APIs (Anthropic, OpenAI) for capability
- Local models (Ollama, vLLM) for air-gapped operation
- Future models as they become available

This is **separate from the MCP tool plane** (ADR-0004). MCP handles tool dispatch; model runtime handles LLM access.

## Decision

We implement a **pluggable model runtime** with a common interface:

```go
type ModelRuntime interface {
    Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
    Embed(ctx context.Context, texts []string) ([][]float32, error)
    Health() HealthStatus
}
```

### Relationship to MCP

```
MCP Tool (e.g., generate_code)
      ↓
Prepares prompt and context
      ↓
Calls Model Runtime
      ↓
Validates and formats output
      ↓
Returns structured result
```

AI-assisted MCP tools use model runtime internally, but:
- MCP handles input/output schema validation
- MCP handles audit logging
- Model runtime handles LLM communication and token tracking

### Implementations

1. **CloudRuntime**: Connects to cloud APIs (Anthropic, OpenAI)
2. **OllamaRuntime**: Connects to local Ollama
3. **VLLMRuntime**: Connects to vLLM server
4. **MockRuntime**: For testing

### Selection

Runtime selected by configuration:
```yaml
model_runtime:
  type: ollama  # or: cloud, vllm
  endpoint: http://localhost:11434
  default_model: codellama:13b
```

### Token Tracking

All implementations track token usage:
- Input tokens
- Output tokens
- Total per request
- Aggregated per workflow
- Budget enforcement

## Consequences

### Positive
- Deploy anywhere (cloud or air-gapped)
- Switch models without code changes
- Test with mock runtime
- Add new providers easily
- Clear separation from tool plane

### Negative
- Abstraction limits provider-specific features
- Must maintain multiple implementations
- Different models have different capabilities

## Alternatives Considered

### Cloud-Only
- Pro: Best models, simple implementation
- Con: Cannot operate air-gapped

### Model Runtime as MCP Tool
- Pro: Single abstraction
- Con: Conflates tool policy with model selection, complicates auditing
