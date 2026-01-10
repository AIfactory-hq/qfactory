# qfactory Quality Gates

## Overview

qfactory implements a four-tier quality gate system (PR0-PR3) that ensures all generated artifacts meet defined quality standards before progression. Each gate produces evidence that becomes part of the artifact's provenance chain.

**Current Implementation Status:**
- PR0: Implemented
- PR1: Implemented
- PR2: Implemented
- PR3: Planned (future phase)

## Gate Levels

### PR0: Lint and Format

**Purpose**: Ensure code is syntactically valid and consistently formatted.

**Checks**:
- [ ] Code compiles without errors
- [ ] Formatting matches project standards (gofmt, prettier, black, etc.)
- [ ] Linting passes with zero errors (golangci-lint, eslint, etc.)
- [ ] Import organization correct
- [ ] No banned patterns detected

**Evidence Collected**:
- Compiler output
- Formatter diff (should be empty)
- Linter output
- Timestamp and executor

**Pass Criteria**: All checks pass with zero errors

**Typical Duration**: 1-2 minutes

### PR1: Unit Testing

**Purpose**: Verify individual components work correctly in isolation.

**Checks**:
- [ ] All unit tests pass
- [ ] Coverage meets threshold (default: 80% for new code)
- [ ] No flaky tests detected
- [ ] Contract tests pass (if applicable)
- [ ] Mutation testing score (optional)

**Evidence Collected**:
- Test execution output
- Coverage report (line, branch, function)
- Test timing breakdown
- Failed test details (if any)

**Pass Criteria**: 100% test pass, coverage >= threshold

**Typical Duration**: 2-10 minutes

### PR2: Integration Testing

**Purpose**: Verify components work correctly together.

**Checks**:
- [ ] Integration tests pass
- [ ] API contract verification
- [ ] Security scan clean (SAST, dependency audit)
- [ ] Performance baseline met
- [ ] No regression in existing functionality

**Evidence Collected**:
- Integration test output
- API contract validation report
- Security scan report (SARIF format)
- Performance metrics
- Dependency audit report

**Pass Criteria**: All integration tests pass, no high/critical security findings, performance within 10% of baseline

**Typical Duration**: 10-30 minutes

### PR3: System Validation (Future)

**Status**: Planned for future implementation. Not part of v0.x workflows.

**Purpose**: Verify end-to-end system behavior in production-like environment.

**Planned Checks**:
- [ ] E2E smoke tests pass
- [ ] Performance under load acceptable
- [ ] Error handling verified
- [ ] Rollback procedure validated
- [ ] Documentation generated/updated

**Planned Evidence**:
- E2E test results
- Load test metrics
- Error scenario test results
- Rollback test results
- Documentation delta

## Evidence Bundle Requirements

Every artifact must have a complete evidence bundle containing:

### Mandatory Components (PR0-PR2)
1. **Requirement Trace**: Link to original requirement/ticket
2. **PR0 Evidence**: All PR0 check outputs
3. **PR1 Evidence**: All PR1 check outputs
4. **PR2 Evidence**: All PR2 check outputs
5. **Artifact Manifest**: List of all files with checksums
6. **Provenance Chain**: Parent artifacts and workflows

### Future Components (PR3)
7. **PR3 Evidence**: System validation outputs
8. **Performance Report**: Detailed performance analysis
9. **Security Attestation**: Signed security review

### Bundle Format
```
evidence-bundle/
├── manifest.json           # Bundle metadata and checksums
├── provenance.json         # Full provenance chain
├── requirements/
│   └── requirement.md      # Original requirement
├── pr0/
│   ├── compile.log
│   ├── format.diff
│   └── lint.json
├── pr1/
│   ├── tests.xml           # JUnit format
│   ├── coverage.json
│   └── contracts.json
├── pr2/
│   ├── integration.xml
│   ├── security.sarif
│   └── performance.json
└── signature.sig           # Bundle signature
```

## Gate Configuration

Gates are configurable per project:

```yaml
quality_gates:
  pr0:
    enabled: true
    timeout: 5m
    checks:
      compile: true
      format: true
      lint: true
      lint_config: .golangci.yml

  pr1:
    enabled: true
    timeout: 15m
    checks:
      unit_tests: true
      coverage_threshold: 80
      coverage_for: new_code  # new_code | all_code
      contract_tests: true

  pr2:
    enabled: true
    timeout: 30m
    checks:
      integration_tests: true
      security_scan: true
      security_severity_threshold: high
      performance_baseline: true
      performance_tolerance: 0.1

  pr3:
    enabled: false  # Future: enable for production deployments
    timeout: 60m
```

## Gate Failure Handling

When a gate fails:

1. **Immediate**: Stop workflow progression
2. **Diagnostic**: Collect detailed failure information
3. **Report**: Generate actionable failure report
4. **Evidence**: Store failure evidence for analysis
5. **Notify**: Alert relevant stakeholders

Failure reports include:
- Which specific checks failed
- Detailed error messages
- Suggested remediation steps
- Links to relevant documentation

## Manual Override

Gates can be manually overridden with:
- Explicit approval from authorized user
- Documented justification
- Override recorded in evidence bundle
- Audit trail maintained

Override requires minimum role level (configurable per gate).
