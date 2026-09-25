package embeddings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/suryanshu-09/holy_grail/internal/questions"
	"github.com/suryanshu-09/holy_grail/internal/topics"
)

const defaultBatchSize = 64

// QuestionReader is the narrow question contract used by embedding jobs.
type QuestionReader interface {
	List(ctx context.Context, filter questions.Filter) ([]questions.Question, error)
}

// TopicReader provides the topic metadata included in rich embedding input.
type TopicReader interface {
	ListTopicsForQuestion(ctx context.Context, questionID string) ([]topics.Topic, error)
}

// BatchTopicReader is an optional fast path for topic metadata. When the
// configured TopicReader implements it, the service fetches topics for a whole
// page of questions with one query instead of one query per question (N+1).
// On error the service falls back to per-question ListTopicsForQuestion.
type BatchTopicReader interface {
	ListTopicsForQuestions(ctx context.Context, questionIDs []string) (map[string][]topics.Topic, error)
}

// batchUpserter is the optional batch persistence fast path. When the
// configured Repository implements it, fresh vectors are stored with a single
// multi-row statement; otherwise the service falls back to per-item Upsert.
type batchUpserter interface {
	BatchUpsert(ctx context.Context, items []UpsertItem) error
}

// Service batches and persists embedding work. It is safe to invoke repeatedly:
// unchanged questions are skipped and equal rich inputs reuse an existing vector.
//
// Process-wide vector cache: in addition to the per-run byHash dedup and the
// DB-side FindReusable/Copy reuse, successfully embedded vectors are kept in
// a process-wide in-memory cache keyed by model+input-hash. A later run (same
// or different Service instance) can Upsert directly from the cache without
// calling the provider, then Copy to duplicates. The cache is nil-safe,
// bounded best-effort, and never changes error semantics.
type Service struct {
	questions    QuestionReader
	topics       TopicReader
	repository   Repository
	embedder     Embedder
	BatchSize    int
	MaxAttempts  int
	RetryBackoff time.Duration
}

// globalEmbeddingCache is the process-wide model+hash -> vector store.
// sync.Map gives lock-free concurrent access; values are copied on store
// and load so callers can never alias the cached slice.
var globalEmbeddingCache sync.Map // map[string][]float32

// embeddingCacheKey builds the process-cache key. Empty model/hash yields "".
func embeddingCacheKey(model, hash string) string {
	if model == "" || hash == "" {
		return ""
	}
	return model + "\x00" + hash
}

// GlobalEmbeddingCacheHit reports whether a vector for model+hash is cached
// in-process. It is exported for observability and unit tests; a nil/empty
// key always reports false.
func GlobalEmbeddingCacheHit(model, hash string) bool {
	key := embeddingCacheKey(model, hash)
	if key == "" {
		return false
	}
	_, ok := globalEmbeddingCache.Load(key)
	return ok
}

// ClearGlobalEmbeddingCache empties the process-wide cache (tests only).
func ClearGlobalEmbeddingCache() {
	globalEmbeddingCache.Range(func(k, _ any) bool {
		globalEmbeddingCache.Delete(k)
		return true
	})
}

func globalCacheGet(model, hash string) ([]float32, bool) {
	key := embeddingCacheKey(model, hash)
	if key == "" {
		return nil, false
	}
	v, ok := globalEmbeddingCache.Load(key)
	if !ok {
		return nil, false
	}
	vec, ok := v.([]float32)
	if !ok || len(vec) == 0 {
		return nil, false
	}
	out := make([]float32, len(vec))
	copy(out, vec)
	return out, true
}

func globalCacheSet(model, hash string, vec []float32) {
	key := embeddingCacheKey(model, hash)
	if key == "" || len(vec) == 0 {
		return
	}
	out := make([]float32, len(vec))
	copy(out, vec)
	globalEmbeddingCache.Store(key, out)
}

func NewService(questionReader QuestionReader, topicReader TopicReader, repository Repository, embedder Embedder) (*Service, error) {
	if questionReader == nil || topicReader == nil || repository == nil || embedder == nil {
		return nil, fmt.Errorf("embeddings: all dependencies are required")
	}
	if embedder.Dimensions() != DefaultDimensions {
		return nil, fmt.Errorf("embeddings: dimension %d is incompatible with vector(%d)", embedder.Dimensions(), DefaultDimensions)
	}
	if strings.TrimSpace(embedder.Model()) == "" {
		return nil, fmt.Errorf("embeddings: model is required")
	}
	return &Service{
		questions: questionReader, topics: topicReader, repository: repository, embedder: embedder,
		BatchSize: defaultBatchSize, MaxAttempts: 3, RetryBackoff: 200 * time.Millisecond,
	}, nil
}

type pendingEmbedding struct {
	questionID string
	hash       string
	input      string
}

// EmbedDocument embeds all questions in a document. Listing errors are fatal;
// failures for individual inputs are returned in Result so successful batches
// are never discarded.
func (s *Service) EmbedDocument(ctx context.Context, documentID string) (Result, error) {
	if strings.TrimSpace(documentID) == "" {
		return Result{}, fmt.Errorf("embeddings: document id is required")
	}
	result := Result{DocumentID: documentID}
	var pending []pendingEmbedding
	for offset := 0; ; offset += 100 {
		batch, err := s.questions.List(ctx, questions.Filter{DocumentID: documentID, Limit: 100, Offset: offset})
		if err != nil {
			return result, fmt.Errorf("embeddings: list document questions: %w", err)
		}
		// Fetch topics for the whole page in one query when supported,
		// falling back to per-question lookups on error (backward compat).
		topicMap := s.topicsForBatch(ctx, batch)
		for _, question := range batch {
			input, err := s.buildInputWithMap(ctx, question, topicMap)
			if err != nil {
				result.addFailure(question.ID, err)
				continue
			}
			hash := inputHash(input)
			record, exists, err := s.repository.Get(ctx, question.ID)
			if err != nil {
				return result, err
			}
			if exists && record.Model == s.embedder.Model() && record.InputHash == hash {
				result.Skipped++
				continue
			}
			if source, found, err := s.repository.FindReusable(ctx, s.embedder.Model(), hash); err != nil {
				return result, err
			} else if found && source.QuestionID != question.ID {
				if err := s.repository.Copy(ctx, question.ID, source.QuestionID, s.embedder.Model(), hash); err != nil {
					return result, err
				}
				result.Reused++
				continue
			}
			// Process-wide in-memory cache: reuse a vector embedded earlier
			// in this process without calling the provider. Upsert from the
			// cached vector and count as reused (provider-free).
			if cached, hit := globalCacheGet(s.embedder.Model(), hash); hit {
				if err := s.repository.Upsert(ctx, question.ID, cached, s.embedder.Model(), hash); err != nil {
					return result, err
				}
				result.Reused++
				continue
			}
			pending = append(pending, pendingEmbedding{questionID: question.ID, hash: hash, input: input})
		}
		if len(batch) < 100 {
			break
		}
	}

	// Deduplicate fresh requests in this run. Once one vector is stored, all
	// questions with the same input receive a database-side copy of that vector.
	byHash := make(map[string][]pendingEmbedding)
	order := make([]string, 0, len(pending))
	for _, item := range pending {
		if _, exists := byHash[item.hash]; !exists {
			order = append(order, item.hash)
		}
		byHash[item.hash] = append(byHash[item.hash], item)
	}
	batchSize := s.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	for start := 0; start < len(order); start += batchSize {
		end := start + batchSize
		if end > len(order) {
			end = len(order)
		}
		hashes := order[start:end]
		inputs := make([]string, len(hashes))
		for index, hash := range hashes {
			inputs[index] = byHash[hash][0].input
		}
		vectors, err := s.embedWithRetry(ctx, inputs)
		if err != nil {
			for _, hash := range hashes {
				for _, item := range byHash[hash] {
					result.addFailure(item.questionID, err)
				}
			}
			continue
		}
		s.persistVectors(ctx, &result, hashes, byHash, vectors)
	}
	return result, nil
}

// persistVectors stores one fresh vector per hash, then database-side copies
// it to duplicate questions. It uses BatchUpsert when the repository supports
// it and falls back to per-item Upsert otherwise (or on batch error).
func (s *Service) persistVectors(ctx context.Context, result *Result, hashes []string, byHash map[string][]pendingEmbedding, vectors [][]float32) {
	batchItems := make([]UpsertItem, len(hashes))
	for index, hash := range hashes {
		first := byHash[hash][0]
		batchItems[index] = UpsertItem{QuestionID: first.questionID, Vector: vectors[index], Model: s.embedder.Model(), InputHash: hash}
	}
	upserted := make([]bool, len(hashes))
	if bu, ok := s.repository.(batchUpserter); ok {
		// On batch error, fall through to per-item Upsert below.
		if err := bu.BatchUpsert(ctx, batchItems); err == nil {
			for i := range upserted {
				upserted[i] = true
			}
		}
	}
	for index, hash := range hashes {
		items := byHash[hash]
		first := items[0]
		if !upserted[index] {
			if err := s.repository.Upsert(ctx, first.questionID, vectors[index], s.embedder.Model(), hash); err != nil {
				for _, item := range items {
					result.addFailure(item.questionID, err)
				}
				continue
			}
		}
		// Populate the process-wide cache so later runs reuse this vector
		// without calling the provider (Copy path stays DB-side).
		globalCacheSet(s.embedder.Model(), hash, vectors[index])
		result.Embedded++
		for _, item := range items[1:] {
			if err := s.repository.Copy(ctx, item.questionID, first.questionID, s.embedder.Model(), hash); err != nil {
				result.addFailure(item.questionID, err)
				continue
			}
			result.Reused++
		}
	}
}

// topicsForBatch returns topic metadata for a page of questions with a single
// query when the TopicReader supports it. It returns nil when the reader only
// implements per-question lookups, or when the batch fetch fails (callers then
// fall back to ListTopicsForQuestion per question).
func (s *Service) topicsForBatch(ctx context.Context, batch []questions.Question) map[string][]topics.Topic {
	batcher, ok := s.topics.(BatchTopicReader)
	if !ok || len(batch) == 0 {
		return nil
	}
	ids := make([]string, 0, len(batch))
	for _, q := range batch {
		ids = append(ids, q.ID)
	}
	m, err := batcher.ListTopicsForQuestions(ctx, ids)
	if err != nil {
		return nil
	}
	return m
}

// buildInputWithMap builds rich embedding input, preferring the pre-fetched
// batch topic map and falling back to a single ListTopicsForQuestion lookup
// when the map is nil or misses the question (backward compat).
func (s *Service) buildInputWithMap(ctx context.Context, question questions.Question, topicMap map[string][]topics.Topic) (string, error) {
	if topicMap != nil {
		if ts, ok := topicMap[question.ID]; ok {
			return buildInputWithTopics(question, ts)
		}
		// Key absent (e.g. partial map): fall back to a single lookup.
	}
	return s.buildInput(ctx, question)
}

func (s *Service) buildInput(ctx context.Context, question questions.Question) (string, error) {
	questionTopics, err := s.topics.ListTopicsForQuestion(ctx, question.ID)
	if err != nil {
		return "", fmt.Errorf("list topics: %w", err)
	}
	input, err := buildInputWithTopics(question, questionTopics)
	if err != nil {
		// Preserve the historical error shape for empty question text.
		return "", err
	}
	return input, nil
}

// buildInputWithTopics is the pure rich-input renderer: subject + topic names
// + question text. It performs no I/O and is unit-testable.
func buildInputWithTopics(question questions.Question, questionTopics []topics.Topic) (string, error) {
	text := ""
	if question.QuestionText != nil {
		text = strings.TrimSpace(*question.QuestionText)
	}
	if text == "" {
		return "", fmt.Errorf("question text is empty")
	}
	var builder strings.Builder
	builder.WriteString("Subject: ")
	if question.Subject != nil && strings.TrimSpace(*question.Subject) != "" {
		builder.WriteString(strings.TrimSpace(*question.Subject))
	} else {
		builder.WriteString("Unspecified")
	}
	builder.WriteString("\n\nTopics:\n")
	if len(questionTopics) == 0 {
		builder.WriteString("Unspecified\n")
	} else {
		for _, topic := range questionTopics {
			name := strings.TrimSpace(topic.Name)
			if name != "" {
				builder.WriteString("- ")
				builder.WriteString(name)
				builder.WriteByte('\n')
			}
		}
	}
	builder.WriteString("\nQuestion:\n")
	builder.WriteString(text)
	return builder.String(), nil
}

func (s *Service) embedWithRetry(ctx context.Context, inputs []string) ([][]float32, error) {
	attempts := s.MaxAttempts
	if attempts <= 0 {
		attempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		vectors, err := s.embedder.Embed(ctx, inputs)
		if err == nil {
			if len(vectors) != len(inputs) {
				return nil, fmt.Errorf("embeddings: embedder returned %d vectors for %d inputs", len(vectors), len(inputs))
			}
			return vectors, nil
		}
		lastErr = err
		if ctx.Err() != nil || attempt == attempts-1 {
			break
		}
		delay := s.RetryBackoff
		if delay <= 0 {
			delay = 200 * time.Millisecond
		}
		delay *= time.Duration(1 << attempt)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, fmt.Errorf("embeddings: exhausted %d attempts: %w", attempts, lastErr)
}

func (r *Result) addFailure(questionID string, err error) {
	r.Failed++
	r.Failures = append(r.Failures, Failure{QuestionID: questionID, Error: err.Error()})
}

func inputHash(input string) string {
	hash := sha256.Sum256([]byte(input))
	return hex.EncodeToString(hash[:])
}
