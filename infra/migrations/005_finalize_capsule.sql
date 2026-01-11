-- Migration 005: Add run finalization and capsule support
-- Idempotent: safe to run multiple times

-- Add finalized_at column if not exists
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'runs' AND column_name = 'finalized_at'
    ) THEN
        ALTER TABLE runs ADD COLUMN finalized_at TIMESTAMPTZ NULL;
    END IF;
END $$;

-- Add finalized_by column if not exists
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'runs' AND column_name = 'finalized_by'
    ) THEN
        ALTER TABLE runs ADD COLUMN finalized_by TEXT NULL;
    END IF;
END $$;

-- Add finalize_reason column if not exists
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'runs' AND column_name = 'finalize_reason'
    ) THEN
        ALTER TABLE runs ADD COLUMN finalize_reason TEXT NULL;
    END IF;
END $$;

-- Add finalize_override column if not exists
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'runs' AND column_name = 'finalize_override'
    ) THEN
        ALTER TABLE runs ADD COLUMN finalize_override BOOLEAN NOT NULL DEFAULT FALSE;
    END IF;
END $$;

-- Add finalize_override_reason column if not exists
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'runs' AND column_name = 'finalize_override_reason'
    ) THEN
        ALTER TABLE runs ADD COLUMN finalize_override_reason TEXT NULL;
    END IF;
END $$;

-- Add capsule_id column if not exists
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'runs' AND column_name = 'capsule_id'
    ) THEN
        ALTER TABLE runs ADD COLUMN capsule_id TEXT NULL;
    END IF;
END $$;

-- Add capsule_path column if not exists
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'runs' AND column_name = 'capsule_path'
    ) THEN
        ALTER TABLE runs ADD COLUMN capsule_path TEXT NULL;
    END IF;
END $$;

-- Add capsule_manifest_sha256 column if not exists
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'runs' AND column_name = 'capsule_manifest_sha256'
    ) THEN
        ALTER TABLE runs ADD COLUMN capsule_manifest_sha256 TEXT NULL;
    END IF;
END $$;

-- Create index on finalized_at if not exists
CREATE INDEX IF NOT EXISTS idx_runs_finalized_at
ON runs (finalized_at) WHERE finalized_at IS NOT NULL;

-- Create index on capsule_id if not exists
CREATE INDEX IF NOT EXISTS idx_runs_capsule_id
ON runs (capsule_id) WHERE capsule_id IS NOT NULL;
