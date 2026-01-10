# ADR-0005: Verification Mesh and PR Levels

## Status

Accepted

## Context

AI-generated code requires verification before use. We need a systematic approach to quality assurance that:
- Provides confidence appropriate to the use case
- Produces evidence for audit
- Allows incremental verification
- Catches different classes of issues

## Decision

We implement a **four-tier quality gate system** (PR0-PR3), each level building on the previous:

### PR0: Compile and Format
- Does it compile?
- Is it properly formatted?
- Does it pass linting?

### PR1: Unit Testing
- Do unit tests pass?
- Is coverage sufficient?
- Are contracts valid?

### PR2: Integration
- Do integration tests pass?
- Is it secure (SAST, dependency audit)?
- Does it meet performance baselines?

### PR3: System (Future)
- Do E2E tests pass?
- Does it handle load?
- Can it be rolled back?

**Note**: PR3 is planned for future phases. Current implementation covers PR0-PR2.

### Evidence at Each Level
Every gate produces evidence that becomes part of the artifact's provenance:
- Logs and outputs
- Metrics and measurements
- Pass/fail determination
- Timestamp and executor

## Consequences

### Positive
- Progressive confidence building
- Clear evidence for each level
- Configurable per project
- Supports different deployment requirements

### Negative
- Overhead for simple changes
- May slow iteration speed
- Requires infrastructure for each gate type

## Alternatives Considered

### Single Gate
- Pro: Simple
- Con: No incremental feedback, all-or-nothing

### Continuous Testing Only
- Pro: Fast feedback
- Con: No clear milestone for "ready"
