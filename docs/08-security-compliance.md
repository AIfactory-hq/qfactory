# qfactory Security and Compliance

## Security Architecture

### Defense in Depth

qfactory implements multiple security layers:

1. **Network Isolation**: Execution plane isolated from control plane
2. **Sandboxed Execution**: Code execution in restricted containers
3. **Least Privilege**: Services run with minimal permissions
4. **Encryption**: TLS for transit, encryption at rest for sensitive data
5. **Audit Logging**: Complete audit trail of all operations

### Threat Model

**Assets Protected**:
- Source code and generated artifacts
- Model weights and configurations
- Credentials and secrets
- Audit logs and evidence bundles

**Threat Actors Considered**:
- External attackers (network-based)
- Malicious insiders
- Compromised dependencies
- Prompt injection attacks

### Security Controls

#### Authentication
- API authentication via JWT or API keys
- Service-to-service mTLS
- No default credentials
- Credential rotation support

#### Authorization
- Role-based access control (RBAC)
- Project-level isolation
- Principle of least privilege
- Audit of authorization decisions

#### Secrets Management
- Secrets never in code or config files
- External secrets provider integration (Vault, AWS SM)
- Encrypted at rest
- Rotation without restart

## Sandboxed Execution

All code execution occurs in sandboxed environments:

### Container Isolation
- Unprivileged containers
- Read-only root filesystem
- No network access (except explicit allowlist)
- Resource limits (CPU, memory, disk)
- Seccomp profiles
- No new privileges

### Filesystem Restrictions
- Workspace mounted read-write
- System directories read-only
- No access to host filesystem
- Temporary files in ephemeral storage

### Process Restrictions
- No root execution
- Limited capabilities
- Process namespace isolation
- No access to host processes

## Prompt Injection Defense

AI model interactions are protected against prompt injection:

1. **Input Sanitization**: User input separated from system prompts
2. **Output Validation**: Model outputs parsed and validated
3. **Execution Boundaries**: Model suggestions never execute automatically
4. **Context Isolation**: Each workflow has isolated context

## Compliance Considerations

### Audit Trail

Every operation produces audit records:
- Who initiated the action
- What action was performed
- When it occurred
- What was affected
- What was the outcome

Audit logs are:
- Immutable (append-only)
- Tamper-evident (hash chain)
- Retained per policy (configurable)
- Searchable and exportable

### Evidence for Compliance

Evidence bundles support:
- **SOC 2**: Change management controls
- **ISO 27001**: Information security controls
- **GDPR**: Data processing records
- **HIPAA**: Access controls and audit trails
- **FedRAMP**: Continuous monitoring

### Data Residency

qfactory supports data residency requirements:
- All processing in designated region
- No data leaves defined boundaries
- Air-gapped mode for strictest requirements
- Configurable data retention

## Vulnerability Management

### Dependency Scanning
- Automated dependency audit
- CVE monitoring
- Automated PR for security updates
- Block deployment with critical CVEs

### Static Analysis
- SAST integrated in PR2 gate
- Custom rules for qfactory patterns
- No high/critical findings pass gate

### Container Scanning
- Image vulnerability scanning
- Base image updates
- Minimal base images (distroless where possible)

## Incident Response

### Detection
- Anomaly detection on API patterns
- Failed authentication monitoring
- Resource usage anomalies
- Audit log analysis

### Response
- Automated alerting
- Incident runbooks
- Evidence preservation
- Post-incident review

## Security Checklist for Deployment

- [ ] Default credentials changed
- [ ] TLS configured for all endpoints
- [ ] Secrets in external provider
- [ ] Network policies configured
- [ ] Audit logging enabled
- [ ] Container security policies applied
- [ ] Vulnerability scanning enabled
- [ ] Access control configured
- [ ] Backup encryption enabled
- [ ] Incident response plan documented
