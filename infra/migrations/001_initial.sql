-- Migration 001: Initial schema for qfactory v0.4a
-- Replaces in-memory Store with Postgres persistence

-- runs table: stores workflow run metadata
CREATE TABLE IF NOT EXISTS runs (
    id TEXT PRIMARY KEY,
    temporal_id TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    current_stage TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    error TEXT,
    stages_json JSONB NOT NULL DEFAULT '[]',
    gates_json JSONB DEFAULT '[]',
    budget_policy_json JSONB,
    budget_status_json JSONB,
    model_calls_json JSONB DEFAULT '[]'
);

-- Index for listing runs by most recent
CREATE INDEX IF NOT EXISTS idx_runs_updated_at ON runs(updated_at DESC);

-- Index for status filtering
CREATE INDEX IF NOT EXISTS idx_runs_status ON runs(status);

-- run_events table: stores events for each run
CREATE TABLE IF NOT EXISTS run_events (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    type TEXT NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    stage_name TEXT,
    stage_index INT,
    payload_json JSONB NOT NULL DEFAULT '{}'
);

-- Index for listing events by run and time
CREATE INDEX IF NOT EXISTS idx_run_events_run_id_timestamp ON run_events(run_id, timestamp);

-- Index for run_id lookups
CREATE INDEX IF NOT EXISTS idx_run_events_run_id ON run_events(run_id);

-- schema_migrations table for tracking applied migrations
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Record this migration
INSERT INTO schema_migrations (version) VALUES (1) ON CONFLICT (version) DO NOTHING;

-- Add comment for documentation
COMMENT ON TABLE runs IS 'Workflow run metadata and state';
COMMENT ON TABLE run_events IS 'Events emitted during workflow execution';
