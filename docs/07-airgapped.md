# qfactory Air-Gapped Operation

## Definition

Air-gapped mode means qfactory operates with **zero outbound network connectivity**. No API calls, telemetry, license checks, or package downloads leave the deployment environment.

This is not "offline-capable" or "works without internet most of the time." It is strict network isolation where outbound connections are blocked at the network level.

## Requirements

### Before Air-Gap Deployment

The following must be prepared in a connected environment and transferred:

1. **Container Images**
   - All qfactory service images
   - Infrastructure images (Postgres, Redis, Qdrant, Temporal)
   - Model runtime images (Ollama, vLLM)

2. **Model Weights**
   - LLM weights for code generation (e.g., CodeLlama, DeepSeek-Coder)
   - Embedding model weights for semantic search
   - Any fine-tuned models

3. **Package Dependencies**
   - Go modules (vendored or proxy cache)
   - npm packages (for UI)
   - System packages

4. **Configuration**
   - License files (if applicable)
   - TLS certificates
   - Initial seed data

### Runtime Requirements

- Local container registry (Harbor, registry:2)
- Local package mirror or vendored dependencies
- Local model serving (Ollama or vLLM)
- Sufficient storage for models (50-200GB typical)
- GPU recommended for model inference (not required)

## Dependency Mirroring Strategy

### Container Images

1. **Connected Environment**:
```bash
# Pull all required images
docker pull postgres:16
docker pull redis:7
docker pull qdrant/qdrant:v1.7.4
docker pull temporalio/auto-setup:1.23
docker pull temporalio/ui:2.25
docker pull ollama/ollama:latest

# Save to tar archives
docker save postgres:16 | gzip > postgres-16.tar.gz
docker save redis:7 | gzip > redis-7.tar.gz
# ... etc
```

2. **Air-Gapped Environment**:
```bash
# Load into local registry
docker load < postgres-16.tar.gz
docker tag postgres:16 local-registry:5000/postgres:16
docker push local-registry:5000/postgres:16
# ... etc
```

### Go Modules

Option A: **Vendor Directory**
```bash
# In connected environment
go mod vendor

# Transfer vendor/ directory
# In air-gapped: builds use vendor automatically
```

Option B: **Module Proxy Cache**
```bash
# Run Athens or similar proxy in connected env
# Cache all dependencies
# Transfer cache to air-gapped proxy
```

### Model Weights

```bash
# Connected environment with Ollama
ollama pull codellama:13b
ollama pull nomic-embed-text

# Export models
# Location: ~/.ollama/models/
tar -czf ollama-models.tar.gz ~/.ollama/models/

# Air-gapped: extract to same location
tar -xzf ollama-models.tar.gz -C ~/
```

## Local Model Runtime Options

### Ollama (Recommended for Simplicity)

- Easy setup and model management
- CPU and GPU support
- Good performance with quantized models
- REST API compatible

Configuration:
```yaml
model_runtime:
  type: ollama
  endpoint: http://localhost:11434
  default_model: codellama:13b
  embedding_model: nomic-embed-text
```

### vLLM (Recommended for Throughput)

- High-throughput inference
- Efficient GPU utilization
- OpenAI-compatible API
- Better for concurrent requests

Configuration:
```yaml
model_runtime:
  type: vllm
  endpoint: http://localhost:8000
  default_model: /models/codellama-13b
  embedding_endpoint: http://localhost:8001
```

### Model Selection for Air-Gap

Recommended models (balance of capability and resource requirements):

| Task | Model | Size | VRAM Required |
|------|-------|------|---------------|
| Code Generation | CodeLlama-13B-Instruct | 13B | 16GB |
| Code Generation (low resource) | DeepSeek-Coder-6.7B | 6.7B | 8GB |
| Embeddings | nomic-embed-text | 137M | 1GB |
| Analysis | Mistral-7B-Instruct | 7B | 8GB |

CPU-only options (slower but no GPU required):
- CodeLlama-7B with Q4 quantization
- DeepSeek-Coder-1.3B

## Offline Licensing

qfactory supports offline licensing:

1. **License Generation**: License generated in connected environment with machine fingerprint
2. **License File**: Cryptographically signed license file transferred to air-gapped environment
3. **Validation**: Local validation using embedded public key
4. **No Callbacks**: Zero license server communication required

License file contains:
- Organization identifier
- Expiration date
- Feature flags
- Node limit
- Signature

## Configuration for Air-Gap Mode

```yaml
# config/airgap.yaml
mode: airgapped

network:
  outbound_enabled: false

registry:
  type: local
  endpoint: local-registry:5000

model_runtime:
  type: ollama
  endpoint: http://localhost:11434

package_mirror:
  go_proxy: http://local-athens:3000
  npm_registry: http://local-verdaccio:4873

telemetry:
  enabled: false

license:
  type: file
  path: /etc/qfactory/license.json
```

## Verification Checklist

Before going air-gapped:

- [ ] All container images loaded in local registry
- [ ] Model weights present and accessible
- [ ] Go modules vendored or proxy seeded
- [ ] npm packages cached or bundled
- [ ] License file in place
- [ ] TLS certificates installed
- [ ] DNS resolution works for internal services
- [ ] All services start without network errors
- [ ] End-to-end workflow completes successfully

## Updating in Air-Gap

To update qfactory in air-gapped environment:

1. Prepare update bundle in connected environment
2. Verify bundle integrity (checksums)
3. Transfer to air-gapped environment
4. Apply update during maintenance window
5. Verify all services restart correctly
6. Run validation workflow
