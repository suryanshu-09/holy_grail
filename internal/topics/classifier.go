package topics

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/suryanshu-09/holy_grail/internal/llm"
)

// Classifier wraps an LLM client for topic classification with
// concurrency control and rate limiting. It builds compact prompts,
// validates strict JSON, clamps confidence, normalizes topic names,
// dedups, and records prompt hash for reproducibility.
type Classifier struct {
	Client         llm.Client
	MaxConcurrency int
	MinInterval    time.Duration

	mu         sync.Mutex
	lastCall   time.Time
	lastPrompt string
	lastHash   string
	// promptHashes stores per-prompt hash for batch observability
	promptHashes map[string]string
}

// BatchItem is a single input for ClassifyBatch.
type BatchItem struct {
	QuestionText string `json:"question_text"`
	Subject      string `json:"subject"`
}

// BatchResult holds the classification output for one batch item.
type BatchResult struct {
	Labels     []TopicLabel `json:"labels"`
	PromptHash string       `json:"prompt_hash"`
	Error      error        `json:"-"`
}

// ClassifierInterface defines the contract for topic classification,
// enabling mocks in tests and callers.
type ClassifierInterface interface {
	ClassifyQuestion(ctx context.Context, questionText, subject string) ([]TopicLabel, error)
	ClassifyBatch(ctx context.Context, items []BatchItem) ([][]TopicLabel, error)
	LastPromptHash() string
	LastPrompt() string
}

// Ensure Classifier implements ClassifierInterface.
var _ ClassifierInterface = (*Classifier)(nil)

// NewClassifier creates a classifier with the given client and limits.
// If maxConcurrency <=0 it defaults to 3. minInterval may be zero to disable rate limiting.
func NewClassifier(client llm.Client, maxConcurrency int, minInterval time.Duration) *Classifier {
	if maxConcurrency <= 0 {
		maxConcurrency = 3
	}
	return &Classifier{
		Client:         client,
		MaxConcurrency: maxConcurrency,
		MinInterval:    minInterval,
		promptHashes:   make(map[string]string),
	}
}

// LastPromptHash returns the sha256 hex (first 16 chars) of the last prompt sent.
func (c *Classifier) LastPromptHash() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastHash
}

// LastPrompt returns the last prompt string sent to the LLM.
func (c *Classifier) LastPrompt() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastPrompt
}

// promptHash computes deterministic hash for a prompt.
func promptHashFor(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)[:16]
}

// wait enforces MinInterval rate limiting before the next LLM call.
func (c *Classifier) wait(ctx context.Context) error {
	if c.MinInterval <= 0 {
		return nil
	}
	c.mu.Lock()
	elapsed := time.Since(c.lastCall)
	c.mu.Unlock()
	if elapsed < c.MinInterval {
		wait := c.MinInterval - elapsed
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// recordPrompt stores prompt and its hash atomically and updates lastCall.
func (c *Classifier) recordPrompt(prompt string) string {
	hash := promptHashFor(prompt)
	c.mu.Lock()
	c.lastPrompt = prompt
	c.lastHash = hash
	c.lastCall = time.Now()
	if c.promptHashes == nil {
		c.promptHashes = make(map[string]string)
	}
	c.promptHashes[prompt] = hash
	c.mu.Unlock()
	return hash
}

// buildPrompt creates a compact prompt containing subject hint and question text
// plus instructions for strict JSON output.
func (c *Classifier) buildPrompt(questionText, subject string) string {
	// Use the canonical prompt builder from classification.go for determinism.
	// It already produces compact subject+question instructions for strict JSON.
	return BuildClassificationPrompt(ClassificationRequest{
		QuestionText: questionText,
		SubjectHint:  subject,
	})
}

// ClassifyQuestion builds a compact prompt (subject+question text, strict JSON
// instructions), calls the LLM, validates JSON schema, clamps confidence to
// [0,1], normalizes names via NormalizeTopicName, dedups via CanonicalTopicKey
// (keeping max confidence), and records prompt hash. It retries up to 3 times
// internally on transient LLM errors? The primary retry is in ClassifyBatch;
// ClassifyQuestion does a single call but will still validate strictly. If the
// caller needs retry, use ClassifyBatch or wrap.
func (c *Classifier) ClassifyQuestion(ctx context.Context, questionText, subject string) ([]TopicLabel, error) {
	if strings.TrimSpace(questionText) == "" {
		return nil, fmt.Errorf("topics: classifier: questionText is required")
	}
	if c.Client == nil {
		return nil, fmt.Errorf("topics: classifier: no llm client configured")
	}

	prompt := c.buildPrompt(questionText, subject)
	hash := c.recordPrompt(prompt)
	_ = hash // recorded via recordPrompt

	if err := c.wait(ctx); err != nil {
		return nil, err
	}

	raw, err := c.Client.ClassifyTopics(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("topics: classifier: llm call: %w", err)
	}

	labels, err := c.parseAndNormalize(raw, subject)
	if err != nil {
		return nil, err
	}
	// Ensure prompt hash is still accessible via LastPromptHash; already recorded.
	return labels, nil
}

// parseAndNormalize handles fence stripping, JSON extraction, strict validation
// (except confidence which is clamped), normalization and dedup.
func (c *Classifier) parseAndNormalize(raw string, subjectHint string) ([]TopicLabel, error) {
	cleaned := StripMarkdownFences(raw)
	cleaned = strings.TrimSpace(cleaned)
	if !strings.HasPrefix(cleaned, "{") {
		cleaned = ExtractJSON(cleaned)
	}
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return nil, fmt.Errorf("topics: classifier: empty LLM response")
	}

	// Strict decode with DisallowUnknownFields, but allow confidence out of range
	// so we can clamp rather than fail.
	dec := json.NewDecoder(strings.NewReader(cleaned))
	dec.DisallowUnknownFields()
	var resp ClassificationResponse
	if err := dec.Decode(&resp); err != nil {
		return nil, fmt.Errorf("topics: classifier: invalid classification JSON: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("topics: classifier: trailing data after JSON object")
	}

	// Fallback subject if LLM returned empty but hint provided
	if strings.TrimSpace(resp.Subject) == "" {
		if strings.TrimSpace(subjectHint) != "" {
			resp.Subject = strings.TrimSpace(subjectHint)
		} else {
			return nil, fmt.Errorf("topics: classifier: subject is required")
		}
	}
	if resp.Topics == nil {
		return nil, fmt.Errorf("topics: classifier: topics is required (use empty array if none)")
	}
	if len(resp.Topics) > 10 {
		return nil, fmt.Errorf("topics: classifier: too many topics: %d > 10", len(resp.Topics))
	}

	// Clamp confidence and validate topic names, then normalize & dedup
	clamped := make([]TopicLabel, 0, len(resp.Topics))
	for i, t := range resp.Topics {
		if strings.TrimSpace(t.Topic) == "" {
			return nil, fmt.Errorf("topics: classifier: topics[%d]: topic name is required", i)
		}
		conf := t.Confidence
		if conf < 0 {
			conf = 0
		}
		if conf > 1 {
			conf = 1
		}
		// normalize name
		normName := NormalizeTopicName(t.Topic)
		if normName == "" {
			continue // drop empty after normalization
		}
		subj := t.Subject
		if strings.TrimSpace(subj) == "" {
			// carry subject from response-level subject if per-topic is empty
			subj = resp.Subject
		}
		clamped = append(clamped, TopicLabel{
			Topic:      normName,
			Confidence: conf,
			Subject:    strings.TrimSpace(subj),
		})
	}

	// Dedup via NormalizeLabels (merges by canonical key, max confidence)
	deduped := NormalizeLabels(clamped)
	return deduped, nil
}

// ClassifyBatch classifies a batch of questions with bounded concurrency
// (MaxConcurrency) and retry (3 tries per item). It respects MinInterval
// globally across calls. Each item is retried independently on LLM or parse
// errors. If an item fails after 3 attempts, its slot in the result will be
// an error and the overall batch returns the first error but still provides
// partial results in the returned slice. Callers can also inspect per-item
// errors via ClassifyBatchDetailed.
func (c *Classifier) ClassifyBatch(ctx context.Context, items []BatchItem) ([][]TopicLabel, error) {
	if c.Client == nil {
		return nil, fmt.Errorf("topics: classifier: no llm client configured")
	}
	if len(items) == 0 {
		return [][]TopicLabel{}, nil
	}
	maxConc := c.MaxConcurrency
	if maxConc <= 0 {
		maxConc = 3
	}

	sem := make(chan struct{}, maxConc)
	results := make([][]TopicLabel, len(items))
	errs := make([]error, len(items))

	var wg sync.WaitGroup
	for idx, item := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, it BatchItem) {
			defer wg.Done()
			defer func() { <-sem }()

			var labels []TopicLabel
			var lastErr error
			// retry up to 3 tries
			for attempt := 0; attempt < 3; attempt++ {
				if ctx.Err() != nil {
					lastErr = ctx.Err()
					break
				}
				// Respect MinInterval between attempts globally
				if err := c.wait(ctx); err != nil {
					lastErr = err
					break
				}
				prompt := c.buildPrompt(it.QuestionText, it.Subject)
				// record hash before call for observability even on failure
				c.recordPrompt(prompt)

				raw, err := c.Client.ClassifyTopics(ctx, prompt)
				if err != nil {
					lastErr = fmt.Errorf("attempt %d: llm call: %w", attempt+1, err)
					// backoff before retry except last attempt
					if attempt < 2 {
						select {
						case <-time.After(time.Duration(attempt+1) * 20 * time.Millisecond):
						case <-ctx.Done():
							lastErr = ctx.Err()
							break
						}
					}
					continue
				}
				parsed, err := c.parseAndNormalize(raw, it.Subject)
				if err != nil {
					lastErr = fmt.Errorf("attempt %d: parse: %w", attempt+1, err)
					if attempt < 2 {
						select {
						case <-time.After(time.Duration(attempt+1) * 20 * time.Millisecond):
						case <-ctx.Done():
							lastErr = ctx.Err()
							break
						}
					}
					continue
				}
				labels = parsed
				lastErr = nil
				break
			}
			results[i] = labels
			errs[i] = lastErr
		}(idx, item)
	}
	wg.Wait()

	// return first error if any item failed after retries
	for _, e := range errs {
		if e != nil {
			return results, e
		}
	}
	return results, nil
}

// ClassifyBatchDetailed is a variant that returns per-item BatchResult
// including prompt hash and per-item error, allowing partial success.
// It also respects MaxConcurrency, MinInterval, and 3 retries.
func (c *Classifier) ClassifyBatchDetailed(ctx context.Context, items []BatchItem) ([]BatchResult, error) {
	if c.Client == nil {
		return nil, fmt.Errorf("topics: classifier: no llm client configured")
	}
	if len(items) == 0 {
		return []BatchResult{}, nil
	}
	maxConc := c.MaxConcurrency
	if maxConc <= 0 {
		maxConc = 3
	}
	sem := make(chan struct{}, maxConc)
	results := make([]BatchResult, len(items))
	var wg sync.WaitGroup
	for idx, item := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, it BatchItem) {
			defer wg.Done()
			defer func() { <-sem }()

			var labels []TopicLabel
			var lastErr error
			var lastHash string
			for attempt := 0; attempt < 3; attempt++ {
				if ctx.Err() != nil {
					lastErr = ctx.Err()
					break
				}
				if err := c.wait(ctx); err != nil {
					lastErr = err
					break
				}
				prompt := c.buildPrompt(it.QuestionText, it.Subject)
				lastHash = c.recordPrompt(prompt)
				raw, err := c.Client.ClassifyTopics(ctx, prompt)
				if err != nil {
					lastErr = fmt.Errorf("attempt %d: llm call: %w", attempt+1, err)
					if attempt < 2 {
						select {
						case <-time.After(time.Duration(attempt+1) * 20 * time.Millisecond):
						case <-ctx.Done():
							lastErr = ctx.Err()
							break
						}
					}
					continue
				}
				parsed, err := c.parseAndNormalize(raw, it.Subject)
				if err != nil {
					lastErr = fmt.Errorf("attempt %d: parse: %w", attempt+1, err)
					if attempt < 2 {
						select {
						case <-time.After(time.Duration(attempt+1) * 20 * time.Millisecond):
						case <-ctx.Done():
							lastErr = ctx.Err()
							break
						}
					}
					continue
				}
				labels = parsed
				lastErr = nil
				break
			}
			results[i] = BatchResult{Labels: labels, PromptHash: lastHash, Error: lastErr}
		}(idx, item)
	}
	wg.Wait()
	return results, nil
}
