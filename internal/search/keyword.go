package search

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// KeywordRepository defines the persistence contract for Postgres full-text
// keyword search over questions.question_text.
//
// It is intentionally a small interface so callers (hybrid retrieval,
// handlers) can mock it in unit tests without a database.
type KeywordRepository interface {
	Search(ctx context.Context, query string, filter Filter) ([]Result, error)
}

// keywordRepository is the PostgreSQL FTS implementation of KeywordRepository.
//
// Ranking uses ts_rank against the questions.search_vector column
// (see migrations/006_add_hybrid_retrieval.sql) with plainto_tsquery for
// query parsing. Rows that match only via a case-insensitive substring
// (ILIKE) fallback receive a small constant score (0.05) so exact FTS
// matches always rank above pure substring matches.
type keywordRepository struct {
	db *sql.DB
}

// NewKeywordRepository creates a Postgres keyword search repository.
// db may be nil only when the caller intends to use the pure query builder
// (BuildKeywordQuery) for unit testing query construction.
func NewKeywordRepository(db *sql.DB) KeywordRepository {
	return &keywordRepository{db: db}
}

// fallbackScore is the ts_rank-equivalent score assigned to rows that match
// only via the ILIKE substring fallback (no FTS match, hence ts_rank = 0).
const fallbackScore = 0.05

// rankExpr returns the SQL expression computing the keyword relevance score.
// It references the full-text query text via the $1 placeholder, so callers
// must ensure args[0] is the trimmed query string.
func keywordRankExpr() string {
	return `GREATEST(` +
		`ts_rank(q.search_vector, plainto_tsquery('english', $1)), ` +
		`CASE WHEN COALESCE(q.question_text, '') ILIKE '%' || $1 || '%' THEN ` + fmt.Sprintf("%g", fallbackScore) + ` ELSE 0 END` +
		`)`
}

// ftsWhereExpr matches rows via FTS or the ILIKE substring fallback.
func keywordMatchExpr() string {
	return `(q.search_vector @@ plainto_tsquery('english', $1) OR COALESCE(q.question_text, '') ILIKE '%' || $1 || '%')`
}

// BuildKeywordQuery constructs the parameterized keyword search SQL and its
// args from a query string and Filter. It is a pure function (no DB access)
// so it is directly unit-testable. args[0] is always the trimmed query text.
//
// Returned SQL selects QuestionColumns plus a "rank" column (ts_rank with
// ILIKE fallback) and orders by rank DESC for top-K retrieval.
func BuildKeywordQuery(query string, filter Filter) (string, []interface{}) {
	clamp(&filter)
	trimmed := strings.TrimSpace(query)
	args := []interface{}{trimmed}
	argIdx := 2 // $1 is the query text

	var whereClauses []string
	whereClauses = append(whereClauses, keywordMatchExpr())
	whereClauses = append(whereClauses, keywordMetadataClauses(&filter, &args, &argIdx)...)

	rank := keywordRankExpr()
	if filter.Threshold != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("%s >= $%d", rank, argIdx))
		args = append(args, *filter.Threshold)
		argIdx++
	}

	whereSQL := "WHERE " + strings.Join(whereClauses, " AND ")

	limitIdx := argIdx
	offsetIdx := argIdx + 1
	args = append(args, filter.Limit, filter.Offset)

	sql := fmt.Sprintf(`
		SELECT %s, %s AS rank
		FROM questions q
		%s
		ORDER BY rank DESC, q.created_at DESC
		LIMIT $%d OFFSET $%d
	`, QuestionColumns, rank, whereSQL, limitIdx, offsetIdx)

	return sql, args
}

// keywordMetadataClauses reuses the same Filter semantics as the vector
// search repository (subject/year/year_min/year_max/document/topic/topic_id/
// question_type/difficulty via EXISTS subqueries to avoid row duplication).
// It appends filter values to *args and advances *nextIdx, returning the SQL
// predicates to AND into the WHERE clause.
func keywordMetadataClauses(filter *Filter, args *[]interface{}, nextIdx *int) []string {
	var clauses []string
	add := func(clause string, value interface{}) {
		clauses = append(clauses, fmt.Sprintf(clause, *nextIdx))
		*args = append(*args, value)
		*nextIdx++
	}

	if strings.TrimSpace(filter.Subject) != "" {
		add("q.subject = $%d", strings.TrimSpace(filter.Subject))
	}
	if filter.Year != nil {
		add("q.year = $%d", *filter.Year)
	}
	if filter.YearMin != nil {
		add("q.year >= $%d", *filter.YearMin)
	}
	if filter.YearMax != nil {
		add("q.year <= $%d", *filter.YearMax)
	}
	if strings.TrimSpace(filter.DocumentID) != "" {
		add("q.document_id = $%d", strings.TrimSpace(filter.DocumentID))
	}
	if strings.TrimSpace(filter.QuestionType) != "" {
		add("q.question_type = $%d", strings.TrimSpace(filter.QuestionType))
	}
	if strings.TrimSpace(filter.Difficulty) != "" {
		add("q.difficulty = $%d", strings.TrimSpace(filter.Difficulty))
	}
	if strings.TrimSpace(filter.TopicID) != "" {
		add("EXISTS (SELECT 1 FROM question_topics qt WHERE qt.question_id = q.id AND qt.topic_id = $%d)", strings.TrimSpace(filter.TopicID))
	}
	if strings.TrimSpace(filter.Topic) != "" {
		add("EXISTS (SELECT 1 FROM question_topics qt JOIN topics t ON t.id = qt.topic_id WHERE qt.question_id = q.id AND LOWER(t.name) = LOWER($%d))", strings.TrimSpace(filter.Topic))
	}
	return clauses
}

// Search executes a Postgres FTS keyword search with metadata filtering.
// Empty queries and out-of-range thresholds are rejected. Results are ordered
// by rank DESC (ts_rank with ILIKE fallback) and paginated via LIMIT/OFFSET.
// Result.Similarity carries the ts_rank score; Result.Distance is 1 - rank
// clamped at 0 to stay compatible with the vector search Result shape.
func (r *keywordRepository) Search(ctx context.Context, query string, filter Filter) ([]Result, error) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return nil, fmt.Errorf("keyword search: query is required")
	}
	if r.db == nil {
		return nil, fmt.Errorf("keyword search: database is required")
	}
	clamp(&filter)
	if filter.Threshold != nil {
		if *filter.Threshold < 0 || *filter.Threshold > 1 {
			return nil, fmt.Errorf("keyword search: threshold must be between 0 and 1")
		}
	}

	sqlQuery, args := BuildKeywordQuery(trimmed, filter)

	rows, err := r.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("keyword search: query: %w", err)
	}
	defer rows.Close()

	results := make([]Result, 0)
	for rows.Next() {
		var res Result
		q := &res.Question
		var rank float64
		if err := rows.Scan(
			&q.ID, &q.DocumentID, &q.QuestionNumber, &q.QuestionText,
			&q.PageNumber, &q.StartPage, &q.EndPage, &q.StartOffset, &q.EndOffset,
			&q.Confidence, &q.QuestionType, &q.OptionsJSON, &q.ExtractionNotesJSON, &q.ImagesJSON,
			&q.Year, &q.Subject, &q.Difficulty, &q.CreatedAt, &q.UpdatedAt,
			&rank,
		); err != nil {
			return nil, fmt.Errorf("keyword search: scan: %w", err)
		}
		res.Similarity = rank
		res.Distance = 1.0 - rank
		if res.Distance < 0 {
			res.Distance = 0
		}
		results = append(results, res)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("keyword search: rows: %w", err)
	}
	return results, nil
}
