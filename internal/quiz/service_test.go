package quiz

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/suryanshu-09/holy_grail/internal/questions"
	"github.com/suryanshu-09/holy_grail/internal/search"
)

func qbPtr(s string) *string { return &s }

func qbSources() []questions.Question {
	return []questions.Question{
		{ID: "q1", DocumentID: "d1", QuestionText: qbPtr("Explain the four necessary conditions for deadlock."), Subject: qbPtr("Operating Systems"), Difficulty: qbPtr("medium")},
		{ID: "q2", DocumentID: "d1", QuestionText: qbPtr("What is paging in OS?"), Subject: qbPtr("Operating Systems"), Difficulty: qbPtr("easy")},
		{ID: "q3", DocumentID: "d2", QuestionText: qbPtr("Describe Banker's algorithm with an example."), Subject: qbPtr("Operating Systems"), Difficulty: qbPtr("hard")},
		{ID: "q4", DocumentID: "d2", QuestionText: qbPtr("What is a foreign key?"), Subject: qbPtr("Databases"), Difficulty: qbPtr("easy")},
	}
}

// fakeRetriever records the request it received and returns scripted sources.
type fakeRetriever struct {
	sources []questions.Question
	err     error
	got     []QuizRequest
}

func (f *fakeRetriever) Retrieve(_ context.Context, req QuizRequest) ([]questions.Question, error) {
	f.got = append(f.got, req)
	if f.err != nil {
		return nil, f.err
	}
	return append([]questions.Question{}, f.sources...), nil
}

// fakeLLM replays scripted outputs/errors and captures prompts.
type fakeLLM struct {
	outputs []string
	errs    []error
	prompts []string
	calls   int
}

func (f *fakeLLM) GenerateQuiz(_ context.Context, prompt string) (string, error) {
	f.prompts = append(f.prompts, prompt)
	i := f.calls
	f.calls++
	if i < len(f.errs) && f.errs[i] != nil {
		return "", f.errs[i]
	}
	if i < len(f.outputs) {
		return f.outputs[i], nil
	}
	if len(f.outputs) > 0 {
		return f.outputs[len(f.outputs)-1], nil
	}
	return "", fmt.Errorf("fakeLLM: no outputs scripted")
}

func validQuizJSON(pairs ...[2]string) string {
	// pairs of (sourceID, docID).
	qs := make([]map[string]any, 0, len(pairs))
	for i, p := range pairs {
		qs = append(qs, map[string]any{
			"source_question_id": p[0],
			"document_id":        p[1],
			"question":           fmt.Sprintf("Generated Q%d?", i+1),
			"options":            []string{"Opt A", "Opt B", "Opt C", "Opt D"},
			"correct_answer":     1,
			"explanation":        fmt.Sprintf("Explanation %d grounded in source.", i+1),
		})
	}
	b, _ := json.Marshal(map[string]any{"questions": qs})
	return string(b)
}

func newTestGenerator(ret *fakeRetriever, llm *fakeLLM) *QuizGenerator {
	return &QuizGenerator{
		Retriever:   ret,
		LLM:         llm,
		MaxAttempts: 3,
		Sleep:       func(time.Duration) {},
	}
}

func TestGenerator_SuccessMCQ(t *testing.T) {
	ret := &fakeRetriever{sources: qbSources()}
	llm := &fakeLLM{outputs: []string{validQuizJSON([2]string{"q1", "d1"}, [2]string{"q2", "d1"})}}
	g := newTestGenerator(ret, llm)

	res, err := g.GenerateWithMeta(context.Background(), QuizRequest{Mode: ModeMCQ, NumQuestions: 2})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if res.Fallback {
		t.Fatal("expected LLM path, got fallback")
	}
	if res.Attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", res.Attempts)
	}
	if len(res.Quiz.Questions) != 2 {
		t.Fatalf("expected 2 questions, got %d", len(res.Quiz.Questions))
	}
	if err := ValidateAgainstSources(res.Quiz, qbSources()[:2]); err != nil {
		t.Fatalf("response fails source validation: %v", err)
	}
	for _, q := range res.Quiz.Questions {
		if strings.TrimSpace(q.Explanation) == "" {
			t.Fatal("explanation must be enforced")
		}
		if strings.TrimSpace(q.ID) == "" {
			t.Fatal("quiz IDs must be assigned")
		}
	}
}

func TestGenerator_RetryThenSuccess(t *testing.T) {
	ret := &fakeRetriever{sources: qbSources()}
	llm := &fakeLLM{outputs: []string{"{bad json", validQuizJSON([2]string{"q1", "d1"})}}
	g := newTestGenerator(ret, llm)

	res, err := g.GenerateWithMeta(context.Background(), QuizRequest{Mode: ModeSimilar, NumQuestions: 1})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if res.Fallback || res.Attempts != 2 {
		t.Fatalf("expected success on attempt 2, got fallback=%v attempts=%d", res.Fallback, res.Attempts)
	}
}

func TestGenerator_RejectsMalformedVariants(t *testing.T) {
	cases := map[string]string{
		"bad bounds": `{"questions": [{"source_question_id": "q1", "document_id": "d1", "question": "Q?", "options": ["A","B","C","D"], "correct_answer": 9, "explanation": "E"}]}`,
		"no expl":    `{"questions": [{"source_question_id": "q1", "document_id": "d1", "question": "Q?", "options": ["A","B","C","D"], "correct_answer": 0, "explanation": ""}]}`,
		"bad opts":   `{"questions": [{"source_question_id": "q1", "document_id": "d1", "question": "Q?", "options": ["A","B"], "correct_answer": 0, "explanation": "E"}]}`,
		"unknown":    `{"questions": [{"source_question_id": "zz", "document_id": "d1", "question": "Q?", "options": ["A","B","C","D"], "correct_answer": 0, "explanation": "E"}]}`,
		"dup": `{"questions": [
			{"source_question_id": "q1", "document_id": "d1", "question": "Same?", "options": ["A","B","C","D"], "correct_answer": 0, "explanation": "E"},
			{"source_question_id": "q1", "document_id": "d1", "question": "Other?", "options": ["A","B","C","D"], "correct_answer": 0, "explanation": "E"}]}`,
		"wrong len": validQuizJSON([2]string{"q1", "d1"}, [2]string{"q2", "d1"}, [2]string{"q3", "d2"}),
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			ret := &fakeRetriever{sources: qbSources()[:2]}
			llm := &fakeLLM{outputs: []string{bad, bad, validQuizJSON([2]string{"q1", "d1"}, [2]string{"q2", "d1"})}}
			g := newTestGenerator(ret, llm)
			// With only 2 sources but NumQuestions defaulting to 10, want=min(10,2)=2,
			// so "wrong len" (3 items) is malformed here too.
			req := QuizRequest{Mode: ModeMCQ, NumQuestions: 2}
			res, err := g.GenerateWithMeta(context.Background(), req)
			if err != nil {
				t.Fatalf("Generate failed: %v", err)
			}
			if res.Fallback {
				t.Fatal("expected retry-then-success, got fallback")
			}
			if llm.calls != 3 {
				t.Fatalf("expected 3 LLM calls (2 malformed + 1 good), got %d", llm.calls)
			}
		})
	}
}

func TestGenerator_FallbackOnLLMError(t *testing.T) {
	ret := &fakeRetriever{sources: qbSources()}
	llm := &fakeLLM{errs: []error{fmt.Errorf("boom"), fmt.Errorf("boom"), fmt.Errorf("boom")}}
	g := newTestGenerator(ret, llm)

	res, err := g.GenerateWithMeta(context.Background(), QuizRequest{Mode: ModeMCQ, NumQuestions: 2})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if !res.Fallback {
		t.Fatal("expected fallback after LLM errors")
	}
	if res.Attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", res.Attempts)
	}
	if len(res.Quiz.Questions) != 2 {
		t.Fatalf("expected 2 fallback questions, got %d", len(res.Quiz.Questions))
	}
	srcs := qbSources()[:2]
	if err := ValidateAgainstSources(res.Quiz, srcs); err != nil {
		t.Fatalf("fallback must be valid: %v", err)
	}
	// Fallback uses original wording verbatim.
	if res.Quiz.Questions[0].Question != strings.TrimSpace(*srcs[0].QuestionText) {
		t.Fatalf("fallback must use original PYQ text, got %q", res.Quiz.Questions[0].Question)
	}
}

func TestGenerator_FallbackDeterministic(t *testing.T) {
	mk := func() Result {
		g := newTestGenerator(
			&fakeRetriever{sources: qbSources()},
			&fakeLLM{errs: []error{fmt.Errorf("down"), fmt.Errorf("down"), fmt.Errorf("down")}},
		)
		res, err := g.GenerateWithMeta(context.Background(), QuizRequest{Mode: ModeMixed, NumQuestions: 3})
		if err != nil {
			t.Fatalf("Generate failed: %v", err)
		}
		return res
	}
	a, b := mk(), mk()
	if !reflect.DeepEqual(a.Quiz, b.Quiz) {
		t.Fatal("fallback must be deterministic across calls")
	}
}

func TestGenerator_NilLLMUsesFallback(t *testing.T) {
	g := &QuizGenerator{Retriever: &fakeRetriever{sources: qbSources()}, Sleep: func(time.Duration) {}}
	res, err := g.GenerateWithMeta(context.Background(), QuizRequest{Mode: ModeMCQ, NumQuestions: 2})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if !res.Fallback || res.Attempts != 0 {
		t.Fatalf("nil LLM must fall back without attempts, got %+v", res)
	}
}

func TestGenerator_ModeDispatch(t *testing.T) {
	markers := map[QuizMode]string{
		ModeOriginal: "ORIGINAL",
		ModeMCQ:      "MCQ CONVERSION",
		ModeSimilar:  "SIMILAR QUESTION",
		ModeMixed:    "MIXED QUIZ",
	}
	for mode, marker := range markers {
		ret := &fakeRetriever{sources: qbSources()}
		llm := &fakeLLM{outputs: []string{validQuizJSON([2]string{"q1", "d1"}, [2]string{"q2", "d1"})}}
		g := newTestGenerator(ret, llm)
		_, err := g.Generate(context.Background(), QuizRequest{Mode: mode, NumQuestions: 2})
		if err != nil {
			t.Fatalf("mode %s failed: %v", mode, err)
		}
		if len(llm.prompts) != 1 {
			t.Fatalf("mode %s: expected 1 prompt, got %d", mode, len(llm.prompts))
		}
		if !strings.Contains(llm.prompts[0], marker) {
			t.Fatalf("mode %s prompt missing marker %q", mode, marker)
		}
		if !strings.Contains(llm.prompts[0], "EXACTLY 2") {
			t.Fatalf("mode %s prompt missing length constraint", mode)
		}
	}
}

func TestGenerator_LengthDifficultySubjectFiltering(t *testing.T) {
	ret := &fakeRetriever{sources: qbSources()}
	llm := &fakeLLM{outputs: []string{validQuizJSON([2]string{"q1", "d1"}, [2]string{"q2", "d1"})}}
	g := newTestGenerator(ret, llm)

	req := QuizRequest{Mode: ModeMCQ, NumQuestions: 2, Difficulty: "medium", Topics: []string{"Deadlock"}, Subject: "Operating Systems"}
	if _, err := g.Generate(context.Background(), req); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	// Retriever receives the filters (length/difficulty/topic/subject).
	if len(ret.got) != 1 {
		t.Fatalf("expected 1 retrieve call, got %d", len(ret.got))
	}
	got := ret.got[0]
	if got.NumQuestions != 2 || got.Difficulty != "medium" || got.Subject != "Operating Systems" || len(got.Topics) != 1 {
		t.Fatalf("retriever did not receive filters: %+v", got)
	}

	// In-memory difficulty/subject filtering applies even with a naive retriever.
	g2 := newTestGenerator(
		&fakeRetriever{sources: qbSources()},
		&fakeLLM{errs: []error{fmt.Errorf("down"), fmt.Errorf("down"), fmt.Errorf("down")}},
	)
	res, err := g2.GenerateWithMeta(context.Background(), QuizRequest{Mode: ModeOriginal, NumQuestions: 10, Difficulty: "easy"})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if !res.Fallback {
		t.Fatal("expected fallback")
	}
	if len(res.Quiz.Questions) != 2 { // q2 (easy) + q4 (easy)
		t.Fatalf("expected 2 easy questions, got %d", len(res.Quiz.Questions))
	}
	for _, q := range res.Quiz.Questions {
		if q.SourceQuestionID != "q2" && q.SourceQuestionID != "q4" {
			t.Fatalf("unexpected source %q after difficulty filter", q.SourceQuestionID)
		}
	}
}

func TestGenerator_DuplicateSourcesDeduped(t *testing.T) {
	dup := append(qbSources(), qbSources()[0]) // duplicate q1
	g := newTestGenerator(
		&fakeRetriever{sources: dup},
		&fakeLLM{errs: []error{fmt.Errorf("down"), fmt.Errorf("down"), fmt.Errorf("down")}},
	)
	res, err := g.GenerateWithMeta(context.Background(), QuizRequest{Mode: ModeOriginal, NumQuestions: 10})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	seen := map[string]bool{}
	for _, q := range res.Quiz.Questions {
		if seen[q.SourceQuestionID] {
			t.Fatalf("duplicate source %q in output", q.SourceQuestionID)
		}
		seen[q.SourceQuestionID] = true
	}
	if len(res.Quiz.Questions) != 4 {
		t.Fatalf("expected 4 unique questions, got %d", len(res.Quiz.Questions))
	}
}

func TestGenerator_NoSourcesError(t *testing.T) {
	g := newTestGenerator(&fakeRetriever{sources: nil}, &fakeLLM{})
	if _, err := g.Generate(context.Background(), QuizRequest{Mode: ModeMCQ, NumQuestions: 5}); err == nil {
		t.Fatal("expected error when no sources match")
	}
}

func TestGenerator_InvalidRequest(t *testing.T) {
	g := newTestGenerator(&fakeRetriever{sources: qbSources()}, &fakeLLM{})
	if _, err := g.Generate(context.Background(), QuizRequest{Mode: "invent"}); err == nil {
		t.Fatal("expected validation error for bad mode")
	}
}

func TestGenerator_BackoffBetweenRetries(t *testing.T) {
	var waits []time.Duration
	ret := &fakeRetriever{sources: qbSources()}
	llm := &fakeLLM{errs: []error{fmt.Errorf("e1"), fmt.Errorf("e2")}, outputs: []string{"", "", validQuizJSON([2]string{"q1", "d1"})}}
	g := &QuizGenerator{
		Retriever:   ret,
		LLM:         llm,
		MaxAttempts: 3,
		Backoff:     func(a int) time.Duration { return time.Duration(a) * time.Millisecond },
		Sleep:       func(d time.Duration) { waits = append(waits, d) },
	}
	if _, err := g.Generate(context.Background(), QuizRequest{Mode: ModeMCQ, NumQuestions: 1}); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(waits) != 2 || waits[0] != time.Millisecond || waits[1] != 2*time.Millisecond {
		t.Fatalf("expected backoff waits [1ms 2ms], got %v", waits)
	}
}

func TestFallback_UsesStoredOptions(t *testing.T) {
	opts, _ := json.Marshal([]string{"Dead", "Livelock", "Starvation", "Race"})
	q := questions.Question{ID: "q9", DocumentID: "d9", QuestionText: qbPtr("What is deadlock?"), OptionsJSON: qbPtr(string(opts))}
	resp := BuildOriginalQuiz([]questions.Question{q})
	if len(resp.Questions) != 1 {
		t.Fatalf("expected 1 question, got %d", len(resp.Questions))
	}
	if !reflect.DeepEqual(resp.Questions[0].Options, []string{"Dead", "Livelock", "Starvation", "Race"}) {
		t.Fatalf("fallback should reuse stored options, got %v", resp.Questions[0].Options)
	}
	if err := ValidateAgainstSources(resp, []questions.Question{q}); err != nil {
		t.Fatalf("fallback invalid: %v", err)
	}
}

// fakeHybridSearcher records filters for HybridRetriever tests.
type fakeHybridSearcher struct {
	results []questions.Question
	got     []search.HybridFilter
	gotQ    []string
	err     error
}

func (f *fakeHybridSearcher) Search(_ context.Context, query string, filter search.HybridFilter) (search.HybridResponse, error) {
	f.got = append(f.got, filter)
	f.gotQ = append(f.gotQ, query)
	if f.err != nil {
		return search.HybridResponse{}, f.err
	}
	hits := make([]search.HybridResult, 0, len(f.results))
	for _, q := range f.results {
		hits = append(hits, search.HybridResult{Question: q})
	}
	return search.HybridResponse{Query: query, Results: hits, Count: len(hits)}, nil
}

func TestHybridRetriever_MapsFilters(t *testing.T) {
	hs := &fakeHybridSearcher{results: qbSources()[:2]}
	r := &HybridRetriever{Hybrid: hs}
	got, err := r.Retrieve(context.Background(), QuizRequest{
		Mode: ModeMCQ, NumQuestions: 2, Difficulty: "Medium",
		Topics: []string{"Deadlock"}, Subject: "Operating Systems", Query: "Quiz me on deadlocks",
	})
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
	if len(hs.got) != 1 {
		t.Fatalf("expected 1 hybrid search, got %d", len(hs.got))
	}
	f := hs.got[0]
	if f.Subject != "Operating Systems" || f.Difficulty != "medium" || f.Topic != "Deadlock" || f.Limit != 2 {
		t.Fatalf("filter not mapped: %+v", f.Filter)
	}
	if hs.gotQ[0] != "Quiz me on deadlocks" {
		t.Fatalf("query not passed through: %q", hs.gotQ[0])
	}
}

func TestHybridRetriever_MultiTopicFanout(t *testing.T) {
	hs := &fakeHybridSearcher{results: qbSources()[:2]}
	r := &HybridRetriever{Hybrid: hs}
	got, err := r.Retrieve(context.Background(), QuizRequest{
		Mode: ModeMCQ, NumQuestions: 2, Topics: []string{"Deadlock", "Paging"}, Query: "quiz me",
	})
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}
	if len(hs.got) != 1 {
		// Second branch short-circuits once n=2 is reached after the first
		// branch already returned 2 unique questions.
		t.Fatalf("expected fan-out to stop at n, got %d searches", len(hs.got))
	}
	if len(got) != 2 {
		t.Fatalf("expected truncation to 2, got %d", len(got))
	}
}

// fakeQuestionLister serves QuestionRetriever tests.
type fakeQuestionLister struct {
	all []questions.Question
	got []questions.Filter
}

func (f *fakeQuestionLister) List(_ context.Context, fl questions.Filter) ([]questions.Question, error) {
	f.got = append(f.got, fl)
	return append([]questions.Question{}, f.all...), nil
}

func TestQuestionRetriever_Filters(t *testing.T) {
	lister := &fakeQuestionLister{all: qbSources()}
	r := &QuestionRetriever{
		Questions: lister,
		TopicsForQuestion: func(_ context.Context, id string) ([]string, error) {
			if id == "q1" {
				return []string{"Deadlock"}, nil
			}
			return []string{"Other"}, nil
		},
	}
	got, err := r.Retrieve(context.Background(), QuizRequest{
		Mode: ModeMCQ, NumQuestions: 5, Difficulty: "medium",
		Subject: "Operating Systems", Topics: []string{"deadlock"},
	})
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}
	if len(got) != 1 || got[0].ID != "q1" {
		t.Fatalf("expected only q1 after filters, got %v", got)
	}
	if lister.got[0].Subject != "Operating Systems" {
		t.Fatalf("subject not pushed to list filter: %+v", lister.got[0])
	}
}
