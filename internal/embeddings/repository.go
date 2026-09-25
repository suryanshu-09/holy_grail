package embeddings

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/lib/pq"
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

// UpsertItem is one row of a batched embedding upsert.
type UpsertItem struct {
	QuestionID string
	Vector     []float32
	Model      string
	InputHash  string
}

// BatchRepository is the optional batch contract implemented by *repository.
// It exists alongside Repository (which is unchanged) so existing callers and
// mocks keep working. Use a type assertion to opt into batched operations.
type BatchRepository interface {
	BatchUpsert(ctx context.Context, items []UpsertItem) error
	FindReusableBatch(ctx context.Context, model string, inputHashes []string) (map[string]Record, error)
}

// Compile-time check that the SQL repository supports batched operations.
var _ BatchRepository = (*repository)(nil)

// BatchUpsert persists many embeddings with a single multi-row INSERT ...
// ON CONFLICT statement instead of one round-trip per question.
func (r *repository) BatchUpsert(ctx context.Context, items []UpsertItem) error {
	if len(items) == 0 {
		return nil
	}
	query, args, err := buildBatchUpsertQuery(items)
	if err != nil {
		return err
	}
	if _, err := r.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("embeddings: batch upsert: %w", err)
	}
	return nil
}

// buildBatchUpsertQuery renders a single multi-row upsert. It is a pure
// helper so the placeholder arithmetic is unit-testable without a database.
func buildBatchUpsertQuery(items []UpsertItem) (string, []interface{}, error) {
	var sb strings.Builder
	sb.WriteString(`INSERT INTO embeddings (question_id, embedding, model, embedding_input_hash, created_at, updated_at) VALUES `)
	args := make([]interface{}, 0, len(items)*4)
	for i, item := range items {
		if len(item.Vector) != DefaultDimensions {
			return "", nil, fmt.Errorf("embeddings: vector dimension %d for question %s, want %d", len(item.Vector), item.QuestionID, DefaultDimensions)
		}
		if i > 0 {
			sb.WriteString(", ")
		}
		base := len(args) + 1
		fmt.Fprintf(&sb, "($%d, $%d::vector, $%d, $%d, now(), now())", base, base+1, base+2, base+3)
		args = append(args, item.QuestionID, vectorLiteral(item.Vector), item.Model, item.InputHash)
	}
	sb.WriteString(` ON CONFLICT (question_id) DO UPDATE SET
		embedding = EXCLUDED.embedding,
		model = EXCLUDED.model,
		embedding_input_hash = EXCLUDED.embedding_input_hash,
		updated_at = now()`)
	return sb.String(), args, nil
}

// FindReusableBatch looks up reusable vectors for many input hashes with a
// single query (ANY($2)) instead of one query per hash. The result maps each
// matched hash to its most recently updated record; unmatched hashes are
// absent from the map.
func (r *repository) FindReusableBatch(ctx context.Context, model string, inputHashes []string) (map[string]Record, error) {
	out := make(map[string]Record, len(inputHashes))
	if len(inputHashes) == 0 {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT DISTINCT ON (embedding_input_hash) question_id, model, COALESCE(embedding_input_hash, ''), created_at, COALESCE(updated_at, created_at)
		 FROM embeddings
		 WHERE model = $1 AND embedding_input_hash = ANY($2)
		 ORDER BY embedding_input_hash, updated_at DESC NULLS LAST, created_at DESC`, model, pq.Array(inputHashes))
	if err != nil {
		return nil, fmt.Errorf("embeddings: find reusable batch: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var record Record
		if err := rows.Scan(&record.QuestionID, &record.Model, &record.InputHash, &record.CreatedAt, &record.UpdatedAt); err != nil {
			return nil, fmt.Errorf("embeddings: scan reusable batch: %w", err)
		}
		out[record.InputHash] = record
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("embeddings: rows reusable batch: %w", err)
	}
	return out, nil
}

func vectorLiteral(vector []float32) string {
	values := make([]string, len(vector))
	for i, value := range vector {
		values[i] = strconv.FormatFloat(float64(value), 'g', -1, 32)
	}
	return "[" + strings.Join(values, ",") + "]"
}
