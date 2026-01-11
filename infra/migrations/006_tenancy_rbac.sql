-- v1.0: Multi-tenancy and RBAC support
-- Adds tenant/project isolation and policy hardening fields

-- Add tenancy columns to runs table
ALTER TABLE runs ADD COLUMN IF NOT EXISTS tenant_id TEXT NOT NULL DEFAULT 'default';
ALTER TABLE runs ADD COLUMN IF NOT EXISTS project_id TEXT NOT NULL DEFAULT 'default';

-- Add policy decision tracking
ALTER TABLE runs ADD COLUMN IF NOT EXISTS policy_decision JSONB NULL;
ALTER TABLE runs ADD COLUMN IF NOT EXISTS policy_snapshot JSONB NULL;

-- Add gate running state tracking for finalization safety
ALTER TABLE runs ADD COLUMN IF NOT EXISTS gates_running BOOLEAN NOT NULL DEFAULT FALSE;

-- Add indexes for tenant isolation
CREATE INDEX IF NOT EXISTS idx_runs_tenant_id ON runs (tenant_id);
CREATE INDEX IF NOT EXISTS idx_runs_project_id ON runs (project_id);
CREATE INDEX IF NOT EXISTS idx_runs_tenant_project ON runs (tenant_id, project_id);

-- Add tenancy to run_events table
ALTER TABLE run_events ADD COLUMN IF NOT EXISTS tenant_id TEXT NOT NULL DEFAULT 'default';
ALTER TABLE run_events ADD COLUMN IF NOT EXISTS project_id TEXT NOT NULL DEFAULT 'default';

CREATE INDEX IF NOT EXISTS idx_run_events_tenant_id ON run_events (tenant_id);

-- Add tenancy to run_gate_results table (latest view)
ALTER TABLE run_gate_results ADD COLUMN IF NOT EXISTS tenant_id TEXT NOT NULL DEFAULT 'default';
ALTER TABLE run_gate_results ADD COLUMN IF NOT EXISTS project_id TEXT NOT NULL DEFAULT 'default';

-- Add extended policy fields to runs (stored as part of gate_policy JSONB)
-- These are already JSON fields so no schema change needed, just documenting:
-- gate_policy: {
--   required_levels: ["PR1", "PR2", "PR3"],
--   required_gates: [{level, name}],
--   min_trust_score: 80,
--   allow_override: true,
--   require_all_passed: true,
--   max_age_seconds: 86400,
--   max_retries: 3
-- }
