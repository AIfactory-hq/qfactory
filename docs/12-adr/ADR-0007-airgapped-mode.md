# ADR-0007: Air-Gapped Operation Mode

## Status

Accepted

## Context

Enterprise and government customers require operation without any outbound network connectivity. This is not "works offline sometimes" but strict network isolation where outbound connections are blocked at the infrastructure level.

## Decision

qfactory supports a **strict air-gapped mode** where:

1. **Zero Outbound Connections**: No API calls, telemetry, license checks, or package downloads
2. **Local Everything**: All dependencies pre-staged locally
3. **Configuration Flag**: Explicit mode setting, not auto-detection

### Components for Air-Gap

| Component | Air-Gap Solution |
|-----------|------------------|
| Container images | Local registry (Harbor) |
| Model inference | Ollama or vLLM with local weights |
| Go dependencies | Vendored or local proxy |
| npm packages | Local Verdaccio mirror |
| Licensing | Signed file, no callback |

### Pre-Staging Process

1. In connected environment: pull all artifacts
2. Package into transfer bundle
3. Verify bundle integrity
4. Transfer to air-gapped environment
5. Load into local services
6. Validate end-to-end

### Configuration

```yaml
mode: airgapped
network:
  outbound_enabled: false
```

When `mode: airgapped`:
- All outbound checks disabled
- Local registry/mirrors required
- Local model runtime required
- File-based licensing required

## Consequences

### Positive
- Meets strictest security requirements
- No data exfiltration possible
- Full functionality in isolated networks
- Auditable, no hidden callbacks

### Negative
- More complex deployment
- Requires pre-staging infrastructure
- Updates require manual bundle transfer
- Local models may have lower capability

## Alternatives Considered

### Proxy-Based Isolation
- Pro: Easier updates
- Con: Still requires some connectivity, doesn't meet strict air-gap

### Cloud-Only Product
- Pro: Simpler architecture
- Con: Cannot serve air-gapped customers
