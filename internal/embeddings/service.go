package embeddings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
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

// Service batches and persists embedding work. It is safe to invoke repeatedly:
// unchanged questions are skipped and equal rich inputs reuse an existing vector.
type Service struct {
	questions    QuestionReader
	topics       TopicReader
	repository   Repository
	embedder     Embedder
	BatchSize    int
	MaxAttempts  int
	RetryBackoff time.Duration
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
		for _, question := range batch {
			input, err := s.buildInput(ctx, question)
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
		for index, hash := range hashes {
			items := byHash[hash]
			first := items[0]
			if err := s.repository.Upsert(ctx, first.questionID, vectors[index], s.embedder.Model(), hash); err != nil {
				for _, item := range items {
					result.addFailure(item.questionID, err)
				}
				continue
			}
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
	return result, nil
}

func (s *Service) buildInput(ctx context.Context, question questions.Question) (string, error) {
	text := ""
	if question.QuestionText != nil {
		text = strings.TrimSpace(*question.QuestionText)
	}
	if text == "" {
		return "", fmt.Errorf("question text is empty")
	}
	questionTopics, err := s.topics.ListTopicsForQuestion(ctx, question.ID)
	if err != nil {
		return "", fmt.Errorf("list topics: %w", err)
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
