package topics

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
)

// Filter narrows the topics returned by List.
type Filter struct {
	Subject string
	Limit   int
	Offset  int
}

// repository is the SQL implementation of topics.Repository.
type repository struct {
	db *sql.DB
}

// NewRepository creates a topics repository.
func NewRepository(db *sql.DB) Repository {
	return &repository{db: db}
}

const topicColumns = `id, name, subject, created_at`

// List returns topics matching the filter.
func (r *repository) List(ctx context.Context, f Filter) ([]Topic, error) {
	query := strings.Builder{}
	query.WriteString("SELECT " + topicColumns + " FROM topics WHERE 1=1")

	var args []interface{}
	if f.Subject != "" {
		args = append(args, f.Subject)
		query.WriteString(fmt.Sprintf(" AND subject = $%d", len(args)))
	}

	args = append(args, f.Limit)
	query.WriteString(fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", len(args), len(args)+1))
	args = append(args, f.Offset)

	rows, err := r.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("topics: list: %w", err)
	}
	defer rows.Close()

	ts := make([]Topic, 0)
	for rows.Next() {
		var t Topic
		if err := rows.Scan(&t.ID, &t.Name, &t.Subject, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("topics: scan: %w", err)
		}
		ts = append(ts, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("topics: rows: %w", err)
	}
	return ts, nil
}

// GetByID returns a single topic by ID or apperr.ErrNotFound.
func (r *repository) GetByID(ctx context.Context, id string) (Topic, error) {
	var t Topic
	err := r.db.QueryRowContext(ctx,
		"SELECT "+topicColumns+" FROM topics WHERE id = $1", id).
		Scan(&t.ID, &t.Name, &t.Subject, &t.CreatedAt)
	if err == sql.ErrNoRows {
		return Topic{}, apperr.ErrNotFound
	}
	if err != nil {
		return Topic{}, fmt.Errorf("topics: get by id: %w", err)
	}
	return t, nil
}
