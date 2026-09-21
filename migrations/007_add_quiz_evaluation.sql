-- Migration 007: Quiz evaluation (Phase 15).
-- Tracks per-user quiz performance: sessions + per-question attempts.
-- Stores per spec: quiz session, question, selected answer, correct answer,
-- is_correct, time_taken, plus topic/subject for weak-topic analysis
-- (per-topic accuracy, e.g. Deadlock 82%, Paging 61%).
-- Idempotent: safe to re-apply via the Makefile db-migrate loop
-- (CREATE TABLE/INDEX IF NOT EXISTS, ADD COLUMN IF NOT EXISTS).

-- Quiz sessions: one row per quiz evaluation run.
CREATE TABLE IF NOT EXISTS quiz_sessions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  mode TEXT NOT NULL DEFAULT '',
  subject TEXT NOT NULL DEFAULT '',
  total_questions INT NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'in_progress',
  created_at TIMESTAMPTZ DEFAULT now(),
  completed_at TIMESTAMPTZ
);

-- Backfill columns for databases created from the early 007 draft
-- (which stored denormalized metrics instead of mode/status).
ALTER TABLE quiz_sessions ADD COLUMN IF NOT EXISTS mode TEXT NOT NULL DEFAULT '';
ALTER TABLE quiz_sessions ADD COLUMN IF NOT EXISTS subject TEXT NOT NULL DEFAULT '';
ALTER TABLE quiz_sessions ADD COLUMN IF NOT EXISTS total_questions INT NOT NULL DEFAULT 0;
ALTER TABLE quiz_sessions ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'in_progress';
ALTER TABLE quiz_sessions ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ;

-- Quiz attempts: one row per question answered within a session.
-- question_id is TEXT (quiz item IDs like "quiz-q1" are not UUIDs), so no
-- FK to questions; topic/subject are denormalized for weak-topic analysis.
CREATE TABLE IF NOT EXISTS quiz_attempts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  session_id UUID NOT NULL REFERENCES quiz_sessions(id) ON DELETE CASCADE,
  question_id TEXT NOT NULL,
  source_question_id TEXT NOT NULL DEFAULT '',
  question_text TEXT NOT NULL DEFAULT '',
  selected_answer INT NOT NULL,
  correct_answer INT NOT NULL,
  is_correct BOOLEAN NOT NULL DEFAULT FALSE,
  time_taken_seconds DOUBLE PRECISION NOT NULL DEFAULT 0,
  topic TEXT NOT NULL DEFAULT '',
  subject TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ DEFAULT now()
);

-- Backfill columns for databases created from the early 007 draft
-- (UUID question_id with FK, TEXT answers, topic_id FK).
ALTER TABLE quiz_attempts ADD COLUMN IF NOT EXISTS source_question_id TEXT NOT NULL DEFAULT '';
ALTER TABLE quiz_attempts ADD COLUMN IF NOT EXISTS question_text TEXT NOT NULL DEFAULT '';
ALTER TABLE quiz_attempts ADD COLUMN IF NOT EXISTS topic TEXT NOT NULL DEFAULT '';
ALTER TABLE quiz_attempts ADD COLUMN IF NOT EXISTS subject TEXT NOT NULL DEFAULT '';

-- Normalize early-draft column types when present (no-op when already correct).
DO $$ BEGIN
  BEGIN
    ALTER TABLE quiz_attempts ALTER COLUMN question_id TYPE TEXT USING question_id::text;
  EXCEPTION WHEN others THEN NULL;
  END;
  BEGIN
    ALTER TABLE quiz_attempts ALTER COLUMN selected_answer TYPE INT USING selected_answer::int;
  EXCEPTION WHEN others THEN NULL;
  END;
  BEGIN
    ALTER TABLE quiz_attempts ALTER COLUMN correct_answer TYPE INT USING correct_answer::int;
  EXCEPTION WHEN others THEN NULL;
  END;
END $$;

-- Drop early-draft FK constraints that required UUID question/topic IDs.
DO $$ DECLARE r RECORD; BEGIN
  FOR r IN SELECT conname FROM pg_constraint WHERE conrelid = 'quiz_attempts'::regclass AND contype = 'f' LOOP
    IF r.conname LIKE '%question_id%' OR r.conname LIKE '%topic_id%' THEN
      EXECUTE 'ALTER TABLE quiz_attempts DROP CONSTRAINT IF EXISTS ' || quote_ident(r.conname);
    END IF;
  END LOOP;
END $$;
ALTER TABLE quiz_attempts DROP COLUMN IF EXISTS topic_id;

-- Indexes for session lookup, metrics aggregation, and weak-topic analysis.
CREATE INDEX IF NOT EXISTS idx_quiz_sessions_status ON quiz_sessions(status);
CREATE INDEX IF NOT EXISTS idx_quiz_sessions_created_at ON quiz_sessions(created_at);
CREATE INDEX IF NOT EXISTS idx_quiz_attempts_session_id ON quiz_attempts(session_id);
CREATE INDEX IF NOT EXISTS idx_quiz_attempts_question_id ON quiz_attempts(question_id);
CREATE INDEX IF NOT EXISTS idx_quiz_attempts_topic ON quiz_attempts(topic);
CREATE INDEX IF NOT EXISTS idx_quiz_attempts_subject ON quiz_attempts(subject);
CREATE INDEX IF NOT EXISTS idx_quiz_attempts_session_topic ON quiz_attempts(session_id, topic);
