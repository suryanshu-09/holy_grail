-- Basic seed data for development
INSERT INTO topics (id, name, subject) VALUES ('00000000-0000-0000-0000-000000000001', 'Deadlock', 'Operating Systems') ON CONFLICT DO NOTHING;

INSERT INTO documents (id, filename, original_filename, storage_path, subject, year, status) VALUES
  ('00000000-0000-0000-0000-000000000010', 'os_pyq_2018.pdf', 'os_pyq_2018.pdf', '/storage/os_pyq_2018.pdf', 'Operating Systems', 2018, 'processed') ON CONFLICT DO NOTHING;

INSERT INTO questions (id, document_id, question_number, question_text, page_number, year, subject) VALUES
  ('00000000-0000-0000-0000-000000000100', '00000000-0000-0000-0000-000000000010', 'Q1', 'Explain deadlock conditions and prevention.', 12, 2018, 'Operating Systems') ON CONFLICT DO NOTHING;

INSERT INTO question_topics (question_id, topic_id) VALUES
  ('00000000-0000-0000-0000-000000000100', '00000000-0000-0000-0000-000000000001') ON CONFLICT DO NOTHING;

-- No embeddings seeded by default
