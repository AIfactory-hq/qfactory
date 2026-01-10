# qfactory Observability

## Observability Pillars

qfactory provides comprehensive observability through three pillars:

1. **Logs**: Structured, contextual logging
2. **Metrics**: Quantitative measurements
3. **Traces**: Request flow tracking

## Logging

### Structured Logging

All services emit structured JSON logs:

```json
{
  "timestamp": "2026-01-10T10:30:00.123Z",
  "level": "info",
  "service": "control-plane-api",
  "component": "workflow",
  "workflow_id": "wf-123",
  "project_id": "proj-456",
  "message": "Workflow started",
  "duration_ms": 45
}
```

### Log Levels

| Level | Usage |
|-------|-------|
| ERROR | Operation failed, requires attention |
| WARN | Unexpected but recoverable condition |
| INFO | Significant business events |
| DEBUG | Detailed operational information |

### Contextual Fields

Standard fields included in all logs:
- `service`: Service name
- `instance_id`: Instance identifier
- `trace_id`: Distributed trace ID
- `span_id`: Current span ID
- `workflow_id`: Workflow identifier (when applicable)
- `project_id`: Project identifier (when applicable)

### Log Aggregation

Logs are designed for aggregation with:
- ELK Stack (Elasticsearch, Logstash, Kibana)
- Loki + Grafana
- Splunk
- CloudWatch Logs

## Metrics

### Metric Types

**Counters**: Cumulative values that only increase
- `qfactory_workflows_total{type, status}`
- `qfactory_gate_executions_total{level, result}`
- `qfactory_tool_calls_total{tool, result}`

**Gauges**: Point-in-time values
- `qfactory_active_workflows`
- `qfactory_queue_depth{queue}`
- `qfactory_model_memory_bytes`

**Histograms**: Distribution of values
- `qfactory_workflow_duration_seconds{type}`
- `qfactory_gate_duration_seconds{level}`
- `qfactory_tool_latency_seconds{tool}`
- `qfactory_tokens_used{model, operation}`

### Key Metrics

#### Workflow Metrics
- Workflow start rate
- Workflow completion rate
- Workflow duration distribution
- Workflow failure rate by type

#### Quality Gate Metrics
- Gate pass rate by level
- Gate duration by level
- Gate failure reasons
- Evidence bundle size

#### Model Metrics
- Token usage by model
- Inference latency
- Token budget utilization
- Model error rate

#### Infrastructure Metrics
- CPU/memory utilization
- Database connection pool
- Queue depths
- Cache hit rates

### Prometheus Exposition

All services expose `/metrics` endpoint in Prometheus format:

```
# HELP qfactory_workflows_total Total number of workflows
# TYPE qfactory_workflows_total counter
qfactory_workflows_total{type="feature",status="completed"} 1234
qfactory_workflows_total{type="feature",status="failed"} 56

# HELP qfactory_workflow_duration_seconds Workflow duration
# TYPE qfactory_workflow_duration_seconds histogram
qfactory_workflow_duration_seconds_bucket{type="feature",le="60"} 100
qfactory_workflow_duration_seconds_bucket{type="feature",le="300"} 500
```

## Distributed Tracing

### Trace Context

All operations propagate trace context using W3C Trace Context standard:
- `traceparent`: Trace ID and span ID
- `tracestate`: Vendor-specific data

### Span Attributes

Spans include:
- Service and operation name
- Start time and duration
- Status (OK, ERROR)
- Custom attributes (workflow_id, project_id, etc.)
- Events (logs within span)

### Trace Flow Example

```
API Request (control-plane-api)
└── Workflow Start (temporal)
    ├── Activity: Analyze Requirement
    │   └── Tool Call: semantic_search (mcp)
    │       └── Vector Query (qdrant)
    ├── Activity: Generate Code
    │   └── Tool Call: generate_code (mcp)
    │       └── Model Inference (ollama)
    └── Activity: Run PR0 Gate
        ├── Compile
        ├── Format Check
        └── Lint
```

### Trace Backends

Compatible with:
- Jaeger
- Zipkin
- OpenTelemetry Collector
- Grafana Tempo

## Dashboards

### Operational Dashboard

Key panels:
- Active workflows
- Workflow throughput
- Gate pass rates
- Error rate
- Token usage trend

### Workflow Dashboard

Key panels:
- Workflow by status
- Duration percentiles
- Stage duration breakdown
- Failure analysis

### Infrastructure Dashboard

Key panels:
- Service health
- Resource utilization
- Database performance
- Queue metrics

## Alerting

### Critical Alerts
- Service down
- Error rate > threshold
- Workflow stuck
- Resource exhaustion

### Warning Alerts
- High latency
- Low gate pass rate
- Token budget warnings
- Queue backlog

### Alert Configuration

```yaml
alerts:
  - name: HighErrorRate
    expr: rate(qfactory_workflows_total{status="failed"}[5m]) > 0.1
    for: 5m
    severity: warning

  - name: WorkflowStuck
    expr: qfactory_workflow_duration_seconds > 3600
    for: 10m
    severity: critical
```

## Health Checks

### Endpoints

Each service exposes:
- `/health/live`: Liveness (is process running)
- `/health/ready`: Readiness (can accept traffic)

### Dependency Health

Readiness checks verify:
- Database connectivity
- Redis connectivity
- Temporal connectivity
- Model runtime availability

### Health Response

```json
{
  "status": "healthy",
  "checks": {
    "postgres": {"status": "up", "latency_ms": 2},
    "redis": {"status": "up", "latency_ms": 1},
    "temporal": {"status": "up", "latency_ms": 5},
    "model_runtime": {"status": "up", "latency_ms": 50}
  }
}
```
