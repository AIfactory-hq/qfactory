-- Migration 002: Gate history table for qfactory v0.5
-- Adds append-only gate execution history with latest view support

-- run_gate_results table: append-only gate executions
CREATE TABLE IF NOT EXISTS run_gate_results (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    level TEXT NOT NULL,
    name TEXT NOT NULL,
    passed BOOLEAN NOT NULL,
    executor TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    timestamp TIMESTAMPTZ NOT NULL,
    duration_ms BIGINT NOT NULL DEFAULT 0,
    evidence_path TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    result JSONB NOT NULL
);

-- Index for run_id lookups
CREATE INDEX IF NOT EXISTS idx_run_gate_results_run_id ON run_gate_results(run_id);

-- Index for (run_id, level, name) lookups with timestamp ordering for latest queries
CREATE INDEX IF NOT EXISTS idx_run_gate_results_lookup ON run_gate_results(run_id, level, name, timestamp DESC);

-- Backfill: migrate existing gates from runs.gates_json into run_gate_results
-- Note: This is best-effort. Each existing gate gets an ID derived from run_id + hash.
-- Skip if gates_json is empty, null, or not an array.
DO $$
DECLARE
    r RECORD;
    gate_record JSONB;
    gate_id TEXT;
    gate_name TEXT;
BEGIN
    FOR r IN SELECT id, gates_json FROM runs WHERE gates_json IS NOT NULL AND gates_json != '[]'::jsonb AND jsonb_typeof(gates_json) = 'array' LOOP
        FOR gate_record IN SELECT * FROM jsonb_array_elements(r.gates_json) LOOP
            -- Generate deterministic ID from run_id and gate content
            gate_id := r.id || '-' || md5(gate_record::text);
            gate_name := COALESCE(gate_record->>'name', 'default');
            IF gate_name = '' THEN
                gate_name := 'default';
            END IF;

            -- Insert if not already exists (idempotent)
            INSERT INTO run_gate_results (
                id, run_id, level, name, passed, executor,
                started_at, completed_at, timestamp, duration_ms,
                evidence_path, error, result
            ) VALUES (
                gate_id,
                r.id,
                COALESCE(gate_record->>'level', 'PR1'),
                gate_name,
                COALESCE((gate_record->>'passed')::boolean, false),
                COALESCE(gate_record->>'executor', ''),
                (gate_record->>'started_at')::timestamptz,
                (gate_record->>'completed_at')::timestamptz,
                COALESCE((gate_record->>'timestamp')::timestamptz, NOW()),
                COALESCE((gate_record->>'duration_ms')::bigint, 0),
                COALESCE(gate_record->>'evidence_path', ''),
                COALESCE(gate_record->>'error', ''),
                gate_record
            ) ON CONFLICT (id) DO NOTHING;
        END LOOP;
    END LOOP;
END $$;

-- Record this migration
INSERT INTO schema_migrations (version) VALUES (2) ON CONFLICT (version) DO NOTHING;

-- Add comments for documentation
COMMENT ON TABLE run_gate_results IS 'Append-only gate execution history';
COMMENT ON COLUMN run_gate_results.result IS 'Full GateResult JSON for complete reconstruction';
