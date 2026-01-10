# qfactory User Journeys

## Journey 1: Greenfield Project

### Persona
Platform Engineer setting up a new microservice

### Flow

1. **Initialize Project**
   - Engineer submits project spec via API/UI
   - Spec includes: language, framework, service boundaries, initial requirements

2. **Scaffold Generation**
   - qfactory generates project structure
   - Creates standard configs (CI, linting, testing)
   - Produces initial service skeleton

3. **PR0 Gate**
   - Verify code compiles
   - Confirm formatting passes
   - Check lint rules

4. **Feature Implementation**
   - Engineer submits feature requirements
   - qfactory generates implementation as diffs
   - Generates corresponding tests

5. **PR1 Gate**
   - Run unit tests
   - Check coverage thresholds
   - Validate contracts

6. **Integration**
   - qfactory generates integration tests
   - Creates docker-compose for dependencies

7. **PR2 Gate**
   - Run integration tests
   - Execute security scan
   - Verify API contracts

8. **Evidence Bundle**
   - Collect all gate results
   - Generate provenance manifest
   - Store for audit

## Journey 2: Brownfield Enhancement

### Persona
Developer adding feature to existing codebase

### Flow

1. **Codebase Ingestion**
   - qfactory indexes existing code in Qdrant
   - Analyzes patterns and conventions
   - Maps service boundaries

2. **Requirement Submission**
   - Developer describes feature in structured format
   - Specifies affected areas and constraints

3. **Impact Analysis**
   - qfactory identifies affected files
   - Detects potential conflicts
   - Estimates change scope

4. **Implementation**
   - Generates changes as minimal diffs
   - Follows existing patterns
   - Adds/modifies tests

5. **Gate Progression (PR0 → PR1 → PR2)**
   - Each gate validates incrementally
   - Failures produce actionable diagnostics
   - Evidence collected at each stage

6. **Human Review**
   - Developer reviews generated diffs
   - Approves or requests modifications
   - Final PR3 gate after approval (when enabled)

## Journey 3: Air-Gapped Deployment

### Persona
Security Engineer deploying in restricted environment

### Flow

1. **Pre-Staging (Connected)**
   - Download all container images
   - Cache model weights
   - Package all dependencies

2. **Transfer**
   - Move artifacts to air-gapped environment
   - Verify checksums

3. **Deployment**
   - Start qfactory with local model runtime
   - Configure to use local registries
   - Disable all outbound checks

4. **Validation**
   - Verify all services start
   - Confirm model inference works
   - Test end-to-end workflow

## Journey 4: Compliance Audit

### Persona
Compliance Officer reviewing software provenance

### Flow

1. **Evidence Bundle Request**
   - Request bundle for specific artifact version

2. **Bundle Contents**
   - Original requirements
   - All intermediate artifacts
   - Gate results and logs
   - Checksums and signatures

3. **Provenance Chain**
   - Trace from requirement to deployment
   - Verify all gates passed
   - Confirm no manual bypasses

4. **Report Generation**
   - Generate compliance report
   - Export for audit systems
