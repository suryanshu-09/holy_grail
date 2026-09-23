-- Migration 008: Background jobs (Phase 19).
-- Persistent storage for background job state: status, progress,
-- retries, dedup key, and timeout.
-- Idempotent: safe to re-apply via the Makefile db-migrate loop
-- (CREATE TABLE/INDEX IF NOT EXISTS, ADD COLUMN IF NOT EXISTS).

CREATE TABLE IF NOT EXISTS jobs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  type TEXT NOT NULL,
  document_id UUID REFERENCES documents(id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'pending',
  progress INT NOT NULL DEFAULT 0,
  current_step TEXT NOT NULL DEFAULT '',
  payload JSONB NOT NULL DEFAULT '{}',
  result JSONB,
  last_error TEXT NOT NULL DEFAULT '',
  attempts INT NOT NULL DEFAULT 0,
  max_retries INT NOT NULL DEFAULT 5,
  timeout_seconds INT NOT NULL DEFAULT 600,
  unique_key TEXT UNIQUE,
  created_at TIMESTAMPTZ DEFAULT now(),
  updated_at TIMESTAMPTZ DEFAULT now(),
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ
);

-- Backfill columns for databases created from an early 008 draft.
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS type TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS document_id UUID REFERENCES documents(id) ON DELETE CASCADE;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'pending';
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS progress INT NOT NULL DEFAULT 0;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS current_step TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS payload JSONB NOT NULL DEFAULT '{}';
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS result JSONB;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS attempts INT NOT NULL DEFAULT 0;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS max_retries INT NOT NULL DEFAULT 5;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS timeout_seconds INT NOT NULL DEFAULT 600;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS unique_key TEXT;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT now();
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT now();
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ;

-- Enforce uniqueness of the dedup key (no-op if already present).
DO $$ BEGIN
  BEGIN
    ALTER TABLE jobs ADD CONSTRAINT jobs_unique_key_unique UNIQUE (unique_key);
  EXCEPTION WHEN duplicate_object THEN NULL;
  END;
END $$;

-- Indexes for status polling, per-document lookup, dedup, and ordering.
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
CREATE INDEX IF NOT EXISTS idx_jobs_type ON jobs(type);
CREATE INDEX IF NOT EXISTS idx_jobs_document_id ON jobs(document_id);
CREATE INDEX IF NOT EXISTS idx_jobs_unique_key ON jobs(unique_key);
CREATE INDEX IF NOT EXISTS idx_jobs_status_created_at ON jobs(status, created_at);
CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs(created_at);

-- Keep updated_at fresh on writes (reuses trigger function from 001).
DROP TRIGGER IF EXISTS set_updated_at_jobs ON jobs;
CREATE TRIGGER set_updated_at_jobs
BEFORE UPDATE ON jobs
FOR EACH ROW EXECUTE PROCEDURE trigger_set_updated_at();
