package topics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

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

// TopicWithCount pairs a topic with its associated question count.
type TopicWithCount struct {
	Topic
	QuestionCount int `json:"question_count"`
}

// QuestionTopic mirrors the question_topics join table with metadata.
type QuestionTopic struct {
	QuestionID string    `json:"question_id"`
	TopicID    string    `json:"topic_id"`
	Confidence *float64  `json:"confidence"`
	CreatedAt  time.Time `json:"created_at"`
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

// GetByName returns a topic by normalized name and optional subject.
// Name comparison is case-insensitive (LOWER). Returns apperr.ErrNotFound if absent.
func (r *repository) GetByName(ctx context.Context, name string, subject *string) (Topic, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Topic{}, apperr.ErrNotFound
	}
	var t Topic
	var err error
	if subject != nil && *subject != "" {
		err = r.db.QueryRowContext(ctx,
			"SELECT "+topicColumns+" FROM topics WHERE LOWER(name) = LOWER($1) AND COALESCE(subject,'') = $2 LIMIT 1", name, *subject).
			Scan(&t.ID, &t.Name, &t.Subject, &t.CreatedAt)
	} else {
		err = r.db.QueryRowContext(ctx,
			"SELECT "+topicColumns+" FROM topics WHERE LOWER(name) = LOWER($1) AND subject IS NULL LIMIT 1", name).
			Scan(&t.ID, &t.Name, &t.Subject, &t.CreatedAt)
		if err == sql.ErrNoRows {
			// Fallback: topics with empty subject might be stored as '' or NULL.
			// Try coalesced match for NULL-subject callers.
			err = r.db.QueryRowContext(ctx,
				"SELECT "+topicColumns+" FROM topics WHERE LOWER(name) = LOWER($1) AND COALESCE(subject,'') = '' LIMIT 1", name).
				Scan(&t.ID, &t.Name, &t.Subject, &t.CreatedAt)
		}
	}
	if err == sql.ErrNoRows {
		return Topic{}, apperr.ErrNotFound
	}
	if err != nil {
		return Topic{}, fmt.Errorf("topics: get by name: %w", err)
	}
	return t, nil
}

// Create inserts a new topic and returns it. Handles race via ON CONFLICT fallback.
func (r *repository) Create(ctx context.Context, name string, subject *string) (Topic, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Topic{}, fmt.Errorf("topics: create: name is required")
	}
	// Try insert with ON CONFLICT DO NOTHING to be idempotent under race.
	var t Topic
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO topics (id, name, subject) VALUES (gen_random_uuid(), $1, $2)
		 ON CONFLICT DO NOTHING
		 RETURNING `+topicColumns, name, subject).
		Scan(&t.ID, &t.Name, &t.Subject, &t.CreatedAt)
	if err == nil {
		return t, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		// Check for unique violation without RETURNING (when ON CONFLICT DO NOTHING yields no row)
		// sql.ErrNoRows indicates conflict; fall through to select existing.
		return Topic{}, fmt.Errorf("topics: create: %w", err)
	}
	// Row already existed — fetch existing.
	existing, getErr := r.GetByName(ctx, name, subject)
	if getErr != nil {
		return Topic{}, fmt.Errorf("topics: create conflict fetch: %w", getErr)
	}
	return existing, nil
}

// FindOrCreate returns the existing topic matching name+subject or creates it.
// It is safe for concurrent callers (handles unique violation races).
func (r *repository) FindOrCreate(ctx context.Context, name string, subject *string) (Topic, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Topic{}, fmt.Errorf("topics: find or create: name is required")
	}
	t, err := r.GetByName(ctx, name, subject)
	if err == nil {
		return t, nil
	}
	if !errors.Is(err, apperr.ErrNotFound) {
		return Topic{}, err
	}
	// Not found — try to create. If race creates it concurrently, Create will return existing.
	return r.Create(ctx, name, subject)
}

// ListWithCounts returns topics with question counts via JOIN question_topics GROUP BY.
// Supports filtering by subject, ordering by question_count DESC, and pagination.
func (r *repository) ListWithCounts(ctx context.Context, f Filter) ([]TopicWithCount, error) {
	query := strings.Builder{}
	query.WriteString(`
		SELECT t.id, t.name, t.subject, t.created_at, COUNT(qt.question_id) AS question_count
		FROM topics t
		LEFT JOIN question_topics qt ON qt.topic_id = t.id
		WHERE 1=1`)

	var args []interface{}
	if f.Subject != "" {
		args = append(args, f.Subject)
		query.WriteString(fmt.Sprintf(" AND t.subject = $%d", len(args)))
	}

	query.WriteString(" GROUP BY t.id, t.name, t.subject, t.created_at")
	query.WriteString(" ORDER BY question_count DESC, t.created_at DESC")

	args = append(args, f.Limit)
	query.WriteString(fmt.Sprintf(" LIMIT $%d", len(args)))
	args = append(args, f.Offset)
	query.WriteString(fmt.Sprintf(" OFFSET $%d", len(args)))

	rows, err := r.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("topics: list with counts: %w", err)
	}
	defer rows.Close()

	out := make([]TopicWithCount, 0)
	for rows.Next() {
		var tc TopicWithCount
		if err := rows.Scan(&tc.ID, &tc.Name, &tc.Subject, &tc.CreatedAt, &tc.QuestionCount); err != nil {
			return nil, fmt.Errorf("topics: scan with counts: %w", err)
		}
		out = append(out, tc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("topics: rows with counts: %w", err)
	}
	return out, nil
}

// MergeTopics reassigns all question_topics from source to target and deletes source.
// Duplicate question_topics (same question already linked to target) are de-duplicated.
// The operation is transaction-safe.
func (r *repository) MergeTopics(ctx context.Context, sourceID, targetID string) error {
	if sourceID == "" || targetID == "" {
		return fmt.Errorf("topics: merge: source and target IDs required")
	}
	if sourceID == targetID {
		return fmt.Errorf("topics: merge: source and target must differ")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("topics: merge begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Verify both topics exist.
	var cnt int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM topics WHERE id = $1", targetID).Scan(&cnt); err != nil {
		return fmt.Errorf("topics: merge check target: %w", err)
	}
	if cnt == 0 {
		err = apperr.ErrNotFound
		return err
	}
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM topics WHERE id = $1", sourceID).Scan(&cnt); err != nil {
		return fmt.Errorf("topics: merge check source: %w", err)
	}
	if cnt == 0 {
		err = apperr.ErrNotFound
		return err
	}

	// Reassign: insert missing associations, keeping highest confidence if duplicate.
	_, err = tx.ExecContext(ctx, `
		INSERT INTO question_topics (question_id, topic_id, confidence, created_at)
		SELECT question_id, $2, confidence, COALESCE(created_at, now())
		FROM question_topics WHERE topic_id = $1
		ON CONFLICT (question_id, topic_id) DO NOTHING`, sourceID, targetID)
	if err != nil {
		return fmt.Errorf("topics: merge reassign: %w", err)
	}

	// Optionally preserve confidence on conflict by keeping max confidence.
	// If conflict occurred, update confidence to greatest value.
	_, err = tx.ExecContext(ctx, `
		UPDATE question_topics dest
		SET confidence = GREATEST(COALESCE(dest.confidence, 0), COALESCE(src.confidence, 0))
		FROM question_topics src
		WHERE dest.topic_id = $2 AND src.topic_id = $1 AND dest.question_id = src.question_id
		  AND COALESCE(src.confidence, 0) > COALESCE(dest.confidence, 0)`, sourceID, targetID)
	if err != nil {
		return fmt.Errorf("topics: merge confidence: %w", err)
	}

	// Delete source associations (remaining duplicates already handled)
	if _, err = tx.ExecContext(ctx, "DELETE FROM question_topics WHERE topic_id = $1", sourceID); err != nil {
		return fmt.Errorf("topics: merge delete associations: %w", err)
	}

	// Delete source topic
	if _, err = tx.ExecContext(ctx, "DELETE FROM topics WHERE id = $1", sourceID); err != nil {
		return fmt.Errorf("topics: merge delete topic: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("topics: merge commit: %w", err)
	}
	return nil
}

// AddQuestionTopic links a question to a topic with optional confidence.
// Uses ON CONFLICT to remain idempotent and updates confidence if higher.
func (r *repository) AddQuestionTopic(ctx context.Context, questionID, topicID string, confidence *float64) error {
	if questionID == "" || topicID == "" {
		return fmt.Errorf("topics: add question topic: IDs required")
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO question_topics (question_id, topic_id, confidence, created_at)
		 VALUES ($1, $2, $3, now())
		 ON CONFLICT (question_id, topic_id) DO UPDATE SET confidence = COALESCE(EXCLUDED.confidence, question_topics.confidence), created_at = question_topics.created_at`,
		questionID, topicID, confidence)
	if err != nil {
		return fmt.Errorf("topics: add question topic: %w", err)
	}
	return nil
}

// RemoveQuestionTopic unlinks a question from a topic. Idempotent.
func (r *repository) RemoveQuestionTopic(ctx context.Context, questionID, topicID string) error {
	if questionID == "" || topicID == "" {
		return fmt.Errorf("topics: remove question topic: IDs required")
	}
	_, err := r.db.ExecContext(ctx,
		"DELETE FROM question_topics WHERE question_id = $1 AND topic_id = $2", questionID, topicID)
	if err != nil {
		return fmt.Errorf("topics: remove question topic: %w", err)
	}
	return nil
}

// ListTopicsForQuestion returns all topics associated with a question.
func (r *repository) ListTopicsForQuestion(ctx context.Context, questionID string) ([]Topic, error) {
	if questionID == "" {
		return nil, fmt.Errorf("topics: list for question: questionID required")
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+topicColumns+` FROM topics t
		 JOIN question_topics qt ON qt.topic_id = t.id
		 WHERE qt.question_id = $1
		 ORDER BY t.name ASC`, questionID)
	if err != nil {
		return nil, fmt.Errorf("topics: list for question: %w", err)
	}
	defer rows.Close()

	ts := make([]Topic, 0)
	for rows.Next() {
		var t Topic
		if err := rows.Scan(&t.ID, &t.Name, &t.Subject, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("topics: scan for question: %w", err)
		}
		ts = append(ts, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("topics: rows for question: %w", err)
	}
	return ts, nil
}

// ListQuestionTopics returns join entries for a question with confidence metadata.
func (r *repository) ListQuestionTopics(ctx context.Context, questionID string) ([]QuestionTopic, error) {
	if questionID == "" {
		return nil, fmt.Errorf("topics: list question topics: questionID required")
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT question_id, topic_id, confidence, created_at FROM question_topics WHERE question_id = $1 ORDER BY created_at ASC`, questionID)
	if err != nil {
		return nil, fmt.Errorf("topics: list question topics: %w", err)
	}
	defer rows.Close()

	out := make([]QuestionTopic, 0)
	for rows.Next() {
		var qt QuestionTopic
		if err := rows.Scan(&qt.QuestionID, &qt.TopicID, &qt.Confidence, &qt.CreatedAt); err != nil {
			return nil, fmt.Errorf("topics: scan question_topics: %w", err)
		}
		out = append(out, qt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("topics: rows question_topics: %w", err)
	}
	return out, nil
}

// SetQuestionTopics replaces all topics for a question in a transaction.
func (r *repository) SetQuestionTopics(ctx context.Context, questionID string, topicIDs []string, confidences map[string]*float64) error {
	if questionID == "" {
		return fmt.Errorf("topics: set question topics: questionID required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("topics: set question topics begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, "DELETE FROM question_topics WHERE question_id = $1", questionID); err != nil {
		return fmt.Errorf("topics: set delete: %w", err)
	}
	for _, tid := range topicIDs {
		var conf *float64
		if confidences != nil {
			conf = confidences[tid]
		}
		if _, err = tx.ExecContext(ctx,
			`INSERT INTO question_topics (question_id, topic_id, confidence, created_at) VALUES ($1, $2, $3, now())
			 ON CONFLICT (question_id, topic_id) DO UPDATE SET confidence = EXCLUDED.confidence`, questionID, tid, conf); err != nil {
			return fmt.Errorf("topics: set insert: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("topics: set commit: %w", err)
	}
	return nil
}
