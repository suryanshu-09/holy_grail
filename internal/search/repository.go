package search

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/suryanshu-09/holy_grail/internal/embeddings"
)

// repository is the PostgreSQL/pgvector implementation of Repository.
type repository struct {
	db     *sql.DB
	metric Metric
	model  string
}

// NewRepository creates a pgvector search repository.
// metric selects the distance operator (<=> for cosine, <-> for L2, <#> for inner product).
// model, if non-empty, filters embeddings to a specific embedding model to avoid
// mixing incompatible vector spaces. If empty, no model filter is applied.
func NewRepository(db *sql.DB, metric Metric, model string) Repository {
	if metric == "" {
		metric = DefaultMetric
	}
	if model == "" {
		model = embeddings.DefaultModel
	}
	return &repository{db: db, metric: metric, model: model}
}

// Search executes a vector similarity search with metadata filtering.
// It computes cosine distance (or configured metric), orders by distance ASC,
// applies threshold filtering, and paginates via LIMIT/OFFSET.
func (r *repository) Search(ctx context.Context, vector []float32, filter Filter) ([]Result, error) {
	if len(vector) != embeddings.DefaultDimensions {
		return nil, fmt.Errorf("search: vector dimension %d, want %d", len(vector), embeddings.DefaultDimensions)
	}
	clamp(&filter)
	// Validate threshold if provided.
	if filter.Threshold != nil {
		if *filter.Threshold < 0 || *filter.Threshold > 1 {
			return nil, fmt.Errorf("search: threshold must be between 0 and 1")
		}
	}

	operator := r.operator()
	distanceExpr := fmt.Sprintf("e.embedding %s $1::vector", operator)
	similarityExpr := r.similarityExpr(distanceExpr)

	// Build WHERE clauses dynamically to avoid unnecessary parameters.
	// $1 is always the query vector.
	args := []interface{}{vectorLiteral(vector)}
	argIdx := 2 // next placeholder index

	var whereClauses []string

	// Model filter: ensure vectors are comparable (pinned model).
	if r.model != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("e.model = $%d", argIdx))
		args = append(args, r.model)
		argIdx++
	}

	if strings.TrimSpace(filter.Subject) != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("q.subject = $%d", argIdx))
		args = append(args, strings.TrimSpace(filter.Subject))
		argIdx++
	}
	if filter.Year != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("q.year = $%d", argIdx))
		args = append(args, *filter.Year)
		argIdx++
	}
	if filter.YearMin != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("q.year >= $%d", argIdx))
		args = append(args, *filter.YearMin)
		argIdx++
	}
	if filter.YearMax != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("q.year <= $%d", argIdx))
		args = append(args, *filter.YearMax)
		argIdx++
	}
	if strings.TrimSpace(filter.DocumentID) != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("q.document_id = $%d", argIdx))
		args = append(args, strings.TrimSpace(filter.DocumentID))
		argIdx++
	}
	if strings.TrimSpace(filter.QuestionType) != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("q.question_type = $%d", argIdx))
		args = append(args, strings.TrimSpace(filter.QuestionType))
		argIdx++
	}
	if strings.TrimSpace(filter.Difficulty) != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("q.difficulty = $%d", argIdx))
		args = append(args, strings.TrimSpace(filter.Difficulty))
		argIdx++
	}
	// Topic filters use EXISTS to avoid row duplication from joins.
	if strings.TrimSpace(filter.TopicID) != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("EXISTS (SELECT 1 FROM question_topics qt WHERE qt.question_id = q.id AND qt.topic_id = $%d)", argIdx))
		args = append(args, strings.TrimSpace(filter.TopicID))
		argIdx++
	}
	if strings.TrimSpace(filter.Topic) != "" {
		// Case-insensitive topic name match.
		whereClauses = append(whereClauses, fmt.Sprintf("EXISTS (SELECT 1 FROM question_topics qt JOIN topics t ON t.id = qt.topic_id WHERE qt.question_id = q.id AND LOWER(t.name) = LOWER($%d))", argIdx))
		args = append(args, strings.TrimSpace(filter.Topic))
		argIdx++
	}
	// Threshold filter: distance <= maxDistance.
	// For cosine, similarity = 1 - distance, so maxDistance = 1 - threshold.
	// For L2, we map similarity threshold to maxDistance via 1/(1+distance) => distance <= (1/threshold)-1.
	// For IP, distance = 1 - inner_product, so similar to cosine (vectors assumed normalized).
	var thresholdClause string
	if filter.Threshold != nil {
		maxDist := r.thresholdToDistance(*filter.Threshold)
		whereClauses = append(whereClauses, fmt.Sprintf("%s <= $%d", distanceExpr, argIdx))
		args = append(args, maxDist)
		argIdx++
		_ = thresholdClause
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	// Limit/Offset placeholders.
	limitIdx := argIdx
	offsetIdx := argIdx + 1
	args = append(args, filter.Limit, filter.Offset)

	query := fmt.Sprintf(`
		SELECT %s, %s AS distance, %s AS similarity
		FROM embeddings e
		JOIN questions q ON q.id = e.question_id
		%s
		ORDER BY %s ASC
		LIMIT $%d OFFSET $%d
	`, QuestionColumns, distanceExpr, similarityExpr, whereSQL, distanceExpr, limitIdx, offsetIdx)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search: query: %w", err)
	}
	defer rows.Close()

	results := make([]Result, 0)
	for rows.Next() {
		var res Result
		var q = &res.Question
		var distance, similarity float64
		if err := rows.Scan(
			&q.ID, &q.DocumentID, &q.QuestionNumber, &q.QuestionText,
			&q.PageNumber, &q.StartPage, &q.EndPage, &q.StartOffset, &q.EndOffset,
			&q.Confidence, &q.QuestionType, &q.OptionsJSON, &q.ExtractionNotesJSON, &q.ImagesJSON,
			&q.Year, &q.Subject, &q.Difficulty, &q.CreatedAt, &q.UpdatedAt,
			&distance, &similarity,
		); err != nil {
			return nil, fmt.Errorf("search: scan: %w", err)
		}
		res.Distance = distance
		res.Similarity = similarity
		results = append(results, res)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search: rows: %w", err)
	}
	return results, nil
}

func (r *repository) operator() string {
	switch r.metric {
	case MetricL2:
		return "<->"
	case MetricInnerProduct:
		return "<#>"
	default:
		return "<=>"
	}
}

func (r *repository) similarityExpr(distanceExpr string) string {
	switch r.metric {
	case MetricL2:
		// Convert L2 distance to similarity in (0,1]: 1 / (1 + distance)
		return fmt.Sprintf("1.0 / (1.0 + %s)", distanceExpr)
	case MetricInnerProduct:
		// Inner product distance is 1 - (x·y); for normalized vectors this equals cosine distance.
		// Return 1 - distance as similarity.
		return fmt.Sprintf("1.0 - (%s)", distanceExpr)
	default:
		// Cosine: similarity = 1 - cosine_distance
		return fmt.Sprintf("1.0 - (%s)", distanceExpr)
	}
}

func (r *repository) thresholdToDistance(threshold float64) float64 {
	switch r.metric {
	case MetricL2:
		if threshold <= 0 {
			return 1e9 // effectively no limit, but threshold 0 means allow all
		}
		// similarity = 1/(1+distance) => distance = (1/similarity) -1
		return (1.0 / threshold) - 1.0
	case MetricInnerProduct:
		return 1.0 - threshold
	default:
		return 1.0 - threshold
	}
}

func vectorLiteral(vector []float32) string {
	values := make([]string, len(vector))
	for i, value := range vector {
		values[i] = strconv.FormatFloat(float64(value), 'g', -1, 32)
	}
	return "[" + strings.Join(values, ",") + "]"
}
