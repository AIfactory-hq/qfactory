# ADR-0001: Product Scope

## Status

Accepted

## Context

We need to define the boundaries of qfactory to ensure focused development and clear communication about what the product does and does not do. Without clear scope, there is risk of feature creep and misaligned expectations.

## Decision

qfactory is a **Software Factory** that orchestrates AI-assisted software development workflows. It is:

**In Scope:**
- Workflow orchestration for code generation, testing, and quality verification
- Quality gates with evidence collection
- Artifact provenance tracking
- AI model integration via pluggable runtime
- MCP tool plane for actions and inspections
- Air-gapped operation capability
- Developer-facing API and dashboard

**Out of Scope:**
- General-purpose AI chat assistant
- IDE or editor functionality
- Version control system (uses existing Git)
- CI/CD platform (integrates with existing)
- Project management tools
- Human code review (facilitates, not replaces)

## Consequences

### Positive
- Clear focus for development priorities
- Easy to explain product positioning
- Avoid building features better served by existing tools

### Negative
- May disappoint users expecting chat-based interaction
- Requires integration with external tools for complete workflow

## Alternatives Considered

1. **Full IDE replacement**: Too broad, competing with mature products
2. **CI/CD platform**: Already solved by Jenkins, GitHub Actions, etc.
3. **Chat-based assistant**: Does not provide guarantees we need
