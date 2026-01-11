-- Migration 004: Add gate lineage tracking (parent_execution_id, retry_reason)
-- Idempotent: safe to run multiple times

-- Add parent_execution_id column if not exists
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'run_gate_results' AND column_name = 'parent_execution_id'
    ) THEN
        ALTER TABLE run_gate_results ADD COLUMN parent_execution_id TEXT NULL;
    END IF;
END $$;

-- Add retry_reason column if not exists
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'run_gate_results' AND column_name = 'retry_reason'
    ) THEN
        ALTER TABLE run_gate_results ADD COLUMN retry_reason TEXT NULL;
    END IF;
END $$;

-- Create index on (run_id, level, name, parent_execution_id) if not exists
CREATE INDEX IF NOT EXISTS idx_run_gate_results_lineage
ON run_gate_results (run_id, level, name, parent_execution_id);

-- Create index on (run_id, parent_execution_id) if not exists
CREATE INDEX IF NOT EXISTS idx_run_gate_results_parent
ON run_gate_results (run_id, parent_execution_id);
