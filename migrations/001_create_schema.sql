-- Enable pgvector extension
CREATE EXTENSION IF NOT EXISTS vector;

-- Documents
CREATE TABLE IF NOT EXISTS documents (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  filename TEXT NOT NULL,
  original_filename TEXT,
  storage_path TEXT,
  subject TEXT,
  year INT,
  status TEXT DEFAULT 'uploaded',
  created_at TIMESTAMPTZ DEFAULT now(),
  updated_at TIMESTAMPTZ DEFAULT now()
);

-- Questions
CREATE TABLE IF NOT EXISTS questions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  document_id UUID REFERENCES documents(id) ON DELETE CASCADE,
  question_number TEXT,
  question_text TEXT,
  page_number INT,
  year INT,
  subject TEXT,
  difficulty TEXT,
  created_at TIMESTAMPTZ DEFAULT now(),
  updated_at TIMESTAMPTZ DEFAULT now()
);

-- Topics
CREATE TABLE IF NOT EXISTS topics (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL,
  subject TEXT,
  created_at TIMESTAMPTZ DEFAULT now()
);

-- Question <-> Topic
CREATE TABLE IF NOT EXISTS question_topics (
  question_id UUID REFERENCES questions(id) ON DELETE CASCADE,
  topic_id UUID REFERENCES topics(id) ON DELETE CASCADE,
  PRIMARY KEY (question_id, topic_id)
);

-- Images
CREATE TABLE IF NOT EXISTS question_images (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  question_id UUID REFERENCES questions(id) ON DELETE CASCADE,
  storage_path TEXT,
  page_number INT,
  image_index INT,
  caption TEXT,
  created_at TIMESTAMPTZ DEFAULT now()
);

-- Embeddings
-- NOTE: a dimension must be supplied for the vector type. 1536 is used as a reasonable default
-- but update after selecting an embedding model.
CREATE TABLE IF NOT EXISTS embeddings (
  question_id UUID PRIMARY KEY REFERENCES questions(id) ON DELETE CASCADE,
  embedding vector(1536),
  model TEXT,
  created_at TIMESTAMPTZ DEFAULT now()
);

-- Indexes (examples)
CREATE INDEX IF NOT EXISTS idx_documents_subject_year ON documents(subject, year);
CREATE INDEX IF NOT EXISTS idx_questions_document_id ON questions(document_id);
CREATE INDEX IF NOT EXISTS idx_topics_name ON topics(name);
CREATE INDEX IF NOT EXISTS idx_embeddings_vector ON embeddings USING ivfflat (embedding) WITH (lists = 100);

-- Trigger to update updated_at timestamp
CREATE OR REPLACE FUNCTION trigger_set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER set_updated_at
BEFORE UPDATE ON documents
FOR EACH ROW EXECUTE PROCEDURE trigger_set_updated_at();

CREATE TRIGGER set_updated_at_questions
BEFORE UPDATE ON questions
FOR EACH ROW EXECUTE PROCEDURE trigger_set_updated_at();
