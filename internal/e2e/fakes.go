package e2e

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Script is the canned data the fakes replay through the flow.
type Script struct {
	DocID      string
	Extracted  []Question
	Classified []Question
	QuizItems  []QuizItem
	// FailAt forces the named stage (see Stages) to return FailErr.
	FailAt  string
	FailErr error
}

func (s *Script) failErr(stage string) error {
	if s != nil && s.FailAt == stage {
		if s.FailErr != nil {
			return s.FailErr
		}
		return fmt.Errorf("scripted %s failure", stage)
	}
	return nil
}

func (s *Script) docID() string {
	if s != nil && s.DocID != "" {
		return s.DocID
	}
	return "doc-e2e-1"
}

func cloneQuestions(qs []Question) []Question {
	out := make([]Question, len(qs))
	copy(out, qs)
	for i := range out {
		out[i].Topics = append([]string(nil), qs[i].Topics...)
	}
	return out
}

// FakeUploader validates the PDF payload and returns the scripted document.
type FakeUploader struct {
	Script *Script
	mu     sync.Mutex
	Calls  int
	Got    []UploadInput
}

func (f *FakeUploader) Upload(_ context.Context, in UploadInput) (Document, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls++
	f.Got = append(f.Got, in)
	if err := f.Script.failErr("upload"); err != nil {
		return Document{}, err
	}
	if err := ValidateUploadInput(in); err != nil {
		return Document{}, err
	}
	return Document{ID: f.Script.docID(), Filename: in.Filename, Status: "uploaded"}, nil
}

// FakeExtractor replays the scripted extraction.
type FakeExtractor struct {
	Script *Script
	mu     sync.Mutex
	Calls  int
	Got    []Document
}

func (f *FakeExtractor) Extract(_ context.Context, doc Document) ([]Question, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls++
	f.Got = append(f.Got, doc)
	if err := f.Script.failErr("extract"); err != nil {
		return nil, err
	}
	if doc.ID == "" {
		return nil, fmt.Errorf("missing document id")
	}
	return cloneQuestions(f.Script.Extracted), nil
}

// FakeClassifier replays the scripted classification (topics/subject).
type FakeClassifier struct {
	Script *Script
	mu     sync.Mutex
	Calls  int
}

func (f *FakeClassifier) Classify(_ context.Context, qs []Question) ([]Question, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls++
	if err := f.Script.failErr("classify"); err != nil {
		return nil, err
	}
	return cloneQuestions(f.Script.Classified), nil
}

// FakeEmbedder marks every question embedded.
type FakeEmbedder struct {
	Script *Script
	mu     sync.Mutex
	Calls  int
}

func (f *FakeEmbedder) Embed(_ context.Context, qs []Question) ([]Question, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls++
	if err := f.Script.failErr("embed"); err != nil {
		return nil, err
	}
	out := cloneQuestions(qs)
	for i := range out {
		out[i].Embedded = true
	}
	return out, nil
}

// FakeTopics aggregates topics from classified questions and resolves picks.
type FakeTopics struct {
	Script *Script
	mu     sync.Mutex
	Lists  int
	Picks  int
}

func (f *FakeTopics) ListTopics(_ context.Context, qs []Question) ([]Topic, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Lists++
	if err := f.Script.failErr("list-topics"); err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, q := range qs {
		for _, t := range q.Topics {
			t = strings.TrimSpace(t)
			if t != "" {
				counts[t]++
			}
		}
	}
	var topics []Topic
	for name, n := range counts {
		topics = append(topics, Topic{Name: name, Count: n})
	}
	sort.Slice(topics, func(i, j int) bool { return topics[i].Name < topics[j].Name })
	return topics, nil
}

func (f *FakeTopics) SelectTopic(_ context.Context, topics []Topic, want string) (Topic, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Picks++
	if err := f.Script.failErr("select-topic"); err != nil {
		return Topic{}, err
	}
	want = strings.TrimSpace(want)
	for _, t := range topics {
		if strings.EqualFold(t.Name, want) {
			return t, nil
		}
	}
	return Topic{}, fmt.Errorf("topic %q not available", want)
}

// FakeRetriever filters embedded questions by the selected topic.
type FakeRetriever struct {
	Script *Script
	mu     sync.Mutex
	Calls  int
	Got    []Topic
}

func (f *FakeRetriever) Retrieve(_ context.Context, qs []Question, topic Topic) ([]Question, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls++
	f.Got = append(f.Got, topic)
	if err := f.Script.failErr("retrieve"); err != nil {
		return nil, err
	}
	var out []Question
	for _, q := range qs {
		for _, t := range q.Topics {
			if strings.EqualFold(t, topic.Name) {
				out = append(out, q)
				break
			}
		}
	}
	return out, nil
}

// FakeQuizGenerator builds one quiz item per source (or replays QuizItems).
type FakeQuizGenerator struct {
	Script *Script
	mu     sync.Mutex
	Calls  int
}

func (f *FakeQuizGenerator) Generate(_ context.Context, sources []Question, topic Topic) (Quiz, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls++
	if err := f.Script.failErr("generate-quiz"); err != nil {
		return Quiz{}, err
	}
	items := f.Script.QuizItems
	if len(items) == 0 {
		for i, s := range sources {
			items = append(items, QuizItem{
				ID:            fmt.Sprintf("quiz-item-%d", i+1),
				SourceID:      s.ID,
				Question:      s.Text,
				Options:       []string{"Option A", "Option B", "Option C", "Option D"},
				CorrectAnswer: i % 4,
				Topic:         topic.Name,
			})
		}
	}
	return Quiz{ID: "quiz-e2e-1", Items: append([]QuizItem(nil), items...)}, nil
}

// FakeAnswerer grades answers against each item's CorrectAnswer.
type FakeAnswerer struct {
	Script *Script
	mu     sync.Mutex
	Calls  int
}

func (f *FakeAnswerer) Answer(_ context.Context, quiz Quiz, answers map[string]int) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls++
	if err := f.Script.failErr("answer"); err != nil {
		return Result{}, err
	}
	res := Result{QuizID: quiz.ID, Total: len(quiz.Items), PerItem: make([]bool, len(quiz.Items))}
	byTopic := map[string][]bool{}
	for i, item := range quiz.Items {
		sel, ok := answers[item.ID]
		if !ok {
			return Result{}, fmt.Errorf("missing answer for item %q", item.ID)
		}
		correct := sel == item.CorrectAnswer
		res.PerItem[i] = correct
		if correct {
			res.Correct++
		}
		byTopic[item.Topic] = append(byTopic[item.Topic], correct)
	}
	if res.Total > 0 {
		res.ScorePct = float64(res.Correct) / float64(res.Total) * 100
	}
	for topic, marks := range byTopic {
		hit := 0
		for _, m := range marks {
			if m {
				hit++
			}
		}
		if float64(hit)/float64(len(marks))*100 < 70 {
			res.WeakTopics = append(res.WeakTopics, topic)
		}
	}
	sort.Strings(res.WeakTopics)
	return res, nil
}

// NewFakeDeps wires a full fake stack around one script.
func NewFakeDeps(s *Script) (Deps, *FakeBundle) {
	if s == nil {
		s = &Script{}
	}
	b := &FakeBundle{
		Upload:   &FakeUploader{Script: s},
		Extract:  &FakeExtractor{Script: s},
		Classify: &FakeClassifier{Script: s},
		Embed:    &FakeEmbedder{Script: s},
		Topics:   &FakeTopics{Script: s},
		Retrieve: &FakeRetriever{Script: s},
		Quiz:     &FakeQuizGenerator{Script: s},
		Answer:   &FakeAnswerer{Script: s},
	}
	return Deps{
		Upload: b.Upload, Extract: b.Extract, Classify: b.Classify,
		Embed: b.Embed, Topics: b.Topics, Retrieve: b.Retrieve,
		Quiz: b.Quiz, Answer: b.Answer,
	}, b
}

// FakeBundle exposes every fake for call assertions.
type FakeBundle struct {
	Upload   *FakeUploader
	Extract  *FakeExtractor
	Classify *FakeClassifier
	Embed    *FakeEmbedder
	Topics   *FakeTopics
	Retrieve *FakeRetriever
	Quiz     *FakeQuizGenerator
	Answer   *FakeAnswerer
}
