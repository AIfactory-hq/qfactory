# qfactory Data Model and Contracts

## Overview

qfactory uses strongly-typed contracts for all inter-component communication. Contracts are defined in `pkg/contracts` and shared across all services.

## Core Entities

### Project

Represents a software project managed by qfactory.

```go
type Project struct {
    ID              UUID
    Name            string
    Description     string
    Language        string          // go, typescript, python, etc.
    Framework       string          // gin, next, fastapi, etc.
    RepositoryURL   string
    DefaultBranch   string
    CreatedAt       time.Time
    UpdatedAt       time.Time
    Config          ProjectConfig
}

type ProjectConfig struct {
    QualityGates    []GateConfig
    TokenBudgets    TokenBudgetConfig
    Timeouts        TimeoutConfig
}
```

### Workflow Instance

Represents a running or completed workflow.

```go
type WorkflowInstance struct {
    ID              UUID
    TemporalID      string          // Temporal workflow ID
    ProjectID       UUID
    Type            WorkflowType    // scaffold, feature, integration, etc.
    Status          WorkflowStatus  // pending, running, completed, failed
    Input           json.RawMessage
    Output          json.RawMessage
    StartedAt       time.Time
    CompletedAt     time.Time
    TokensUsed      int
    GateResults     []GateResult
}
```

### Artifact

Represents a versioned output from a workflow.

```go
type Artifact struct {
    ID              UUID
    WorkflowID      UUID
    Type            ArtifactType    // code, test, config, evidence
    Path            string          // Relative path in repo
    ContentHash     string          // SHA256 of content
    CreatedAt       time.Time
    Metadata        map[string]string
}
```

### Evidence Bundle

Complete audit record for an artifact.

```go
type EvidenceBundle struct {
    ID              UUID
    ArtifactID      UUID
    Version         string
    CreatedAt       time.Time

    // Provenance
    SourceWorkflowID    UUID
    InputRequirement    string

    // Gate Results
    PR0Result       GateResult
    PR1Result       GateResult
    PR2Result       GateResult
    // PR3Result is future

    // Checksums
    Checksums       map[string]string

    // Signature
    SignatureAlg    string
    Signature       []byte
}
```

## Event Schemas

All events follow CloudEvents specification.

### WorkflowStarted

```json
{
    "specversion": "1.0",
    "type": "qfactory.workflow.started",
    "source": "/workflows/{workflowId}",
    "id": "{eventId}",
    "time": "2026-01-10T10:30:00Z",
    "data": {
        "workflowId": "...",
        "workflowType": "feature_implementation",
        "projectId": "...",
        "input": {}
    }
}
```

### StageStarted / StageCompleted

```json
{
    "specversion": "1.0",
    "type": "qfactory.stage.started",
    "source": "/workflows/{workflowId}/stages/{stageName}",
    "id": "{eventId}",
    "time": "2026-01-10T10:30:05Z",
    "data": {
        "workflowId": "...",
        "stageName": "analyze",
        "stageIndex": 0
    }
}
```

### GateCompleted

```json
{
    "specversion": "1.0",
    "type": "qfactory.gate.completed",
    "source": "/workflows/{workflowId}/gates/{gateLevel}",
    "id": "{eventId}",
    "time": "2026-01-10T10:35:00Z",
    "data": {
        "workflowId": "...",
        "gateLevel": "PR1",
        "passed": true,
        "results": {
            "testsRun": 42,
            "testsPassed": 42,
            "coverage": 85.5
        },
        "evidenceRef": "..."
    }
}
```

### WorkflowCompleted

```json
{
    "specversion": "1.0",
    "type": "qfactory.workflow.completed",
    "source": "/workflows/{workflowId}",
    "id": "{eventId}",
    "time": "2026-01-10T11:00:00Z",
    "data": {
        "workflowId": "...",
        "status": "completed",
        "artifacts": [],
        "tokensUsed": 15000,
        "evidenceBundleId": "..."
    }
}
```

## API Contracts

### REST API

**POST /api/v1/projects**
```json
// Request
{
    "name": "my-service",
    "language": "go",
    "framework": "gin",
    "description": "..."
}

// Response
{
    "id": "...",
    "name": "my-service",
    "createdAt": "..."
}
```

**POST /api/v1/workflows**
```json
// Request
{
    "projectId": "...",
    "type": "feature_implementation",
    "input": {
        "requirement": "Add user authentication",
        "targetFiles": ["internal/auth/..."]
    }
}

// Response
{
    "id": "...",
    "temporalId": "...",
    "status": "pending"
}
```

**GET /api/v1/workflows/{id}**
```json
// Response
{
    "id": "...",
    "temporalId": "...",
    "status": "running",
    "currentStage": "implement",
    "progress": {
        "stagesCompleted": 2,
        "totalStages": 5
    }
}
```

**GET /api/v1/runs/{id}/events** (SSE)
```
event: stage.started
data: {"stageName": "analyze", "timestamp": "..."}

event: stage.completed
data: {"stageName": "analyze", "timestamp": "...", "result": "success"}
```

**GET /api/v1/workflows/{id}/evidence**
```json
// Response
{
    "bundleId": "...",
    "artifacts": [],
    "gateResults": {},
    "provenance": {}
}
```

## Contract Validation

All contracts are validated at:
1. **API boundary**: Request/response validation
2. **Event emission**: Event schema validation
3. **Storage**: Database constraint enforcement
4. **Inter-service**: gRPC/REST client validation

Validation failures produce structured errors:
```json
{
    "code": "VALIDATION_ERROR",
    "message": "Invalid input",
    "details": [
        {"field": "language", "error": "unsupported value: rust"}
    ]
}
```
