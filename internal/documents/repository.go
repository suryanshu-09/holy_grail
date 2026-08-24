package documents

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
)

// Filter narrows the documents returned by List.
type Filter struct {
	Subject string
	Year    *int
	Status  string
	Limit   int
	Offset  int
}

// repository is the SQL implementation of documents.Repository.
type repository struct {
	db *sql.DB
}

// NewRepository creates a documents repository.
func NewRepository(db *sql.DB) Repository {
	return &repository{db: db}
}

const documentColumns = `id, filename, original_filename, storage_path, subject, year, status, created_at, updated_at`

// List returns documents matching the filter.
func (r *repository) List(ctx context.Context, f Filter) ([]Document, error) {
	query := strings.Builder{}
	query.WriteString("SELECT " + documentColumns + " FROM documents WHERE 1=1")

	var args []interface{}
	if f.Subject != "" {
		args = append(args, f.Subject)
		query.WriteString(fmt.Sprintf(" AND subject = $%d", len(args)))
	}
	if f.Year != nil {
		args = append(args, *f.Year)
		query.WriteString(fmt.Sprintf(" AND year = $%d", len(args)))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		query.WriteString(fmt.Sprintf(" AND status = $%d", len(args)))
	}

	args = append(args, f.Limit)
	query.WriteString(fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", len(args), len(args)+1))
	args = append(args, f.Offset)

	rows, err := r.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("documents: list: %w", err)
	}
	defer rows.Close()

	docs := make([]Document, 0)
	for rows.Next() {
		var d Document
		if err := rows.Scan(&d.ID, &d.Filename, &d.OriginalFilename, &d.StoragePath,
			&d.Subject, &d.Year, &d.Status, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, fmt.Errorf("documents: scan: %w", err)
		}
		docs = append(docs, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("documents: rows: %w", err)
	}
	return docs, nil
}

// Create inserts a new document row and populates its timestamps.
func (r *repository) Create(ctx context.Context, d *Document) error {
	const query = `INSERT INTO documents (id, filename, original_filename, storage_path, subject, year, status)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING created_at, updated_at`
	err := r.db.QueryRowContext(ctx, query,
		d.ID, d.Filename, d.OriginalFilename, d.StoragePath, d.Subject, d.Year, d.Status).
		Scan(&d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return fmt.Errorf("documents: create: %w", err)
	}
	return nil
}

// UpdateStatus sets the status column of one document. The updated_at trigger
// refreshes the timestamp. It returns apperr.ErrNotFound when the id is unknown.
func (r *repository) UpdateStatus(ctx context.Context, id string, status string) error {
	res, err := r.db.ExecContext(ctx, "UPDATE documents SET status = $1 WHERE id = $2", status, id)
	if err != nil {
		return fmt.Errorf("documents: update status: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

// GetByID returns a single document by ID or apperr.ErrNotFound.
func (r *repository) GetByID(ctx context.Context, id string) (Document, error) {
	var d Document
	err := r.db.QueryRowContext(ctx,
		"SELECT "+documentColumns+" FROM documents WHERE id = $1", id).
		Scan(&d.ID, &d.Filename, &d.OriginalFilename, &d.StoragePath,
			&d.Subject, &d.Year, &d.Status, &d.CreatedAt, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		return Document{}, apperr.ErrNotFound
	}
	if err != nil {
		return Document{}, fmt.Errorf("documents: get by id: %w", err)
	}
	return d, nil
}
