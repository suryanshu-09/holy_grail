package embeddings

import (
	"context"
	"time"
)

const (
	// DefaultModel is intentionally pinned. Changing it requires re-embedding
	// existing questions because vectors from distinct models are not comparable.
	DefaultModel      = "text-embedding-3-small"
	DefaultDimensions = 1536
)

// Embedder generates vectors for a batch of text inputs.
type Embedder interface {
	Model() string
	Dimensions() int
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
}

// Record stores the metadata needed to determine whether a question needs to
// be embedded again. The vector stays in PostgreSQL and is never returned here.
type Record struct {
	QuestionID string
	Model      string
	InputHash  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Repository persists question embeddings.
type Repository interface {
	Get(ctx context.Context, questionID string) (Record, bool, error)
	FindReusable(ctx context.Context, model, inputHash string) (Record, bool, error)
	Copy(ctx context.Context, questionID, sourceQuestionID, model, inputHash string) error
	Upsert(ctx context.Context, questionID string, vector []float32, model, inputHash string) error
}

// Result describes an embedding run. Individual failures are retained so a
// caller can surface a partial completion without discarding successful work.
type Result struct {
	DocumentID string    `json:"document_id"`
	Embedded   int       `json:"embedded"`
	Reused     int       `json:"reused"`
	Skipped    int       `json:"skipped"`
	Failed     int       `json:"failed"`
	Failures   []Failure `json:"failures,omitempty"`
}

// Failure identifies one question that could not be embedded.
type Failure struct {
	QuestionID string `json:"question_id"`
	Error      string `json:"error"`
}

func (r Result) Status() string {
	if r.Failed > 0 {
		return "partial"
	}
	return "embedded"
}
