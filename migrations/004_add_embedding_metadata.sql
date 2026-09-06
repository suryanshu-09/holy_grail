-- Phase 10: make embeddings reproducible and safe to regenerate.
-- `embedding_input_hash` identifies the normalized rich input used to create a
-- vector. It lets the application skip unchanged questions and reuse an
-- existing vector for duplicate question content without another API call.
ALTER TABLE embeddings
  ADD COLUMN IF NOT EXISTS embedding_input_hash TEXT,
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT now();

CREATE INDEX IF NOT EXISTS idx_embeddings_model_input_hash
  ON embeddings (model, embedding_input_hash)
  WHERE embedding_input_hash IS NOT NULL;
