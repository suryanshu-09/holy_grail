package embeddings

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

type repository struct {
	db *sql.DB
}

// NewRepository creates the PostgreSQL/pgvector embedding repository.
func NewRepository(db *sql.DB) Repository {
	return &repository{db: db}
}

func (r *repository) Get(ctx context.Context, questionID string) (Record, bool, error) {
	var record Record
	err := r.db.QueryRowContext(ctx,
		`SELECT question_id, model, COALESCE(embedding_input_hash, ''), created_at, COALESCE(updated_at, created_at)
		 FROM embeddings WHERE question_id = $1`, questionID).
		Scan(&record.QuestionID, &record.Model, &record.InputHash, &record.CreatedAt, &record.UpdatedAt)
	if err == sql.ErrNoRows {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("embeddings: get: %w", err)
	}
	return record, true, nil
}

func (r *repository) FindReusable(ctx context.Context, model, inputHash string) (Record, bool, error) {
	var record Record
	err := r.db.QueryRowContext(ctx,
		`SELECT question_id, model, COALESCE(embedding_input_hash, ''), created_at, COALESCE(updated_at, created_at)
		 FROM embeddings
		 WHERE model = $1 AND embedding_input_hash = $2
		 ORDER BY updated_at DESC NULLS LAST, created_at DESC
		 LIMIT 1`, model, inputHash).
		Scan(&record.QuestionID, &record.Model, &record.InputHash, &record.CreatedAt, &record.UpdatedAt)
	if err == sql.ErrNoRows {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("embeddings: find reusable: %w", err)
	}
	return record, true, nil
}

func (r *repository) Copy(ctx context.Context, questionID, sourceQuestionID, model, inputHash string) error {
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO embeddings (question_id, embedding, model, embedding_input_hash, created_at, updated_at)
		 SELECT $1, embedding, $3, $4, now(), now()
		 FROM embeddings
		 WHERE question_id = $2 AND model = $3
		 ON CONFLICT (question_id) DO UPDATE SET
			embedding = EXCLUDED.embedding,
			model = EXCLUDED.model,
			embedding_input_hash = EXCLUDED.embedding_input_hash,
			updated_at = now()`, questionID, sourceQuestionID, model, inputHash)
	if err != nil {
		return fmt.Errorf("embeddings: copy: %w", err)
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return fmt.Errorf("embeddings: source embedding not found")
	}
	return nil
}

func (r *repository) Upsert(ctx context.Context, questionID string, vector []float32, model, inputHash string) error {
	if len(vector) != DefaultDimensions {
		return fmt.Errorf("embeddings: vector dimension %d, want %d", len(vector), DefaultDimensions)
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO embeddings (question_id, embedding, model, embedding_input_hash, created_at, updated_at)
		 VALUES ($1, $2::vector, $3, $4, now(), now())
		 ON CONFLICT (question_id) DO UPDATE SET
			embedding = EXCLUDED.embedding,
			model = EXCLUDED.model,
			embedding_input_hash = EXCLUDED.embedding_input_hash,
			updated_at = now()`, questionID, vectorLiteral(vector), model, inputHash)
	if err != nil {
		return fmt.Errorf("embeddings: upsert: %w", err)
	}
	return nil
}

func vectorLiteral(vector []float32) string {
	values := make([]string, len(vector))
	for i, value := range vector {
		values[i] = strconv.FormatFloat(float64(value), 'g', -1, 32)
	}
	return "[" + strings.Join(values, ",") + "]"
}
