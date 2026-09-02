package questions

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
)

// QuestionTopic mirrors the question_topics join table metadata.
type QuestionTopic struct {
	QuestionID string    `json:"question_id"`
	TopicID    string    `json:"topic_id"`
	Confidence *float64  `json:"confidence"`
	CreatedAt  time.Time `json:"created_at"`
}

// QuestionTopicInfo is a topic associated with a question, including metadata.
type QuestionTopicInfo struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Subject    *string   `json:"subject"`
	Confidence *float64  `json:"confidence"`
	CreatedAt  time.Time `json:"created_at"`
}

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

const questionColumns = `q.id, q.document_id, q.question_number, q.question_text, q.page_number, q.start_page, q.end_page, q.start_offset, q.end_offset, q.confidence, q.question_type, q.options_json, q.extraction_notes_json, q.images_json, q.year, q.subject, q.difficulty, q.created_at, q.updated_at`

// Insert persists a new question into the database. If q.ID is provided (non-empty)
// it is used; otherwise the database generates one via gen_random_uuid().
func (r *repository) Insert(ctx context.Context, q Question) error {
	if q.ID != "" {
		_, err := r.db.ExecContext(ctx,
			`INSERT INTO questions (id, document_id, question_number, question_text, page_number, start_page, end_page, start_offset, end_offset, confidence, question_type, options_json, extraction_notes_json, images_json, year, subject, difficulty)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`,
			q.ID, q.DocumentID, q.QuestionNumber, q.QuestionText, q.PageNumber, q.StartPage, q.EndPage, q.StartOffset, q.EndOffset, q.Confidence, q.QuestionType, q.OptionsJSON, q.ExtractionNotesJSON, q.ImagesJSON, q.Year, q.Subject, q.Difficulty)
		if err != nil {
			return fmt.Errorf("questions: insert: %w", err)
		}
		return nil
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO questions (id, document_id, question_number, question_text, page_number, start_page, end_page, start_offset, end_offset, confidence, question_type, options_json, extraction_notes_json, images_json, year, subject, difficulty)
		 VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
		q.DocumentID, q.QuestionNumber, q.QuestionText, q.PageNumber, q.StartPage, q.EndPage, q.StartOffset, q.EndOffset, q.Confidence, q.QuestionType, q.OptionsJSON, q.ExtractionNotesJSON, q.ImagesJSON, q.Year, q.Subject, q.Difficulty)
	if err != nil {
		return fmt.Errorf("questions: insert: %w", err)
	}
	return nil
}

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
			&q.PageNumber, &q.StartPage, &q.EndPage, &q.StartOffset, &q.EndOffset, &q.Confidence, &q.QuestionType, &q.OptionsJSON, &q.ExtractionNotesJSON, &q.ImagesJSON, &q.Year, &q.Subject, &q.Difficulty, &q.CreatedAt, &q.UpdatedAt); err != nil {
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
			&q.PageNumber, &q.StartPage, &q.EndPage, &q.StartOffset, &q.EndOffset, &q.Confidence, &q.QuestionType, &q.OptionsJSON, &q.ExtractionNotesJSON, &q.ImagesJSON, &q.Year, &q.Subject, &q.Difficulty, &q.CreatedAt, &q.UpdatedAt)
	if err == sql.ErrNoRows {
		return Question{}, apperr.ErrNotFound
	}
	if err != nil {
		return Question{}, fmt.Errorf("questions: get by id: %w", err)
	}
	return q, nil
}

// AddTopic associates a question with a topic, optionally storing LLM confidence.
// It is idempotent via ON CONFLICT and preserves higher confidence.
func (r *repository) AddTopic(ctx context.Context, questionID, topicID string, confidence *float64) error {
	if questionID == "" || topicID == "" {
		return fmt.Errorf("questions: add topic: IDs required")
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO question_topics (question_id, topic_id, confidence, created_at)
		 VALUES ($1, $2, $3, now())
		 ON CONFLICT (question_id, topic_id) DO UPDATE SET confidence = COALESCE(EXCLUDED.confidence, question_topics.confidence)`, questionID, topicID, confidence)
	if err != nil {
		return fmt.Errorf("questions: add topic: %w", err)
	}
	return nil
}

// AddQuestionTopic is an alias for AddTopic (for cross-package ergonomics).
func (r *repository) AddQuestionTopic(ctx context.Context, questionID, topicID string, confidence *float64) error {
	return r.AddTopic(ctx, questionID, topicID, confidence)
}

// RemoveTopic dissociates a question from a topic.
func (r *repository) RemoveTopic(ctx context.Context, questionID, topicID string) error {
	if questionID == "" || topicID == "" {
		return fmt.Errorf("questions: remove topic: IDs required")
	}
	_, err := r.db.ExecContext(ctx, "DELETE FROM question_topics WHERE question_id = $1 AND topic_id = $2", questionID, topicID)
	if err != nil {
		return fmt.Errorf("questions: remove topic: %w", err)
	}
	return nil
}

// RemoveQuestionTopic alias.
func (r *repository) RemoveQuestionTopic(ctx context.Context, questionID, topicID string) error {
	return r.RemoveTopic(ctx, questionID, topicID)
}

// ListTopics returns topics associated with a question, including confidence.
func (r *repository) ListTopics(ctx context.Context, questionID string) ([]QuestionTopicInfo, error) {
	if questionID == "" {
		return nil, fmt.Errorf("questions: list topics: questionID required")
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT t.id, t.name, t.subject, qt.confidence, qt.created_at
		 FROM topics t
		 JOIN question_topics qt ON qt.topic_id = t.id
		 WHERE qt.question_id = $1
		 ORDER BY t.name ASC`, questionID)
	if err != nil {
		return nil, fmt.Errorf("questions: list topics: %w", err)
	}
	defer rows.Close()

	out := make([]QuestionTopicInfo, 0)
	for rows.Next() {
		var ti QuestionTopicInfo
		if err := rows.Scan(&ti.ID, &ti.Name, &ti.Subject, &ti.Confidence, &ti.CreatedAt); err != nil {
			return nil, fmt.Errorf("questions: scan topics: %w", err)
		}
		out = append(out, ti)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("questions: rows topics: %w", err)
	}
	return out, nil
}

// ListTopicsForQuestion alias for ListTopics.
func (r *repository) ListTopicsForQuestion(ctx context.Context, questionID string) ([]QuestionTopicInfo, error) {
	return r.ListTopics(ctx, questionID)
}

// ListQuestionTopics returns raw join rows for a question.
func (r *repository) ListQuestionTopics(ctx context.Context, questionID string) ([]QuestionTopic, error) {
	if questionID == "" {
		return nil, fmt.Errorf("questions: list question topics: questionID required")
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT question_id, topic_id, confidence, created_at FROM question_topics WHERE question_id = $1 ORDER BY created_at ASC`, questionID)
	if err != nil {
		return nil, fmt.Errorf("questions: list question topics: %w", err)
	}
	defer rows.Close()

	out := make([]QuestionTopic, 0)
	for rows.Next() {
		var qt QuestionTopic
		if err := rows.Scan(&qt.QuestionID, &qt.TopicID, &qt.Confidence, &qt.CreatedAt); err != nil {
			return nil, fmt.Errorf("questions: scan question_topics: %w", err)
		}
		out = append(out, qt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("questions: rows question_topics: %w", err)
	}
	return out, nil
}

// SetQuestionTopics replaces all topics for a question transactionally.
func (r *repository) SetQuestionTopics(ctx context.Context, questionID string, topicIDs []string, confidences map[string]*float64) error {
	if questionID == "" {
		return fmt.Errorf("questions: set question topics: questionID required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("questions: set question topics begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, "DELETE FROM question_topics WHERE question_id = $1", questionID); err != nil {
		return fmt.Errorf("questions: set delete: %w", err)
	}
	for _, tid := range topicIDs {
		var conf *float64
		if confidences != nil {
			conf = confidences[tid]
		}
		if _, err = tx.ExecContext(ctx,
			`INSERT INTO question_topics (question_id, topic_id, confidence, created_at) VALUES ($1, $2, $3, now())
			 ON CONFLICT (question_id, topic_id) DO UPDATE SET confidence = EXCLUDED.confidence`, questionID, tid, conf); err != nil {
			return fmt.Errorf("questions: set insert: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("questions: set commit: %w", err)
	}
	return nil
}

// GetQuestionTopicsForTopic returns all question ids for a topic (with confidence).
func (r *repository) GetQuestionTopicsForTopic(ctx context.Context, topicID string) ([]QuestionTopic, error) {
	if topicID == "" {
		return nil, fmt.Errorf("questions: get for topic: topicID required")
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT question_id, topic_id, confidence, created_at FROM question_topics WHERE topic_id = $1 ORDER BY created_at DESC`, topicID)
	if err != nil {
		return nil, fmt.Errorf("questions: get for topic: %w", err)
	}
	defer rows.Close()

	out := make([]QuestionTopic, 0)
	for rows.Next() {
		var qt QuestionTopic
		if err := rows.Scan(&qt.QuestionID, &qt.TopicID, &qt.Confidence, &qt.CreatedAt); err != nil {
			return nil, fmt.Errorf("questions: scan for topic: %w", err)
		}
		out = append(out, qt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("questions: rows for topic: %w", err)
	}
	return out, nil
}
