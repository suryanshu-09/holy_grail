package questions

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
)

// Filter narrows the questions returned by List.
type Filter struct {
	DocumentID string
	TopicID    string
	Year       *int
	Subject    string
	Limit      int
	Offset     int
}

// repository is the SQL implementation of questions.Repository.
type repository struct {
	db *sql.DB
}

// NewRepository creates a questions repository.
func NewRepository(db *sql.DB) Repository {
	return &repository{db: db}
}

const questionColumns = `q.id, q.document_id, q.question_number, q.question_text, q.page_number, q.year, q.subject, q.difficulty, q.created_at, q.updated_at`

// List returns questions matching the filter. TopicID filters through the
// question_topics join table.
func (r *repository) List(ctx context.Context, f Filter) ([]Question, error) {
	query := strings.Builder{}
	query.WriteString("SELECT " + questionColumns + " FROM questions q WHERE 1=1")

	var args []interface{}
	if f.DocumentID != "" {
		args = append(args, f.DocumentID)
		query.WriteString(fmt.Sprintf(" AND q.document_id = $%d", len(args)))
	}
	if f.TopicID != "" {
		query.WriteString(" AND EXISTS (SELECT 1 FROM question_topics qt WHERE qt.question_id = q.id AND qt.topic_id = $" + fmt.Sprint(len(args)+1) + ")")
		args = append(args, f.TopicID)
	}
	if f.Year != nil {
		args = append(args, *f.Year)
		query.WriteString(fmt.Sprintf(" AND q.year = $%d", len(args)))
	}
	if f.Subject != "" {
		args = append(args, f.Subject)
		query.WriteString(fmt.Sprintf(" AND q.subject = $%d", len(args)))
	}

	args = append(args, f.Limit)
	query.WriteString(fmt.Sprintf(" ORDER BY q.created_at DESC LIMIT $%d OFFSET $%d", len(args), len(args)+1))
	args = append(args, f.Offset)

	rows, err := r.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("questions: list: %w", err)
	}
	defer rows.Close()

	qs := make([]Question, 0)
	for rows.Next() {
		var q Question
		if err := rows.Scan(&q.ID, &q.DocumentID, &q.QuestionNumber, &q.QuestionText,
			&q.PageNumber, &q.Year, &q.Subject, &q.Difficulty, &q.CreatedAt, &q.UpdatedAt); err != nil {
			return nil, fmt.Errorf("questions: scan: %w", err)
		}
		qs = append(qs, q)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("questions: rows: %w", err)
	}
	return qs, nil
}

// GetByID returns a single question by ID or apperr.ErrNotFound.
func (r *repository) GetByID(ctx context.Context, id string) (Question, error) {
	var q Question
	err := r.db.QueryRowContext(ctx,
		"SELECT "+questionColumns+" FROM questions q WHERE q.id = $1", id).
		Scan(&q.ID, &q.DocumentID, &q.QuestionNumber, &q.QuestionText,
			&q.PageNumber, &q.Year, &q.Subject, &q.Difficulty, &q.CreatedAt, &q.UpdatedAt)
	if err == sql.ErrNoRows {
		return Question{}, apperr.ErrNotFound
	}
	if err != nil {
		return Question{}, fmt.Errorf("questions: get by id: %w", err)
	}
	return q, nil
}
