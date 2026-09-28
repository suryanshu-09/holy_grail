package search

import (
	"strings"
	"testing"
)

func TestRewriteQuery(t *testing.T) {
	rw := RewriteQuery("  What is OS deadlock?  ")
	if rw.Normalized != "what is os deadlock?" {
		t.Errorf("normalized = %q", rw.Normalized)
	}
	if !strings.Contains(rw.Expanded, "operating systems") {
		t.Errorf("expanded should contain synonym, got %q", rw.Expanded)
	}
	for _, w := range strings.Fields(rw.Keyword) {
		if w == "what" || w == "is" {
			t.Errorf("keyword contains stop-word %q in %q", w, rw.Keyword)
		}
	}
	if !strings.Contains(rw.Keyword, "deadlock") {
		t.Errorf("keyword should keep content terms, got %q", rw.Keyword)
	}
	empty := RewriteQuery("   ")
	if empty.Normalized != "" || empty.Keyword != "" {
		t.Errorf("empty rewrite = %+v", empty)
	}
}

func TestBuildQueryVariants(t *testing.T) {
	variants := BuildQueryVariants("OS deadlock", []string{"Synchronization"})
	if len(variants) < 3 {
		t.Fatalf("expected >=3 variants, got %v", variants)
	}
	if variants[0] != "OS deadlock" {
		t.Errorf("first variant should be original, got %q", variants[0])
	}
	foundTopic := false
	for _, v := range variants {
		if strings.Contains(strings.ToLower(v), "synchronization") {
			foundTopic = true
		}
	}
	if !foundTopic {
		t.Errorf("expected topic-expanded variant, got %v", variants)
	}
	seen := map[string]bool{}
	for _, v := range variants {
		if seen[v] {
			t.Errorf("duplicate variant %q", v)
		}
		seen[v] = true
	}
	plain := BuildQueryVariants("paging", nil)
	if len(plain) == 0 {
		t.Errorf("expected non-empty variants")
	}
}

func TestFuseMultiQuery(t *testing.T) {
	a := HybridResult{Question: hybridTestQuestion("q1", "a"), CombinedScore: 0.9, Sources: []string{"vector"}}
	b := HybridResult{Question: hybridTestQuestion("q2", "b"), CombinedScore: 0.8, Sources: []string{"vector"}}
	c := HybridResult{Question: hybridTestQuestion("q2", "b"), CombinedScore: 0.7, Sources: []string{"keyword"}}
	d := HybridResult{Question: hybridTestQuestion("q3", "c"), CombinedScore: 0.6, Sources: []string{"keyword"}}
	fused := FuseMultiQuery([][]HybridResult{{a, b}, {c, d}}, 60)
	if len(fused) != 3 {
		t.Fatalf("expected 3 fused results, got %d", len(fused))
	}
	// q2 appears in both lists (rank 2 then rank 1) so it must outrank q1 (rank 1 only).
	if fused[0].Question.ID != "q2" {
		t.Errorf("expected q2 first after RRF, got %s", fused[0].Question.ID)
	}
	for _, r := range fused {
		if r.Question.ID == "q2" && len(r.Sources) != 2 {
			t.Errorf("q2 sources = %v want union of 2", r.Sources)
		}
	}
	if len(FuseMultiQuery(nil, 0)) != 0 {
		t.Errorf("expected empty fusion for nil input")
	}
}

func TestGroupByDocument(t *testing.T) {
	subj := "Operating Systems"
	q1 := hybridTestQuestion("q1", "deadlock")
	q1.DocumentID = "d1"
	q1.Subject = &subj
	q2 := hybridTestQuestion("q2", "bankers")
	q2.DocumentID = "d1"
	q3 := hybridTestQuestion("q3", "paging")
	q3.DocumentID = "d2"
	results := []HybridResult{
		{Question: q1, CombinedScore: 0.9, Sources: []string{"vector"}},
		{Question: q2, CombinedScore: 0.5, Sources: []string{"keyword"}},
		{Question: q3, CombinedScore: 0.7, Sources: []string{"vector"}},
	}
	resp := GroupByDocument("deadlock", results)
	if resp.Count != 2 {
		t.Fatalf("expected 2 parents, got %d", resp.Count)
	}
	if resp.Parents[0].DocumentID != "d1" {
		t.Errorf("expected d1 first (top 0.9), got %s", resp.Parents[0].DocumentID)
	}
	if resp.Parents[0].Count != 2 {
		t.Errorf("d1 count = %d want 2", resp.Parents[0].Count)
	}
	for _, p := range resp.Parents {
		for _, ch := range p.Children {
			if ch.Context == "" || !strings.Contains(ch.Context, "doc:") {
				t.Errorf("child missing document context: %q", ch.Context)
			}
		}
	}
}

func TestContextualQueryAndEnrich(t *testing.T) {
	q := BuildContextualQuery("deadlock?", "Operating Systems", "Synchronization", "d1")
	if !strings.Contains(q, "[subject: Operating Systems]") || !strings.Contains(q, "deadlock?") {
		t.Errorf("contextual query = %q", q)
	}
	bare := BuildContextualQuery("paging", "", "", "")
	if bare != "paging" {
		t.Errorf("bare contextual query = %q want paging", bare)
	}
	results := []HybridResult{{Question: hybridTestQuestion("q1", "Explain deadlock prevention methods in detail"), CombinedScore: 0.5}}
	enriched := EnrichResults(results)
	if len(enriched) != 1 {
		t.Fatalf("expected 1 enriched hit")
	}
	if enriched[0].ContextSnippet == "" || !strings.Contains(enriched[0].ContextSnippet, "deadlock") {
		t.Errorf("snippet = %q", enriched[0].ContextSnippet)
	}
}

func TestTokenOverlapAndChainedReranker(t *testing.T) {
	results := []HybridResult{
		{Question: hybridTestQuestion("q1", "unrelated scheduling topic"), CombinedScore: 0.9},
		{Question: hybridTestQuestion("q2", "bankers algorithm deadlock"), CombinedScore: 0.5},
	}
	rr := NewTokenOverlapReranker(1.0)
	ranked := rr.Rerank("bankers deadlock", results)
	if ranked[0].Question.ID != "q2" {
		t.Errorf("expected q2 first after overlap rerank, got %s", ranked[0].Question.ID)
	}
	if ranked[0].RerankBoost <= 0 {
		t.Errorf("expected positive boost, got %v", ranked[0].RerankBoost)
	}
	chained := NewChainedReranker(NewExactMatchReranker(1.0), NewTokenOverlapReranker(1.0))
	out := chained.Rerank("bankers", results)
	if len(out) != 2 {
		t.Fatalf("expected 2 results")
	}
	if out[0].Question.ID != "q2" {
		t.Errorf("expected q2 first after chained rerank, got %s", out[0].Question.ID)
	}
	var nilChain *ChainedReranker
	if got := nilChain.Rerank("q", results); len(got) != 2 {
		t.Errorf("nil chained reranker should pass through")
	}
}

func TestAdvancedFilterEmbedsHybrid(t *testing.T) {
	var f AdvancedFilter
	f.Limit = 5
	f.VectorWeight = 0.7
	f.Rewrite = true
	f.MultiQuery = true
	var h HybridFilter = f.HybridFilter
	if h.Limit != 5 {
		t.Errorf("embedded limit = %d want 5", h.Limit)
	}
	if err := normalizeHybridFilter(&f.HybridFilter); err != nil {
		t.Fatalf("normalize embedded filter: %v", err)
	}
}
