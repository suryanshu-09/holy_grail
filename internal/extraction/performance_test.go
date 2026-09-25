package extraction

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/questions"
	"github.com/suryanshu-09/holy_grail/internal/topics"
)

// TestExtractConcurrencyOrderPreservation verifies that concurrent page
// extraction preserves page order under a small worker pool.
func TestExtractConcurrencyOrderPreservation(t *testing.T) {
	t.Setenv("EXTRACT_CONCURRENCY", "2")
	root := t.TempDir()
	streams := []string{
		textStream("First page content with plenty of text here"),
		textStream("Second page content with plenty of text here"),
		textStream("Third page content with plenty of text here"),
		textStream("Fourth page content with plenty of text here"),
		textStream("Fifth page content with plenty of text here"),
	}
	storeTestPDF(t, root, "doc-order", buildTestPDF(t, streams))

	svc, err := NewService(root)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	got, err := svc.Extract(context.Background(), "doc-order")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if got.PageCount != 5 || len(got.Pages) != 5 {
		t.Fatalf("PageCount = %d pages = %d, want 5", got.PageCount, len(got.Pages))
	}
	wants := []string{"First", "Second", "Third", "Fourth", "Fifth"}
	for i, w := range wants {
		p := got.Pages[i]
		if p.Number != i+1 {
			t.Errorf("Pages[%d].Number = %d, want %d", i, p.Number, i+1)
		}
		if got.Pages[i].Text == "" || !contains(got.Pages[i].Text, w) {
			t.Errorf("Pages[%d].Text = %q, want to contain %q", i, got.Pages[i].Text, w)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// TestContentHashSkip verifies that a second pipeline run with an unchanged
// PDF reuses the cached extraction (manifest match) and still succeeds.
func TestContentHashSkip(t *testing.T) {
	root := t.TempDir()
	storeTestPDF(t, root, "doc-skip", buildTestPDF(t, []string{
		textStream("Q1. What is a process? Explain in detail here."),
	}))

	extractor, err := NewService(root)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	repo := newFakeRepo("doc-skip")
	qrepo := &fakeQuestionsRepo{}
	svc, err := NewExtractionService(root, extractor, repo, qrepo)
	if err != nil {
		t.Fatalf("NewExtractionService: %v", err)
	}

	if _, err := svc.Extract(context.Background(), "doc-skip", "documents/doc-skip/original.pdf"); err != nil {
		t.Fatalf("first Extract: %v", err)
	}
	manifestPath := filepath.Join(root, "documents", "doc-skip", "extraction", "manifest.json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("manifest.json missing after first run: %v", err)
	}

	cached, skip := svc.shouldSkipExtraction("doc-skip", ContentHashFile(filepath.Join(root, "documents", "doc-skip", "original.pdf")))
	if !skip || cached == nil {
		t.Fatalf("shouldSkipExtraction = (%v, %v), want cached reuse", cached != nil, skip)
	}

	// Second full run must succeed via the skip path and still mark extracted.
	if _, err := svc.Extract(context.Background(), "doc-skip", "documents/doc-skip/original.pdf"); err != nil {
		t.Fatalf("second Extract (skip path): %v", err)
	}
	if repo.statuses["doc-skip"] != "extracted" {
		t.Errorf("status = %q, want extracted", repo.statuses["doc-skip"])
	}
	// Timings artifact must record the skip.
	timingsPath := filepath.Join(root, "documents", "doc-skip", "extraction", "timings.json")
	data, err := os.ReadFile(timingsPath)
	if err != nil {
		t.Fatalf("read timings.json: %v", err)
	}
	var d ProcessingDurations
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatalf("unmarshal timings.json: %v", err)
	}
	if !d.Skipped || !d.SkippedExtraction {
		t.Errorf("durations = %+v, want skipped flags set on unchanged rerun", d)
	}
	if d.ContentHash == "" {
		t.Error("durations.ContentHash is empty, want PDF hash recorded")
	}
}

// TestProcessingDurationsArtifact verifies timings.json is written with
// per-step millisecond fields on a normal run.
func TestProcessingDurationsArtifact(t *testing.T) {
	root := t.TempDir()
	storeTestPDF(t, root, "doc-timing", buildTestPDF(t, []string{
		textStream("Q1. What is paging? Explain in detail for timing."),
	}))

	extractor, _ := NewService(root)
	repo := newFakeRepo("doc-timing")
	svc, err := NewExtractionService(root, extractor, repo, &fakeQuestionsRepo{})
	if err != nil {
		t.Fatalf("NewExtractionService: %v", err)
	}
	if _, err := svc.Extract(context.Background(), "doc-timing", ""); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "documents", "doc-timing", "extraction", "timings.json"))
	if err != nil {
		t.Fatalf("read timings.json: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal timings.json: %v", err)
	}
	for _, k := range []string{"page_extraction_ms", "vision_ms", "save_ms", "question_ms", "total_ms", "document_id"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("timings.json missing key %q: %v", k, raw)
		}
	}
}

// countingClassifier dedups via the pipeline cache; it counts ClassifyBatch
// calls and ClassifyQuestion fallback calls separately.
type countingClassifier struct {
	mu            sync.Mutex
	batchCalls    int
	questionCalls int64
	failBatchOnce bool
	labels        []topics.TopicLabel
}

func (c *countingClassifier) ClassifyQuestion(ctx context.Context, questionText, subject string) ([]topics.TopicLabel, error) {
	atomic.AddInt64(&c.questionCalls, 1)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.labels, nil
}

func (c *countingClassifier) ClassifyBatch(ctx context.Context, items []topics.BatchItem) ([][]topics.TopicLabel, error) {
	c.mu.Lock()
	c.batchCalls++
	fail := c.failBatchOnce
	c.failBatchOnce = false
	c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([][]topics.TopicLabel, len(items))
	for i := range items {
		out[i] = c.labels
	}
	if fail {
		return out, fmt.Errorf("batch partial failure (test)")
	}
	return out, nil
}

func (c *countingClassifier) LastPromptHash() string { return "testhash" }
func (c *countingClassifier) LastPrompt() string     { return "testprompt" }

// fakeTopicRepo implements the topics persistence subset used by ClassifyDocument.
type fakeTopicRepo struct {
	mu     sync.Mutex
	topics map[string]topics.Topic
	links  int
}

func newFakeTopicRepo() *fakeTopicRepo { return &fakeTopicRepo{topics: make(map[string]topics.Topic)} }

func (r *fakeTopicRepo) List(ctx context.Context, f topics.Filter) ([]topics.Topic, error) {
	return nil, nil
}
func (r *fakeTopicRepo) GetByID(ctx context.Context, id string) (topics.Topic, error) {
	return topics.Topic{}, fmt.Errorf("not found")
}
func (r *fakeTopicRepo) Create(ctx context.Context, name string, subject *string) (topics.Topic, error) {
	t := topics.Topic{ID: "topic-" + name, Name: name}
	r.mu.Lock()
	r.topics[name] = t
	r.mu.Unlock()
	return t, nil
}
func (r *fakeTopicRepo) GetByName(ctx context.Context, name string, subject *string) (topics.Topic, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.topics[name]; ok {
		return t, nil
	}
	return topics.Topic{}, fmt.Errorf("not found")
}
func (r *fakeTopicRepo) FindOrCreate(ctx context.Context, name string, subject *string) (topics.Topic, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.topics[name]; ok {
		return t, nil
	}
	t := topics.Topic{ID: "topic-" + name, Name: name}
	r.topics[name] = t
	return t, nil
}
func (r *fakeTopicRepo) ListWithCounts(ctx context.Context, f topics.Filter) ([]topics.TopicWithCount, error) {
	return nil, nil
}
func (r *fakeTopicRepo) MergeTopics(ctx context.Context, sourceID, targetID string) error {
	return nil
}
func (r *fakeTopicRepo) AddQuestionTopic(ctx context.Context, questionID, topicID string, confidence *float64) error {
	r.mu.Lock()
	r.links++
	r.mu.Unlock()
	return nil
}
func (r *fakeTopicRepo) RemoveQuestionTopic(ctx context.Context, questionID, topicID string) error {
	return nil
}
func (r *fakeTopicRepo) ListTopicsForQuestion(ctx context.Context, questionID string) ([]topics.Topic, error) {
	return nil, nil
}
func (r *fakeTopicRepo) ListQuestionTopics(ctx context.Context, questionID string) ([]topics.QuestionTopic, error) {
	return nil, nil
}
func (r *fakeTopicRepo) SetQuestionTopics(ctx context.Context, questionID string, topicIDs []string, confidences map[string]*float64) error {
	return nil
}

// listQuestionsRepo returns a fixed question list for classification tests.
type listQuestionsRepo struct {
	qs []questions.Question
}

func (r *listQuestionsRepo) List(_ context.Context, _ questions.Filter) ([]questions.Question, error) {
	return r.qs, nil
}
func (r *listQuestionsRepo) GetByID(_ context.Context, _ string) (questions.Question, error) {
	return questions.Question{}, fmt.Errorf("not found")
}
func (r *listQuestionsRepo) Insert(_ context.Context, _ questions.Question) error { return nil }

// TestClassifierPromptCacheDedup verifies duplicate question texts share one
// batch LLM call and that the classifier cache file is written.
func TestClassifierPromptCacheDedup(t *testing.T) {
	root := t.TempDir()
	extractor, _ := NewService(root)
	repo := newFakeRepo("doc-cache")
	text := "What is a process? Explain in detail."
	subj := "Operating Systems"
	qrepo := &listQuestionsRepo{qs: []questions.Question{
		{ID: "q1", DocumentID: "doc-cache", QuestionText: &text, Subject: &subj},
		{ID: "q2", DocumentID: "doc-cache", QuestionText: &text, Subject: &subj},
		{ID: "q3", DocumentID: "doc-cache", QuestionText: &text, Subject: &subj},
	}}
	svc, err := NewExtractionService(root, extractor, repo, qrepo)
	if err != nil {
		t.Fatalf("NewExtractionService: %v", err)
	}
	classifier := &countingClassifier{labels: []topics.TopicLabel{{Topic: "Processes", Confidence: 0.9, Subject: "Operating Systems"}}}
	svc.WithTopicClassifier(classifier, newFakeTopicRepo())

	if err := svc.ClassifyDocument(context.Background(), "doc-cache"); err != nil {
		t.Fatalf("ClassifyDocument: %v", err)
	}
	if classifier.batchCalls != 1 {
		t.Errorf("ClassifyBatch calls = %d, want 1 (dedup of 3 identical texts)", classifier.batchCalls)
	}
	if n := atomic.LoadInt64(&classifier.questionCalls); n != 0 {
		t.Errorf("ClassifyQuestion fallback calls = %d, want 0 on clean batch", n)
	}
	cachePath := filepath.Join(root, "documents", "doc-cache", "extraction", "classifier_cache.json")
	if _, err := os.Stat(cachePath); err != nil {
		t.Errorf("classifier_cache.json missing: %v", err)
	}
	// Second run hits the persisted cache and makes no new batch call.
	if err := svc.ClassifyDocument(context.Background(), "doc-cache"); err != nil {
		t.Fatalf("second ClassifyDocument: %v", err)
	}
	if classifier.batchCalls != 1 {
		t.Errorf("ClassifyBatch calls after cache hit = %d, want still 1", classifier.batchCalls)
	}
}

// TestContentHashHelpers verifies hash helpers are deterministic and the
// skip helper never breaks on missing/corrupt state.
func TestContentHashHelpers(t *testing.T) {
	if ContentHashBytes([]byte("abc")) != ContentHashBytes([]byte("abc")) {
		t.Error("ContentHashBytes not deterministic")
	}
	if ContentHashBytes([]byte("abc")) == ContentHashBytes([]byte("abd")) {
		t.Error("ContentHashBytes collision on distinct inputs")
	}
	if got := ContentHashFile("/nonexistent/path.pdf"); got != "" {
		t.Errorf("ContentHashFile(missing) = %q, want empty", got)
	}
	svc, _ := NewExtractionService(t.TempDir(), mustService(t), newFakeRepo(), &fakeQuestionsRepo{})
	if _, skip := svc.shouldSkipExtraction("doc-none", ""); skip {
		t.Error("shouldSkipExtraction with empty hash skipped, want false")
	}
	if _, skip := svc.shouldSkipExtraction("doc-none", "deadbeef"); skip {
		t.Error("shouldSkipExtraction with no manifest skipped, want false")
	}
}

func mustService(t *testing.T) *Service {
	t.Helper()
	svc, err := NewService(t.TempDir())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}
