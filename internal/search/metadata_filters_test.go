package search

import (
	"context"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/embeddings"
	"github.com/suryanshu-09/holy_grail/internal/questions"
)

func sIntPtr(n int) *int           { return &n }
func sFloatPtr(f float64) *float64 { return &f }

func unitVec(dim int, idx int) []float32 {
	v := make([]float32, dim)
	v[idx] = 1.0
	return v
}

// seedFilterCorpus builds a fake repository with distinguishable metadata:
// q-os matches "deadlock" queries, q-db matches "paging" queries.
func seedFilterCorpus() *fakeRepository {
	r := newFakeRepository()
	subOS := "Operating Systems"
	subDB := "Databases"
	mcq := "MCQ"
	desc := "descriptive"
	easy := "easy"
	hard := "hard"
	r.questions = []questions.Question{
		{ID: "q-os", DocumentID: "d1", Subject: &subOS, Year: sIntPtr(2020), QuestionType: &mcq, Difficulty: &easy},
		{ID: "q-db", DocumentID: "d2", Subject: &subDB, Year: sIntPtr(2022), QuestionType: &desc, Difficulty: &hard},
	}
	r.vectors["q-os"] = unitVec(embeddings.DefaultDimensions, 0)
	r.vectors["q-db"] = unitVec(embeddings.DefaultDimensions, 1)
	return r
}

func filterService(t *testing.T) *Service {
	t.Helper()
	svc, err := NewService(seedFilterCorpus(), newFakeEmbedder())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}

func filterIDs(resp Response) map[string]bool {
	out := make(map[string]bool, len(resp.Results))
	for _, r := range resp.Results {
		out[r.Question.ID] = true
	}
	return out
}

func TestSearchMetadataFilters(t *testing.T) {
	tests := []struct {
		name    string
		filter  Filter
		want    map[string]bool
		wantErr bool
	}{
		{"subject match", Filter{Subject: "Operating Systems"}, map[string]bool{"q-os": true}, false},
		{"subject no match", Filter{Subject: "Physics"}, map[string]bool{}, false},
		{"exact year", Filter{Year: sIntPtr(2022)}, map[string]bool{"q-db": true}, false},
		{"year range", Filter{YearMin: sIntPtr(2019), YearMax: sIntPtr(2021)}, map[string]bool{"q-os": true}, false},
		{"year min only", Filter{YearMin: sIntPtr(2021)}, map[string]bool{"q-db": true}, false},
		{"year max only", Filter{YearMax: sIntPtr(2021)}, map[string]bool{"q-os": true}, false},
		{"document id", Filter{DocumentID: "d2"}, map[string]bool{"q-db": true}, false},
		{"question type", Filter{QuestionType: "descriptive"}, map[string]bool{"q-db": true}, false},
		{"difficulty", Filter{Difficulty: "easy"}, map[string]bool{"q-os": true}, false},
		{"combined subject and year", Filter{Subject: "Databases", Year: sIntPtr(2022)}, map[string]bool{"q-db": true}, false},
		{"combined contradiction", Filter{Subject: "Databases", Year: sIntPtr(2020)}, map[string]bool{}, false},
		{"threshold passes all", Filter{Threshold: sFloatPtr(0.0)}, map[string]bool{"q-os": true, "q-db": true}, false},
		{"threshold excludes weak", Filter{Threshold: sFloatPtr(0.9)}, map[string]bool{"q-os": true}, false},
		{"threshold negative rejected", Filter{Threshold: sFloatPtr(-0.1)}, nil, true},
		{"threshold above one rejected", Filter{Threshold: sFloatPtr(1.5)}, nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := filterService(t).Search(context.Background(), "deadlock", tc.filter)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Search succeeded, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			got := filterIDs(resp)
			if len(got) != len(tc.want) {
				t.Fatalf("matched = %v, want %v", got, tc.want)
			}
			for id := range tc.want {
				if !got[id] {
					t.Fatalf("matched = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestSearchFilterPaginationClamped(t *testing.T) {
	svc := filterService(t)
	ctx := context.Background()

	resp, err := svc.Search(ctx, "deadlock", Filter{Limit: 0})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp.Count != 2 {
		t.Fatalf("count = %d, want 2 (default limit covers corpus)", resp.Count)
	}

	resp, err = svc.Search(ctx, "deadlock", Filter{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp.Count != 1 {
		t.Fatalf("count = %d, want 1", resp.Count)
	}

	if _, err := svc.Search(ctx, "   ", Filter{}); err == nil {
		t.Fatalf("empty query accepted")
	}
}

func TestSearchMetricPinned(t *testing.T) {
	svc := filterService(t)
	if svc.Metric() != DefaultMetric {
		t.Fatalf("metric = %q, want %q", svc.Metric(), DefaultMetric)
	}
	if svc.Model() == "" {
		t.Fatalf("model is empty")
	}
}
