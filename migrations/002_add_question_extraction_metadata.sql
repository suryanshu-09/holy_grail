-- Add extraction metadata to questions
ALTER TABLE questions
  ADD COLUMN IF NOT EXISTS start_page INT,
  ADD COLUMN IF NOT EXISTS end_page INT,
  ADD COLUMN IF NOT EXISTS start_offset INT,
  ADD COLUMN IF NOT EXISTS end_offset INT,
  ADD COLUMN IF NOT EXISTS confidence DOUBLE PRECISION DEFAULT 0.0,
  ADD COLUMN IF NOT EXISTS question_type TEXT,
  ADD COLUMN IF NOT EXISTS options_json JSONB,
  ADD COLUMN IF NOT EXISTS extraction_notes_json JSONB,
  ADD COLUMN IF NOT EXISTS images_json JSONB;

-- Index for faster lookups by document + page range
CREATE INDEX IF NOT EXISTS idx_questions_document_start_end ON questions(document_id, start_page, end_page);
