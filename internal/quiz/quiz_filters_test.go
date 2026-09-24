package quiz

import (
	"context"
	"fmt"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/questions"
)

func intPtr(n int) *int { return &n }

func filterSources() []questions.Question {
	qtMCQ := "mcq"
	qtDesc := "descriptive"
	y2021, y2022, y2023 := 2021, 2022, 2023
	return []questions.Question{
		{ID: "q1", DocumentID: "d1", QuestionText: strPtr("MCQ on deadlocks?"), QuestionType: &qtMCQ, Year: &y2021},
		{ID: "q2", DocumentID: "d1", QuestionText: strPtr("Explain paging in detail."), QuestionType: &qtDesc, Year: &y2022},
		{ID: "q3", DocumentID: "d2", QuestionText: strPtr("MCQ on scheduling?"), QuestionType: &qtMCQ, Year: &y2023},
		{ID: "q4", DocumentID: "d2", QuestionText: strPtr("Undated, untyped question."), Year: nil},
	}
}

func TestRequestValidate_NewFilters(t *testing.T) {
	// Question type is normalized to lowercase.
	r := QuizRequest{Mode: ModeMCQ, QuestionType: " MCQ "}
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	if r.QuestionType != "mcq" {
		t.Fatalf("question type not normalized: %q", r.QuestionType)
	}
	if got := r.NormalizedQuestionType(); got != "mcq" {
		t.Fatalf("NormalizedQuestionType = %q want mcq", got)
	}

	// Valid year range passes.
	r = QuizRequest{Mode: ModeMCQ, YearMin: intPtr(2020), YearMax: intPtr(2023)}
	if err := r.Validate(); err != nil {
		t.Fatalf("valid year range rejected: %v", err)
	}

	// Inverted range fails.
	bad := QuizRequest{Mode: ModeMCQ, YearMin: intPtr(2023), YearMax: intPtr(2020)}
	if err := bad.Validate(); err == nil {
		t.Fatal("year_min > year_max should fail")
	}

	// Negative years fail.
	neg := QuizRequest{Mode: ModeMCQ, YearMin: intPtr(-1)}
	if err := neg.Validate(); err == nil {
		t.Fatal("negative year_min should fail")
	}
	negMax := QuizRequest{Mode: ModeMCQ, YearMax: intPtr(-5)}
	if err := negMax.Validate(); err == nil {
		t.Fatal("negative year_max should fail")
	}

	// OnlyUnseen and OnlyIncorrect are mutually exclusive.
	conflict := QuizRequest{Mode: ModeMCQ, OnlyUnseen: true, OnlyIncorrect: true}
	if err := conflict.Validate(); err == nil {
		t.Fatal("only_unseen + only_incorrect should fail")
	}
	either := QuizRequest{Mode: ModeMCQ, OnlyUnseen: true}
	if err := either.Validate(); err != nil {
		t.Fatalf("only_unseen alone should pass: %v", err)
	}

	// ID lists are trimmed, empties dropped, deduped preserving order.
	ids := QuizRequest{Mode: ModeMCQ, ExcludeSourceIDs: []string{" q1 ", "", "q1", "q2"}, OnlySourceIDs: []string{"q3", "q3", " "}}
	if err := ids.Validate(); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	if len(ids.ExcludeSourceIDs) != 2 || ids.ExcludeSourceIDs[0] != "q1" || ids.ExcludeSourceIDs[1] != "q2" {
		t.Fatalf("exclude IDs not cleaned: %v", ids.ExcludeSourceIDs)
	}
	if len(ids.OnlySourceIDs) != 1 || ids.OnlySourceIDs[0] != "q3" {
		t.Fatalf("only IDs not cleaned: %v", ids.OnlySourceIDs)
	}
}

func TestPrepareSources_NewFilters(t *testing.T) {
	srcs := filterSources()

	// Question type filter (case-insensitive).
	got := prepareSources(srcs, QuizRequest{Mode: ModeMCQ, NumQuestions: 10, QuestionType: "MCQ"})
	if len(got) != 2 || got[0].ID != "q1" || got[1].ID != "q3" {
		t.Fatalf("question_type filter failed: %v", idsOf(got))
	}

	// Year range filter (inclusive); undated questions are dropped.
	got = prepareSources(srcs, QuizRequest{Mode: ModeMCQ, NumQuestions: 10, YearMin: intPtr(2022), YearMax: intPtr(2023)})
	if len(got) != 2 || got[0].ID != "q2" || got[1].ID != "q3" {
		t.Fatalf("year range filter failed: %v", idsOf(got))
	}

	// YearMin only.
	got = prepareSources(srcs, QuizRequest{Mode: ModeMCQ, NumQuestions: 10, YearMin: intPtr(2023)})
	if len(got) != 1 || got[0].ID != "q3" {
		t.Fatalf("year_min filter failed: %v", idsOf(got))
	}

	// Exclude list drops IDs.
	got = prepareSources(srcs, QuizRequest{Mode: ModeMCQ, NumQuestions: 10, ExcludeSourceIDs: []string{"q1", "q3"}})
	if len(got) != 2 || got[0].ID != "q2" || got[1].ID != "q4" {
		t.Fatalf("exclude filter failed: %v", idsOf(got))
	}

	// Only list restricts to members.
	got = prepareSources(srcs, QuizRequest{Mode: ModeMCQ, NumQuestions: 10, OnlySourceIDs: []string{"q2"}})
	if len(got) != 1 || got[0].ID != "q2" {
		t.Fatalf("only filter failed: %v", idsOf(got))
	}

	// Combined filters compose.
	got = prepareSources(srcs, QuizRequest{
		Mode: ModeMCQ, NumQuestions: 10,
		QuestionType: "mcq", YearMin: intPtr(2020), YearMax: intPtr(2022),
		ExcludeSourceIDs: []string{"q3"},
	})
	if len(got) != 1 || got[0].ID != "q1" {
		t.Fatalf("combined filters failed: %v", idsOf(got))
	}
}

func idsOf(qs []questions.Question) []string {
	out := make([]string, 0, len(qs))
	for _, q := range qs {
		out = append(out, q.ID)
	}
	return out
}

func TestHybridRetriever_NewFiltersMapped(t *testing.T) {
	hs := &fakeHybridSearcher{results: filterSources()}
	r := &HybridRetriever{Hybrid: hs}
	_, err := r.Retrieve(context.Background(), QuizRequest{
		Mode: ModeMCQ, NumQuestions: 10, Query: "quiz me",
		QuestionType: "MCQ", YearMin: intPtr(2021), YearMax: intPtr(2022),
	})
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}
	if len(hs.got) != 1 {
		t.Fatalf("expected 1 search, got %d", len(hs.got))
	}
	f := hs.got[0]
	if f.QuestionType != "MCQ" {
		t.Fatalf("question_type not pushed to filter: %q", f.QuestionType)
	}
	if f.YearMin == nil || *f.YearMin != 2021 || f.YearMax == nil || *f.YearMax != 2022 {
		t.Fatalf("year range not pushed to filter: %+v", f.Filter)
	}
}

func TestHybridRetriever_ExcludeOnlyPostFilter(t *testing.T) {
	// The backend ignores metadata filters; the retriever must still enforce
	// exclude/only in memory.
	hs := &fakeHybridSearcher{results: filterSources()}
	r := &HybridRetriever{Hybrid: hs}
	got, err := r.Retrieve(context.Background(), QuizRequest{
		Mode: ModeMCQ, NumQuestions: 10, Query: "quiz me",
		ExcludeSourceIDs: []string{"q1"},
		OnlySourceIDs:    []string{"q2", "q3"},
	})
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}
	if len(got) != 2 || got[0].ID != "q2" || got[1].ID != "q3" {
		t.Fatalf("exclude/only post-filter failed: %v", idsOf(got))
	}
}

func TestQuestionRetriever_NewFilters(t *testing.T) {
	lister := &fakeQuestionLister{all: filterSources()}
	r := &QuestionRetriever{Questions: lister}
	got, err := r.Retrieve(context.Background(), QuizRequest{
		Mode: ModeMCQ, NumQuestions: 10,
		QuestionType: "mcq", YearMin: intPtr(2022),
		ExcludeSourceIDs: []string{"q3"},
	})
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}
	// q1 is mcq but year 2021 < 2022; q3 is mcq in range but excluded.
	if len(got) != 0 {
		t.Fatalf("expected no matches, got %v", idsOf(got))
	}

	got, err = r.Retrieve(context.Background(), QuizRequest{
		Mode: ModeMCQ, NumQuestions: 10, OnlySourceIDs: []string{"q2", "q4"},
	})
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}
	if len(got) != 2 || got[0].ID != "q2" || got[1].ID != "q4" {
		t.Fatalf("only-source filter failed: %v", idsOf(got))
	}
}

func TestGenerator_NewFiltersEndToEnd(t *testing.T) {
	// In-memory filtering in the generator applies even with a naive retriever.
	g := newTestGenerator(
		&fakeRetriever{sources: filterSources()},
		&fakeLLM{errs: []error{fmt.Errorf("down"), fmt.Errorf("down"), fmt.Errorf("down")}},
	)
	res, err := g.GenerateWithMeta(context.Background(), QuizRequest{
		Mode: ModeOriginal, NumQuestions: 10, QuestionType: "mcq",
		YearMin: intPtr(2021), YearMax: intPtr(2021),
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if !res.Fallback {
		t.Fatal("expected fallback")
	}
	if len(res.Quiz.Questions) != 1 || res.Quiz.Questions[0].SourceQuestionID != "q1" {
		t.Fatalf("expected only q1, got %+v", res.Quiz.Questions)
	}
}
