-- Migration 009: Authentication and user data (Phase 20).
-- Users + sessions (opaque bearer tokens, SHA-256 hash persisted) +
-- per-user preferences, with user_id ownership on documents and
-- quiz_sessions. Legacy rows keep NULL user_id (public/anonymous) so
-- pre-auth deployments keep working.
-- Idempotent: safe to re-apply via the Makefile db-migrate loop.

-- Users: email login with bcrypt password hash.
CREATE TABLE IF NOT EXISTS users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL,
  created_at TIMESTAMPTZ DEFAULT now(),
  updated_at TIMESTAMPTZ DEFAULT now()
);
ALTER TABLE users ADD COLUMN IF NOT EXISTS email TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS display_name TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS password_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT now();
ALTER TABLE users ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT now();

-- Case-insensitive unique email (citrate-less lower() index).
DO $$ BEGIN
  BEGIN
    CREATE UNIQUE INDEX users_email_unique ON users (lower(email));
  EXCEPTION WHEN duplicate_object THEN NULL;
  END;
END $$;
CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);

-- Sessions: one row per login; only the token hash is stored.
CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ DEFAULT now(),
  expires_at TIMESTAMPTZ NOT NULL
);
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS user_id UUID REFERENCES users(id) ON DELETE CASCADE;
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ DEFAULT now();
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);

-- Per-user preferences (quiz defaults).
CREATE TABLE IF NOT EXISTS user_preferences (
  user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  default_subject TEXT NOT NULL DEFAULT '',
  preferred_difficulty TEXT NOT NULL DEFAULT '',
  default_quiz_length INT NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ DEFAULT now()
);
ALTER TABLE user_preferences ADD COLUMN IF NOT EXISTS default_subject TEXT NOT NULL DEFAULT '';
ALTER TABLE user_preferences ADD COLUMN IF NOT EXISTS preferred_difficulty TEXT NOT NULL DEFAULT '';
ALTER TABLE user_preferences ADD COLUMN IF NOT EXISTS default_quiz_length INT NOT NULL DEFAULT 0;
ALTER TABLE user_preferences ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT now();

-- Ownership: nullable user_id on documents and quiz_sessions.
-- NULL = legacy/anonymous row, visible to everyone (including anonymous).
ALTER TABLE documents ADD COLUMN IF NOT EXISTS user_id UUID REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE quiz_sessions ADD COLUMN IF NOT EXISTS user_id UUID REFERENCES users(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_documents_user_id ON documents(user_id);
CREATE INDEX IF NOT EXISTS idx_quiz_sessions_user_id ON quiz_sessions(user_id);

-- Keep updated_at fresh on writes (reuses trigger function from 001).
DROP TRIGGER IF EXISTS set_updated_at_users ON users;
CREATE TRIGGER set_updated_at_users
BEFORE UPDATE ON users
FOR EACH ROW EXECUTE PROCEDURE trigger_set_updated_at();
