# qfactory Temporal Workflows

## Overview

All qfactory operations are implemented as Temporal workflows, providing durability, visibility, and bounded execution guarantees.

## Workflow Types

### 1. ProjectScaffoldWorkflow

Generates initial project structure from specifications.

**Input:**
- Project name and description
- Language and framework choices
- Service boundary definitions
- Initial requirements

**Activities:**
1. `GenerateProjectStructure` - Create directory layout
2. `GenerateConfigs` - CI, linting, testing configs
3. `GenerateSkeleton` - Initial service code
4. `RunPR0Gate` - Validate compilation and formatting

**Timeouts:**
- Workflow: 30 minutes
- Per activity: 5 minutes

**Stop-Loss:**
- Max 3 retries per activity
- Terminate if no progress in 10 minutes

### 2. FeatureImplementationWorkflow

Implements a feature from requirement to verified code.

**Input:**
- Feature specification
- Target files/modules
- Test requirements

**Activities:**
1. `AnalyzeRequirement` - Parse and validate spec
2. `PlanImplementation` - Generate implementation plan
3. `GenerateCode` - Produce code as diffs
4. `GenerateTests` - Create unit tests
5. `RunPR0Gate` - Compilation and lint
6. `RunPR1Gate` - Unit tests and coverage

**Timeouts:**
- Workflow: 2 hours
- Code generation: 15 minutes
- Gate execution: 10 minutes

### 3. IntegrationTestWorkflow

Runs integration tests for a set of changes.

**Input:**
- Changed files
- Service dependencies
- Test scope

**Activities:**
1. `PrepareTestEnvironment` - Start dependencies
2. `RunIntegrationTests` - Execute test suite
3. `CollectCoverage` - Gather coverage data
4. `TeardownEnvironment` - Clean up

**Timeouts:**
- Workflow: 1 hour
- Environment prep: 10 minutes
- Test execution: 30 minutes

### 4. QualityGateWorkflow

Executes a specific quality gate level.

**Input:**
- Gate level (PR0/PR1/PR2)
- Artifact references
- Previous gate evidence

**Activities:**
- Varies by gate level (see Quality Gates doc)

**Note:** PR3 is planned for future phases and not yet implemented.

### 5. EvidenceBundleWorkflow

Collects and packages evidence for an artifact.

**Input:**
- Artifact identifier
- Gate results references
- Provenance chain

**Activities:**
1. `CollectGateResults` - Gather all gate outputs
2. `GenerateManifest` - Create provenance manifest
3. `ComputeChecksums` - Hash all components
4. `PackageBundle` - Create signed archive
5. `StoreBundle` - Persist to storage via Artifact API

## Workflow Patterns

### Progress Metrics

Every long-running activity reports progress:
```
type Progress struct {
    CurrentStep   int
    TotalSteps    int
    StepName      string
    TokensUsed    int
    TokenBudget   int
}
```

### Stop-Loss Evaluation

At each progress report:
1. Check time budget remaining
2. Check token budget remaining
3. Evaluate progress rate
4. If any threshold breached, initiate graceful termination

### Graceful Termination

When stop-loss triggers:
1. Mark current activity for completion
2. Collect partial results
3. Generate termination report
4. Store evidence of work completed
5. Return structured failure with diagnostics

## Activity Retry Policies

### Default Policy
- Initial interval: 1 second
- Backoff coefficient: 2.0
- Maximum interval: 1 minute
- Maximum attempts: 3

### Idempotent Activities
- File writes, API calls with idempotency keys
- Safe to retry without side effects

### Non-Idempotent Activities
- Wrapped with deduplication layer
- Activity ID used as idempotency key

## Visibility and Monitoring

All workflows emit:
- Start/complete events
- Activity transitions
- Progress updates
- Gate results
- Error conditions

Available in:
- Temporal UI (http://localhost:8080)
- Structured logs
- Metrics endpoint
