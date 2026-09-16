package extraction

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
	"github.com/suryanshu-09/holy_grail/internal/llm"
	"github.com/suryanshu-09/holy_grail/internal/questions"
	"github.com/suryanshu-09/holy_grail/internal/topics"
)

// fakeLLMForIntegration implements llm.Client for integration test.
type fakeLLMForIntegration struct {
	mu           sync.Mutex
	prompts      []string
	classifyFunc func(ctx context.Context, prompt string) (string, error)
}

func (f *fakeLLMForIntegration) ExtractQuestionsFromText(ctx context.Context, prompt string) (string, error) {
	return "[]", nil
}
func (f *fakeLLMForIntegration) ClassifyTopics(ctx context.Context, prompt string) (string, error) {
	f.mu.Lock()
	f.prompts = append(f.prompts, prompt)
	fn := f.classifyFunc
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx, prompt)
	}
	return `{"subject":"Operating Systems","topics":[]}`, nil
}

func (f *fakeLLMForIntegration) GenerateQuiz(_ context.Context, _ string) (string, error) {
	return `{"questions":[]}`, nil
}

var _ llm.Client = (*fakeLLMForIntegration)(nil)

// memQuestionRepo is an in-memory questions repo for integration test.
type memQuestionRepo struct {
	mu   sync.Mutex
	qs   []questions.Question
	byID map[string]questions.Question
}

func newMemQuestionRepo() *memQuestionRepo {
	return &memQuestionRepo{byID: make(map[string]questions.Question)}
}
func (m *memQuestionRepo) List(_ context.Context, f questions.Filter) ([]questions.Question, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var filtered []questions.Question
	for _, q := range m.qs {
		if f.DocumentID != "" && q.DocumentID != f.DocumentID {
			continue
		}
		if f.Subject != "" {
			if q.Subject == nil || *q.Subject != f.Subject {
				continue
			}
		}
		if f.Year != nil {
			if q.Year == nil || *q.Year != *f.Year {
				continue
			}
		}
		filtered = append(filtered, q)
	}
	if f.Limit == 0 {
		f.Limit = 20
	}
	if f.Limit > 100 {
		f.Limit = 100
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	start := f.Offset
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + f.Limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return append([]questions.Question(nil), filtered[start:end]...), nil
}
func (m *memQuestionRepo) GetByID(_ context.Context, id string) (questions.Question, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if q, ok := m.byID[id]; ok {
		return q, nil
	}
	return questions.Question{}, apperr.ErrNotFound
}
func (m *memQuestionRepo) Insert(_ context.Context, q questions.Question) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.qs = append(m.qs, q)
	m.byID[q.ID] = q
	return nil
}

// memTopicRepo is an in-memory topics repo that implements topics.Repository.
type memTopicRepo struct {
	mu             sync.Mutex
	topics         map[string]topics.Topic
	nameIndex      map[string]string
	questionTopics map[string]map[string]*float64
	seq            int
}

func newMemTopicRepo() *memTopicRepo {
	return &memTopicRepo{
		topics:         make(map[string]topics.Topic),
		nameIndex:      make(map[string]string),
		questionTopics: make(map[string]map[string]*float64),
	}
}

func topicKey(name string, subject *string) string {
	key := strings.ToLower(strings.TrimSpace(name))
	sub := ""
	if subject != nil {
		sub = strings.ToLower(strings.TrimSpace(*subject))
	}
	return key + "|" + sub
}
func (m *memTopicRepo) List(_ context.Context, f topics.Filter) ([]topics.Topic, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []topics.Topic
	for _, t := range m.topics {
		if f.Subject != "" {
			if t.Subject == nil || *t.Subject != f.Subject {
				continue
			}
		}
		out = append(out, t)
	}
	if f.Limit == 0 {
		f.Limit = 20
	}
	if f.Limit > 100 {
		f.Limit = 100
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	start := f.Offset
	if start > len(out) {
		start = len(out)
	}
	end := start + f.Limit
	if end > len(out) {
		end = len(out)
	}
	return out[start:end], nil
}
func (m *memTopicRepo) GetByID(_ context.Context, id string) (topics.Topic, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.topics[id]; ok {
		return t, nil
	}
	return topics.Topic{}, apperr.ErrNotFound
}
func (m *memTopicRepo) Create(_ context.Context, name string, subject *string) (topics.Topic, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return topics.Topic{}, fmt.Errorf("name required")
	}
	key := topicKey(name, subject)
	if id, ok := m.nameIndex[key]; ok {
		return m.topics[id], nil
	}
	m.seq++
	id := fmt.Sprintf("topic-%d", m.seq)
	t := topics.Topic{ID: id, Name: name, Subject: subject, CreatedAt: time.Now()}
	m.topics[id] = t
	m.nameIndex[key] = id
	return t, nil
}
func (m *memTopicRepo) GetByName(_ context.Context, name string, subject *string) (topics.Topic, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := topicKey(name, subject)
	if id, ok := m.nameIndex[key]; ok {
		return m.topics[id], nil
	}
	return topics.Topic{}, apperr.ErrNotFound
}
func (m *memTopicRepo) FindOrCreate(_ context.Context, name string, subject *string) (topics.Topic, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return topics.Topic{}, fmt.Errorf("name required")
	}
	key := topicKey(name, subject)
	if id, ok := m.nameIndex[key]; ok {
		return m.topics[id], nil
	}
	m.seq++
	id := fmt.Sprintf("topic-%d", m.seq)
	t := topics.Topic{ID: id, Name: name, Subject: subject, CreatedAt: time.Now()}
	m.topics[id] = t
	m.nameIndex[key] = id
	return t, nil
}
func (m *memTopicRepo) ListWithCounts(_ context.Context, f topics.Filter) ([]topics.TopicWithCount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	counts := make(map[string]int)
	for _, qMap := range m.questionTopics {
		for tid := range qMap {
			counts[tid]++
		}
	}
	var out []topics.TopicWithCount
	for _, t := range m.topics {
		if f.Subject != "" {
			if t.Subject == nil || *t.Subject != f.Subject {
				continue
			}
		}
		out = append(out, topics.TopicWithCount{Topic: t, QuestionCount: counts[t.ID]})
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].QuestionCount > out[i].QuestionCount {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if f.Limit == 0 {
		f.Limit = 20
	}
	if f.Limit > 100 {
		f.Limit = 100
	}
	start := f.Offset
	if start > len(out) {
		start = len(out)
	}
	end := start + f.Limit
	if end > len(out) {
		end = len(out)
	}
	return out[start:end], nil
}
func (m *memTopicRepo) MergeTopics(_ context.Context, sourceID, targetID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sourceID == "" || targetID == "" {
		return fmt.Errorf("ids required")
	}
	if sourceID == targetID {
		return fmt.Errorf("source and target must differ")
	}
	if _, ok := m.topics[sourceID]; !ok {
		return apperr.ErrNotFound
	}
	if _, ok := m.topics[targetID]; !ok {
		return apperr.ErrNotFound
	}
	for qid, tMap := range m.questionTopics {
		if conf, ok := tMap[sourceID]; ok {
			if _, exists := tMap[targetID]; !exists {
				if m.questionTopics[qid] == nil {
					m.questionTopics[qid] = make(map[string]*float64)
				}
				m.questionTopics[qid][targetID] = conf
			} else {
				existing := tMap[targetID]
				if conf != nil && existing != nil && *conf > *existing {
					m.questionTopics[qid][targetID] = conf
				} else if conf != nil && existing == nil {
					m.questionTopics[qid][targetID] = conf
				}
			}
			delete(m.questionTopics[qid], sourceID)
		}
	}
	for k, id := range m.nameIndex {
		if id == sourceID {
			delete(m.nameIndex, k)
			break
		}
	}
	delete(m.topics, sourceID)
	return nil
}
func (m *memTopicRepo) AddQuestionTopic(_ context.Context, questionID, topicID string, confidence *float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if questionID == "" || topicID == "" {
		return fmt.Errorf("ids required")
	}
	if m.questionTopics[questionID] == nil {
		m.questionTopics[questionID] = make(map[string]*float64)
	}
	m.questionTopics[questionID][topicID] = confidence
	return nil
}
func (m *memTopicRepo) RemoveQuestionTopic(_ context.Context, questionID, topicID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.questionTopics[questionID] != nil {
		delete(m.questionTopics[questionID], topicID)
	}
	return nil
}
func (m *memTopicRepo) ListTopicsForQuestion(_ context.Context, questionID string) ([]topics.Topic, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tMap := m.questionTopics[questionID]
	if tMap == nil {
		return []topics.Topic{}, nil
	}
	var out []topics.Topic
	for tid := range tMap {
		if t, ok := m.topics[tid]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}
func (m *memTopicRepo) ListQuestionTopics(_ context.Context, questionID string) ([]topics.QuestionTopic, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tMap := m.questionTopics[questionID]
	var out []topics.QuestionTopic
	for tid, conf := range tMap {
		out = append(out, topics.QuestionTopic{QuestionID: questionID, TopicID: tid, Confidence: conf})
	}
	return out, nil
}
func (m *memTopicRepo) SetQuestionTopics(_ context.Context, questionID string, topicIDs []string, confidences map[string]*float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if questionID == "" {
		return fmt.Errorf("questionID required")
	}
	m.questionTopics[questionID] = make(map[string]*float64)
	for _, tid := range topicIDs {
		var conf *float64
		if confidences != nil {
			conf = confidences[tid]
		}
		m.questionTopics[questionID][tid] = conf
	}
	return nil
}

func TestPhase9_FullFlow_ExtractClassifyCountsCorrectionMerge(t *testing.T) {
	root := t.TempDir()
	storeTestPDF(t, root, "doc-phase9", buildTestPDF(t, []string{
		textStream("Q1. Explain the four necessary conditions for deadlock and discuss methods to prevent it."),
		textStream("Q2. Explain paging and page replacement algorithms in OS."),
		textStream("Q3. What is CPU scheduling? Compare FCFS and SJF."),
		textStream("Q4. Explain synchronization using semaphores and critical section."),
	}))

	extractor, err := NewService(root)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	docRepo := newFakeRepo("doc-phase9")
	qRepo := newMemQuestionRepo()
	svc, err := NewExtractionService(root, extractor, docRepo, qRepo)
	if err != nil {
		t.Fatalf("NewExtractionService: %v", err)
	}

	de, err := svc.Extract(context.Background(), "doc-phase9", "")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if de.PageCount != 4 {
		t.Fatalf("PageCount=%d want 4", de.PageCount)
	}
	qs, _ := qRepo.List(context.Background(), questions.Filter{DocumentID: "doc-phase9", Limit: 100})
	if len(qs) != 4 {
		t.Fatalf("expected 4 questions after extract, got %d", len(qs))
	}

	fake := &fakeLLMForIntegration{
		classifyFunc: func(_ context.Context, prompt string) (string, error) {
			lower := strings.ToLower(prompt)
			switch {
			case strings.Contains(lower, "deadlock"):
				return `{"subject":"Operating Systems","topics":[{"topic":"Deadlock","confidence":0.92}]}`, nil
			case strings.Contains(lower, "paging"):
				return `{"subject":"Operating Systems","topics":[{"topic":"Paging","confidence":0.88}]}`, nil
			case strings.Contains(lower, "cpu scheduling") || strings.Contains(lower, "fcfs"):
				return `{"subject":"Operating Systems","topics":[{"topic":"CPU Scheduling","confidence":0.85}]}`, nil
			case strings.Contains(lower, "synchronization") || strings.Contains(lower, "semaphore"):
				return `{"subject":"Operating Systems","topics":[{"topic":"Synchronization","confidence":0.87}]}`, nil
			default:
				return `{"subject":"Operating Systems","topics":[]}`, nil
			}
		},
	}
	classifier := topics.NewClassifier(fake, 2, 0)
	memRepo := newMemTopicRepo()

	svc = svc.WithTopicClassifier(classifier, memRepo)

	if err := svc.ClassifyDocument(context.Background(), "doc-phase9"); err != nil {
		t.Fatalf("ClassifyDocument: %v", err)
	}

	topicSvc := topics.NewService(memRepo)
	tcs, err := topicSvc.ListWithCounts(context.Background(), topics.Filter{Limit: 100})
	if err != nil {
		t.Fatalf("ListWithCounts: %v", err)
	}
	if len(tcs) != 4 {
		t.Fatalf("expected 4 topics with counts, got %d: %+v", len(tcs), tcs)
	}
	countMap := make(map[string]int)
	for _, tc := range tcs {
		countMap[tc.Name] = tc.QuestionCount
		t.Logf("%s %d questions", tc.Name, tc.QuestionCount)
	}
	for _, want := range []string{"Deadlock", "Paging", "CPU Scheduling", "Synchronization"} {
		if c, ok := countMap[want]; !ok || c != 1 {
			t.Errorf("topic %q count=%d want 1", want, c)
		}
	}

	for _, q := range qs {
		ts, err := topicSvc.ListTopicsForQuestion(context.Background(), q.ID)
		if err != nil {
			t.Fatalf("ListTopicsForQuestion %s: %v", q.ID, err)
		}
		if len(ts) != 1 {
			t.Errorf("question %q expected 1 topic, got %d: %+v", *q.QuestionText, len(ts), ts)
		}
		if len(ts) == 1 {
			t.Logf("question %q -> topic %q", *q.QuestionText, ts[0].Name)
		}
	}

	var deadlockQID string
	var pagingTID string
	for _, q := range qs {
		ts, _ := topicSvc.ListTopicsForQuestion(context.Background(), q.ID)
		if len(ts) == 1 && ts[0].Name == "Deadlock" {
			deadlockQID = q.ID
		}
		if len(ts) == 1 && ts[0].Name == "Paging" {
			pagingTID = ts[0].ID
		}
	}
	if deadlockQID == "" || pagingTID == "" {
		t.Fatalf("could not find deadlock and paging questions for correction test")
	}
	updated, err := topicSvc.CorrectQuestionTopics(context.Background(), deadlockQID, []string{pagingTID})
	if err != nil {
		t.Fatalf("CorrectQuestionTopics: %v", err)
	}
	if len(updated) != 1 || updated[0].Name != "Paging" {
		t.Errorf("correction result unexpected: %+v", updated)
	}
	tcs2, _ := topicSvc.ListWithCounts(context.Background(), topics.Filter{Limit: 100})
	countMap2 := make(map[string]int)
	for _, tc := range tcs2 {
		countMap2[tc.Name] = tc.QuestionCount
	}
	if countMap2["Paging"] != 2 {
		t.Errorf("after correction Paging count=%d want 2", countMap2["Paging"])
	}
	if countMap2["Deadlock"] != 0 {
		t.Errorf("after correction Deadlock count=%d want 0", countMap2["Deadlock"])
	}
	tsAfter, _ := topicSvc.ListTopicsForQuestion(context.Background(), deadlockQID)
	if len(tsAfter) != 1 || tsAfter[0].Name != "Paging" {
		t.Errorf("after correction topics for question %s = %+v want Paging", deadlockQID, tsAfter)
	}

	dup, err := memRepo.FindOrCreate(context.Background(), "Deadlocks", strPtr("Operating Systems"))
	if err != nil {
		t.Fatalf("Create duplicate: %v", err)
	}
	dup2, _ := memRepo.FindOrCreate(context.Background(), "Deadlock Problem", strPtr("Operating Systems"))
	var originalDeadlockID string
	for _, tc := range tcs2 {
		if tc.Name == "Deadlock" {
			originalDeadlockID = tc.ID
			break
		}
	}
	if originalDeadlockID == "" {
		all, _ := topicSvc.List(context.Background(), topics.Filter{Limit: 100})
		for _, tt := range all {
			if tt.Name == "Deadlock" {
				originalDeadlockID = tt.ID
				break
			}
		}
	}
	if originalDeadlockID == "" {
		t.Fatalf("original Deadlock topic not found for merge test")
	}
	var cpuQID string
	for _, q := range qs {
		ts, _ := topicSvc.ListTopicsForQuestion(context.Background(), q.ID)
		for _, tt := range ts {
			if tt.Name == "CPU Scheduling" {
				cpuQID = q.ID
				break
			}
		}
	}
	if cpuQID == "" {
		t.Fatalf("CPU Scheduling question not found")
	}
	_ = memRepo.AddQuestionTopic(context.Background(), cpuQID, dup.ID, floatPtr(0.6))
	_ = memRepo.AddQuestionTopic(context.Background(), cpuQID, dup2.ID, floatPtr(0.5))

	if err := topicSvc.MergeDuplicateTopics(context.Background(), originalDeadlockID, []string{dup.ID, dup2.ID}); err != nil {
		t.Fatalf("MergeDuplicateTopics: %v", err)
	}
	if _, err := topicSvc.Get(context.Background(), dup.ID); err == nil {
		t.Errorf("duplicate topic %s still exists after merge", dup.ID)
	}
	if _, err := topicSvc.Get(context.Background(), dup2.ID); err == nil {
		t.Errorf("duplicate topic %s still exists after merge", dup2.ID)
	}
	tcs3, _ := topicSvc.ListWithCounts(context.Background(), topics.Filter{Limit: 100})
	for _, tc := range tcs3 {
		if tc.ID == originalDeadlockID {
			if tc.QuestionCount == 0 {
				t.Errorf("merged Deadlock count still 0, want >0")
			}
			t.Logf("after merge Deadlock %d questions", tc.QuestionCount)
		}
	}
	tsMerged, _ := topicSvc.ListTopicsForQuestion(context.Background(), cpuQID)
	foundDeadlock := false
	foundCPU := false
	for _, tt := range tsMerged {
		if tt.Name == "Deadlock" {
			foundDeadlock = true
		}
		if tt.Name == "CPU Scheduling" {
			foundCPU = true
		}
	}
	if !foundDeadlock || !foundCPU {
		t.Errorf("after merge cpu question topics = %+v want Deadlock and CPU Scheduling", tsMerged)
	}
}

func strPtr(s string) *string { return &s }
func floatPtr(f float64) *float64 { return &f }
