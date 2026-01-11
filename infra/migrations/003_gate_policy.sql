-- Migration 003: Gate policy for qfactory v0.6
-- Adds gate policy column to runs table

-- Add gate_policy_json column to runs
ALTER TABLE runs ADD COLUMN IF NOT EXISTS gate_policy_json JSONB DEFAULT '{}';

-- Record this migration
INSERT INTO schema_migrations (version) VALUES (3) ON CONFLICT (version) DO NOTHING;

-- Add comment for documentation
COMMENT ON COLUMN runs.gate_policy_json IS 'Gate policy defining required gates and freshness constraints';
