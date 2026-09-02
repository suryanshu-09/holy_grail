package topics

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/suryanshu-09/holy_grail/internal/llm"
)

// fakeLLM is a test double implementing llm.Client via ClassifyTopics.
// It records prompts and allows per-call function injection.
type fakeLLM struct {
	mu           sync.Mutex
	prompts      []string
	callCount    int
	classifyFunc func(ctx context.Context, prompt string) (string, error)
}

func (f *fakeLLM) ExtractQuestionsFromText(ctx context.Context, prompt string) (string, error) {
	return "[]", nil
}

func (f *fakeLLM) ClassifyTopics(ctx context.Context, prompt string) (string, error) {
	f.mu.Lock()
	f.prompts = append(f.prompts, prompt)
	f.callCount++
	fn := f.classifyFunc
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx, prompt)
	}
	return `{"subject":"Operating Systems","topics":[]}`, nil
}

func (f *fakeLLM) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.callCount
}

func (f *fakeLLM) Prompts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]string, len(f.prompts))
	copy(cp, f.prompts)
	return cp
}

// compile-time check that fakeLLM implements llm.Client
var _ llm.Client = (*fakeLLM)(nil)

func TestClassifier_ClassifyQuestion_Success(t *testing.T) {
	fake := &fakeLLM{
		classifyFunc: func(ctx context.Context, prompt string) (string, error) {
			// Verify prompt is compact and contains subject+question and JSON instructions
			if !strings.Contains(prompt, "Operating Systems") {
				t.Errorf("prompt missing subject hint")
			}
			if !strings.Contains(prompt, "deadlock") {
				t.Errorf("prompt missing question text")
			}
			if !strings.Contains(prompt, `"topics"`) {
				t.Errorf("prompt missing JSON instructions")
			}
			return `{"subject":"Operating Systems","topics":[{"topic":"Deadlock","confidence":0.9},{"topic":"Paging","confidence":0.7}]}`, nil
		},
	}
	c := NewClassifier(fake, 2, 0)
	labels, err := c.ClassifyQuestion(context.Background(), "Explain the four necessary conditions for deadlock", "Operating Systems")
	if err != nil {
		t.Fatalf("ClassifyQuestion: %v", err)
	}
	if len(labels) != 2 {
		t.Fatalf("expected 2 labels, got %d: %v", len(labels), labels)
	}
	m := map[string]float64{}
	for _, l := range labels {
		m[l.Topic] = l.Confidence
	}
	if m["Deadlock"] != 0.9 {
		t.Errorf("Deadlock confidence=%v want 0.9", m["Deadlock"])
	}
	if m["Paging"] != 0.7 {
		t.Errorf("Paging confidence=%v want 0.7", m["Paging"])
	}
}

func TestClassifier_ClassifyQuestion_NormalizesAndDedups(t *testing.T) {
	fake := &fakeLLM{
		classifyFunc: func(ctx context.Context, prompt string) (string, error) {
			return `{"subject":"Operating Systems","topics":[
				{"topic":"Deadlocks","confidence":0.8},
				{"topic":"deadlock","confidence":0.9},
				{"topic":"Deadlock Problem","confidence":0.5},
				{"topic":"Paging","confidence":0.7},
				{"topic":"  paging ","confidence":0.6}
			]}`, nil
		},
	}
	c := NewClassifier(fake, 1, 0)
	labels, err := c.ClassifyQuestion(context.Background(), "question about deadlocks and paging", "Operating Systems")
	if err != nil {
		t.Fatalf("ClassifyQuestion: %v", err)
	}
	if len(labels) != 2 {
		t.Fatalf("expected deduped 2, got %d: %v", len(labels), labels)
	}
	// Should be normalized display names and max confidence
	m := map[string]float64{}
	for _, l := range labels {
		m[CanonicalTopicKey(l.Topic)] = l.Confidence
	}
	if m["deadlock"] != 0.9 {
		t.Errorf("deadlock confidence=%v want 0.9 (max)", m["deadlock"])
	}
	if m["paging"] != 0.7 {
		t.Errorf("paging confidence=%v want 0.7 (max)", m["paging"])
	}
	for _, l := range labels {
		if l.Topic == "Deadlocks" || l.Topic == "deadlock" || l.Topic == "Deadlock Problem" {
			t.Errorf("not normalized: %q", l.Topic)
		}
	}
	// Order preserves first occurrence: Deadlock before Paging
	if labels[0].Topic != "Deadlock" {
		t.Errorf("first label expected Deadlock got %q", labels[0].Topic)
	}
	if labels[1].Topic != "Paging" {
		t.Errorf("second label expected Paging got %q", labels[1].Topic)
	}
}

func TestClassifier_ClassifyQuestion_ClampsConfidence(t *testing.T) {
	fake := &fakeLLM{
		classifyFunc: func(ctx context.Context, prompt string) (string, error) {
			return `{"subject":"OS","topics":[
				{"topic":"Deadlock","confidence":1.5},
				{"topic":"Paging","confidence":-0.2},
				{"topic":"CPU Scheduling","confidence":0.5}
			]}`, nil
		},
	}
	c := NewClassifier(fake, 1, 0)
	labels, err := c.ClassifyQuestion(context.Background(), "q", "OS")
	if err != nil {
		t.Fatalf("ClassifyQuestion: %v", err)
	}
	m := map[string]float64{}
	for _, l := range labels {
		m[l.Topic] = l.Confidence
	}
	if m["Deadlock"] != 1.0 {
		t.Errorf("clamp high: got %v want 1.0", m["Deadlock"])
	}
	if m["Paging"] != 0.0 {
		t.Errorf("clamp low: got %v want 0.0", m["Paging"])
	}
	if m["CPU Scheduling"] != 0.5 {
		t.Errorf("normal confidence corrupted: %v", m["CPU Scheduling"])
	}
}

func TestClassifier_ClassifyQuestion_StripsFences(t *testing.T) {
	fake := &fakeLLM{
		classifyFunc: func(ctx context.Context, prompt string) (string, error) {
			return "```json\n{\"subject\":\"Operating Systems\",\"topics\":[{\"topic\":\"Deadlock\",\"confidence\":0.88}]}\n```", nil
		},
	}
	c := NewClassifier(fake, 1, 0)
	labels, err := c.ClassifyQuestion(context.Background(), "deadlock question", "Operating Systems")
	if err != nil {
		t.Fatalf("ClassifyQuestion with fences: %v", err)
	}
	if len(labels) != 1 || labels[0].Topic != "Deadlock" || labels[0].Confidence != 0.88 {
		t.Errorf("unexpected labels %v", labels)
	}
}

func TestClassifier_ClassifyQuestion_ExtractJSONFromProse(t *testing.T) {
	fake := &fakeLLM{
		classifyFunc: func(ctx context.Context, prompt string) (string, error) {
			return "Here is the result: {\"subject\":\"Operating Systems\",\"topics\":[{\"topic\":\"Synchronization\",\"confidence\":0.77}]} hope that helps", nil
		},
	}
	c := NewClassifier(fake, 1, 0)
	labels, err := c.ClassifyQuestion(context.Background(), "synchronization question", "Operating Systems")
	if err != nil {
		t.Fatalf("ClassifyQuestion with prose wrapper: %v", err)
	}
	if len(labels) != 1 || labels[0].Topic != "Synchronization" {
		t.Errorf("unexpected labels %v", labels)
	}
}

func TestClassifier_ClassifyQuestion_RecordsPromptHash(t *testing.T) {
	fake := &fakeLLM{
		classifyFunc: func(ctx context.Context, prompt string) (string, error) {
			return `{"subject":"Operating Systems","topics":[]}`, nil
		},
	}
	c := NewClassifier(fake, 1, 0)
	if got := c.LastPromptHash(); got != "" {
		t.Errorf("initial hash should be empty, got %q", got)
	}
	_, err := c.ClassifyQuestion(context.Background(), "Explain paging", "Operating Systems")
	if err != nil {
		t.Fatalf("ClassifyQuestion: %v", err)
	}
	hash := c.LastPromptHash()
	if hash == "" {
		t.Fatalf("LastPromptHash empty after call")
	}
	if len(hash) != 16 {
		t.Errorf("hash length=%d want 16, got %q", len(hash), hash)
	}
	// Second call should change hash if question differs
	_, err = c.ClassifyQuestion(context.Background(), "Explain deadlock", "Operating Systems")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	hash2 := c.LastPromptHash()
	if hash2 == hash {
		t.Errorf("hash did not change after different question: %q", hash2)
	}
	// Same question should produce same hash
	c2 := NewClassifier(fake, 1, 0)
	q := "Same question text"
	_, _ = c2.ClassifyQuestion(context.Background(), q, "OS")
	h1 := c2.LastPromptHash()
	_, _ = c2.ClassifyQuestion(context.Background(), q, "OS")
	h2 := c2.LastPromptHash()
	if h1 != h2 {
		t.Errorf("same prompt should have same hash: %q vs %q", h1, h2)
	}
	// Check LastPrompt contains subject+question
	prompt := c2.LastPrompt()
	if !strings.Contains(prompt, q) {
		t.Errorf("LastPrompt missing question text: %q", prompt)
	}
}

func TestClassifier_ClassifyQuestion_EmptyQuestionError(t *testing.T) {
	fake := &fakeLLM{}
	c := NewClassifier(fake, 1, 0)
	_, err := c.ClassifyQuestion(context.Background(), "   ", "OS")
	if err == nil {
		t.Fatalf("expected error for empty question")
	}
	_, err = c.ClassifyQuestion(context.Background(), "", "OS")
	if err == nil {
		t.Fatalf("expected error for empty question")
	}
}

func TestClassifier_ClassifyQuestion_NilClientError(t *testing.T) {
	c := NewClassifier(nil, 1, 0)
	_, err := c.ClassifyQuestion(context.Background(), "question", "OS")
	if err == nil {
		t.Fatalf("expected error for nil client")
	}
}

func TestClassifier_ClassifyQuestion_InvalidJSONError(t *testing.T) {
	fake := &fakeLLM{
		classifyFunc: func(ctx context.Context, prompt string) (string, error) {
			return `{"subject":"OS","topics": [{"topic": "Deadlock", "confidence": "bad"}]}`, nil
		},
	}
	c := NewClassifier(fake, 1, 0)
	_, err := c.ClassifyQuestion(context.Background(), "question", "OS")
	if err == nil {
		t.Fatalf("expected error for invalid JSON")
	}
}

func TestClassifier_ClassifyBatch_Success(t *testing.T) {
	fake := &fakeLLM{
		classifyFunc: func(ctx context.Context, prompt string) (string, error) {
			if strings.Contains(prompt, "deadlock") {
				return `{"subject":"Operating Systems","topics":[{"topic":"Deadlock","confidence":0.9}]}`, nil
			}
			if strings.Contains(prompt, "paging") {
				return `{"subject":"Operating Systems","topics":[{"topic":"Paging","confidence":0.8}]}`, nil
			}
			return `{"subject":"Operating Systems","topics":[]}`, nil
		},
	}
	c := NewClassifier(fake, 2, 0)
	items := []BatchItem{
		{QuestionText: "Explain deadlock conditions", Subject: "Operating Systems"},
		{QuestionText: "Explain paging in OS", Subject: "Operating Systems"},
		{QuestionText: "Empty topics question", Subject: "Operating Systems"},
	}
	results, err := c.ClassifyBatch(context.Background(), items)
	if err != nil {
		t.Fatalf("ClassifyBatch: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if len(results[0]) != 1 || results[0][0].Topic != "Deadlock" {
		t.Errorf("batch[0] unexpected %v", results[0])
	}
	if len(results[1]) != 1 || results[1][0].Topic != "Paging" {
		t.Errorf("batch[1] unexpected %v", results[1])
	}
	if len(results[2]) != 0 {
		t.Errorf("batch[2] expected empty, got %v", results[2])
	}
}

func TestClassifier_ClassifyBatch_Retry(t *testing.T) {
	var calls int32
	fake := &fakeLLM{
		classifyFunc: func(ctx context.Context, prompt string) (string, error) {
			n := atomic.AddInt32(&calls, 1)
			if n < 3 {
				return "", fmt.Errorf("transient error %d", n)
			}
			return `{"subject":"Operating Systems","topics":[{"topic":"Deadlock","confidence":0.9}]}`, nil
		},
	}
	c := NewClassifier(fake, 1, 0)
	items := []BatchItem{{QuestionText: "deadlock question", Subject: "Operating Systems"}}
	results, err := c.ClassifyBatch(context.Background(), items)
	if err != nil {
		t.Fatalf("ClassifyBatch with retry should succeed after 3 tries, got err: %v", err)
	}
	if len(results) != 1 || len(results[0]) != 1 {
		t.Fatalf("unexpected results %v", results)
	}
	if fake.CallCount() != 3 {
		t.Errorf("expected 3 calls (retry), got %d", fake.CallCount())
	}

	// Test that after 3 failures it returns error
	calls = 0
	fake2 := &fakeLLM{
		classifyFunc: func(ctx context.Context, prompt string) (string, error) {
			atomic.AddInt32(&calls, 1)
			return "", fmt.Errorf("always fail")
		},
	}
	c2 := NewClassifier(fake2, 1, 0)
	items2 := []BatchItem{{QuestionText: "question", Subject: "OS"}}
	_, err = c2.ClassifyBatch(context.Background(), items2)
	if err == nil {
		t.Fatalf("expected error after 3 retries")
	}
	if fake2.CallCount() != 3 {
		t.Errorf("expected 3 attempts on permanent failure, got %d", fake2.CallCount())
	}
}

func TestClassifier_ClassifyBatch_RetryOnInvalidJSON(t *testing.T) {
	var calls int32
	fake := &fakeLLM{
		classifyFunc: func(ctx context.Context, prompt string) (string, error) {
			n := atomic.AddInt32(&calls, 1)
			if n == 1 {
				return `not json`, nil
			}
			return `{"subject":"OS","topics":[{"topic":"Paging","confidence":0.7}]}`, nil
		},
	}
	c := NewClassifier(fake, 1, 0)
	items := []BatchItem{{QuestionText: "paging question", Subject: "OS"}}
	results, err := c.ClassifyBatch(context.Background(), items)
	if err != nil {
		t.Fatalf("ClassifyBatch retry on invalid JSON: %v", err)
	}
	if len(results[0]) != 1 || results[0][0].Topic != "Paging" {
		t.Errorf("unexpected %v", results[0])
	}
	if calls != 2 {
		t.Errorf("expected 2 calls, got %d", calls)
	}
}

func TestClassifier_ClassifyBatch_Concurrency(t *testing.T) {
	var concurrent int32
	var maxConcurrent int32
	fake := &fakeLLM{
		classifyFunc: func(ctx context.Context, prompt string) (string, error) {
			cur := atomic.AddInt32(&concurrent, 1)
			// track max
			for {
				m := atomic.LoadInt32(&maxConcurrent)
				if cur > m && atomic.CompareAndSwapInt32(&maxConcurrent, m, cur) {
					break
				}
				if cur <= m {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			atomic.AddInt32(&concurrent, -1)
			return `{"subject":"OS","topics":[{"topic":"Deadlock","confidence":0.9}]}`, nil
		},
	}
	c := NewClassifier(fake, 2, 0) // limit 2
	items := make([]BatchItem, 6)
	for i := range items {
		items[i] = BatchItem{QuestionText: fmt.Sprintf("question %d deadlock", i), Subject: "OS"}
	}
	start := time.Now()
	results, err := c.ClassifyBatch(context.Background(), items)
	if err != nil {
		t.Fatalf("ClassifyBatch: %v", err)
	}
	elapsed := time.Since(start)
	if len(results) != 6 {
		t.Fatalf("expected 6 results, got %d", len(results))
	}
	if maxConcurrent > 2 {
		t.Errorf("max concurrency exceeded limit: %d > 2", maxConcurrent)
	}
	// With concurrency 2 and 6 items each 20ms, should take at least 60ms (3 batches)
	if elapsed < 50*time.Millisecond {
		t.Errorf("batch too fast, concurrency not respected? elapsed %v", elapsed)
	}
}

func TestClassifier_MinInterval(t *testing.T) {
	fake := &fakeLLM{
		classifyFunc: func(ctx context.Context, prompt string) (string, error) {
			return `{"subject":"OS","topics":[]}`, nil
		},
	}
	c := NewClassifier(fake, 1, 30*time.Millisecond)
	start := time.Now()
	_, err := c.ClassifyQuestion(context.Background(), "q1", "OS")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	_, err = c.ClassifyQuestion(context.Background(), "q2", "OS")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 30*time.Millisecond {
		t.Errorf("MinInterval not enforced: elapsed %v < 30ms", elapsed)
	}
}

func TestClassifier_InterfaceMocking(t *testing.T) {
	// Verify Classifier implements ClassifierInterface and can be mocked
	var _ ClassifierInterface = (*Classifier)(nil)

	// Mock classifier for unit testing callers
	mock := &mockClassifier{
		labels: []TopicLabel{{Topic: "Deadlock", Confidence: 0.95}},
	}
	var iface ClassifierInterface = mock
	labels, err := iface.ClassifyQuestion(context.Background(), "q", "OS")
	if err != nil {
		t.Fatalf("mock ClassifyQuestion: %v", err)
	}
	if len(labels) != 1 || labels[0].Topic != "Deadlock" {
		t.Errorf("mock unexpected %v", labels)
	}
	batch, err := iface.ClassifyBatch(context.Background(), []BatchItem{{QuestionText: "q", Subject: "OS"}})
	if err != nil {
		t.Fatalf("mock ClassifyBatch: %v", err)
	}
	if len(batch) != 1 {
		t.Errorf("mock batch len=%d", len(batch))
	}
}

// mockClassifier is a simple mock implementing ClassifierInterface
type mockClassifier struct {
	labels []TopicLabel
	err    error
}

func (m *mockClassifier) ClassifyQuestion(ctx context.Context, questionText, subject string) ([]TopicLabel, error) {
	return m.labels, m.err
}
func (m *mockClassifier) ClassifyBatch(ctx context.Context, items []BatchItem) ([][]TopicLabel, error) {
	out := make([][]TopicLabel, len(items))
	for i := range items {
		out[i] = m.labels
	}
	return out, m.err
}
func (m *mockClassifier) LastPromptHash() string { return "mockhash" }
func (m *mockClassifier) LastPrompt() string     { return "mock prompt" }

func TestClassifier_BuildPromptCompact(t *testing.T) {
	fake := &fakeLLM{
		classifyFunc: func(ctx context.Context, prompt string) (string, error) {
			// Ensure prompt is compact: contains subject, question, and strict JSON instructions
			if !strings.Contains(prompt, "Subject hint: Operating Systems") && !strings.Contains(prompt, "Operating Systems") {
				t.Errorf("prompt missing subject")
			}
			if !strings.Contains(prompt, "Explain") {
				t.Errorf("prompt missing question text")
			}
			if !strings.Contains(prompt, "Respond ONLY with JSON") {
				t.Errorf("prompt missing JSON instruction")
			}
			return `{"subject":"Operating Systems","topics":[]}`, nil
		},
	}
	c := NewClassifier(fake, 1, 0)
	_, err := c.ClassifyQuestion(context.Background(), "Explain deadlock", "Operating Systems")
	if err != nil {
		t.Fatalf("ClassifyQuestion: %v", err)
	}
	prompts := fake.Prompts()
	if len(prompts) != 1 {
		t.Fatalf("expected 1 prompt, got %d", len(prompts))
	}
	if len(prompts[0]) > 5000 {
		t.Errorf("prompt too long (not compact): %d", len(prompts[0]))
	}
}

func TestClassifier_ClassifyBatchDetailed_HashPerItem(t *testing.T) {
	fake := &fakeLLM{
		classifyFunc: func(ctx context.Context, prompt string) (string, error) {
			return `{"subject":"OS","topics":[{"topic":"Paging","confidence":0.6}]}`, nil
		},
	}
	c := NewClassifier(fake, 2, 0)
	items := []BatchItem{
		{QuestionText: "paging question", Subject: "OS"},
		{QuestionText: "deadlock question", Subject: "OS"},
	}
	detailed, err := c.ClassifyBatchDetailed(context.Background(), items)
	if err != nil {
		t.Fatalf("ClassifyBatchDetailed: %v", err)
	}
	if len(detailed) != 2 {
		t.Fatalf("expected 2, got %d", len(detailed))
	}
	for i, r := range detailed {
		if r.Error != nil {
			t.Errorf("item %d error: %v", i, r.Error)
		}
		if r.PromptHash == "" {
			t.Errorf("item %d missing prompt hash", i)
		}
		if len(r.PromptHash) != 16 {
			t.Errorf("item %d hash len=%d want 16", i, len(r.PromptHash))
		}
		if len(r.Labels) == 0 {
			t.Errorf("item %d empty labels", i)
		}
	}
	if detailed[0].PromptHash == detailed[1].PromptHash {
		t.Errorf("different questions should have different hashes: %q", detailed[0].PromptHash)
	}
}
