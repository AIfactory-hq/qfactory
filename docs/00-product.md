# qfactory Product Definition

## Vision

qfactory is an enterprise Software Factory that transforms high-level software requirements into verified, deployable artifacts through AI-assisted workflows with deterministic execution guarantees.

## Problem Statement

Current AI coding assistants suffer from:
1. **Unbounded execution**: No guaranteed termination or resource limits
2. **Unverified output**: Generated code lacks systematic quality assurance
3. **No provenance**: Impossible to audit what happened and why
4. **Cloud dependency**: Require constant internet connectivity
5. **Conversational loops**: Waste cycles on back-and-forth chat

## Solution

qfactory provides:
- Workflow-based execution with explicit bounds and stop-loss mechanisms
- Four-tier quality gates (PR0-PR3) with evidence bundles
- Complete artifact provenance from intent to deployment
- Air-gapped operation with local model runtimes
- Artifact-only handoffs—no conversational agent loops

## Core Capabilities

### 1. Workflow Orchestration
- Temporal-based durable execution
- Explicit timeout and retry policies
- Progress metrics with stop-loss termination

### 2. Quality Verification
- Automated test generation and execution
- Contract validation at service boundaries
- Security scanning and compliance checks

### 3. MCP Tool Plane
- Standardized tool interface for actions and inspections
- File operations, build commands, security scans, code analysis
- Structured input/output schemas with audit trail

### 4. Pluggable Model Runtime
- Separate abstraction for LLM access
- Cloud APIs or local models (Ollama, vLLM)
- Token tracking and budget enforcement

### 5. Evidence Collection
- Build logs, test results, coverage reports
- Security scan outputs
- Artifact checksums and signatures

## Target Users

1. **Platform Engineers**: Configure workflows and quality gates
2. **Development Teams**: Submit requirements and receive verified artifacts
3. **Security/Compliance**: Audit evidence bundles and provenance chains

## Success Metrics

- Time from requirement to deployable artifact
- Quality gate pass rate at each PR level
- Evidence bundle completeness score
- Air-gapped deployment success rate
