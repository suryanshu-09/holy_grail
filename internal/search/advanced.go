package search

import (
	"sort"
	"strings"

	"github.com/suryanshu-09/holy_grail/internal/questions"
)

// AdvancedFilter extends HybridFilter with optional advanced-retrieval flags.
// It embeds HybridFilter so all existing hybrid behavior is preserved; each
// flag only enables an additional preprocessing/postprocessing step.
type AdvancedFilter struct {
	HybridFilter
	Rewrite     bool `json:"rewrite"`
	MultiQuery  bool `json:"multi_query"`
	ParentChild bool `json:"parent_child"`
	Contextual  bool `json:"contextual"`
}

// RewrittenQuery holds the deterministic rewrite products for a raw query.
type RewrittenQuery struct {
	Original   string `json:"original"`
	Normalized string `json:"normalized"`
	Expanded   string `json:"expanded"`
	Keyword    string `json:"keyword"`
}

// querySynonyms maps a normalized token to its expansion phrase.
var querySynonyms = map[string]string{
	"os":   "operating systems",
	"db":   "database",
	"dbms": "database management system",
	"cpu":  "central processing unit",
	"ml":   "machine learning",
	"ai":   "artificial intelligence",
	"cn":   "computer networks",
	"mcq":  "multiple choice question",
}

// lightStopWords is a small stop-word set for the keyword variant.
// Kept intentionally light so content-bearing terms are never dropped.
var lightStopWords = map[string]bool{
	"the": true, "a": true, "an": true, "is": true, "are": true,
	"was": true, "were": true, "what": true, "how": true, "why": true,
	"when": true, "where": true, "which": true, "who": true, "whom": true,
	"of": true, "in": true, "on": true, "for": true, "to": true,
	"and": true, "or": true, "with": true, "about": true, "explain": true,
	"describe": true, "discuss": true, "give": true, "tell": true,
}

// normalizeQuery lowercases, trims, and collapses whitespace.
func normalizeQuery(query string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(query))), " ")
}

// tokenize splits text into lowercase alphanumeric tokens.
func tokenize(text string) []string {
	lower := strings.ToLower(text)
	fields := strings.FieldsFunc(lower, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// expandSynonyms appends synonym phrases for known tokens.
func expandSynonyms(normalized string) string {
	tokens := strings.Fields(normalized)
	if len(tokens) == 0 {
		return normalized
	}
	seen := map[string]bool{}
	extra := make([]string, 0)
	for _, tok := range tokens {
		if phrase, ok := querySynonyms[tok]; ok {
			for _, w := range strings.Fields(phrase) {
				if !seen[w] {
					seen[w] = true
					extra = append(extra, w)
				}
			}
		}
	}
	if len(extra) == 0 {
		return normalized
	}
	return normalized + " " + strings.Join(extra, " ")
}

// keywordVariant drops light stop-words; falls back to normalized when empty.
func keywordVariant(normalized string) string {
	tokens := strings.Fields(normalized)
	kept := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		if !lightStopWords[tok] {
			kept = append(kept, tok)
		}
	}
	if len(kept) == 0 {
		return normalized
	}
	return strings.Join(kept, " ")
}

// RewriteQuery lowercases/trims the query, expands known synonyms, and
// builds a stop-word-light keyword variant.
func RewriteQuery(query string) RewrittenQuery {
	original := strings.TrimSpace(query)
	normalized := normalizeQuery(query)
	return RewrittenQuery{
		Original:   original,
		Normalized: normalized,
		Expanded:   expandSynonyms(normalized),
		Keyword:    keywordVariant(normalized),
	}
}

// BuildQueryVariants returns the retrieval queries for multi-query retrieval:
// the original (trimmed), the rewritten/expanded form, the keyword variant,
// plus one topic-expanded variant per topic ("<query> <topic>").
// Empty and duplicate variants are dropped, preserving first-seen order.
func BuildQueryVariants(query string, topics []string) []string {
	rw := RewriteQuery(query)
	seen := map[string]bool{}
	out := make([]string, 0, 3+len(topics))
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	add(rw.Original)
	add(rw.Expanded)
	add(rw.Keyword)
	base := rw.Normalized
	if base == "" {
		base = rw.Original
	}
	for _, t := range topics {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if base == "" {
			add(t)
		} else {
			add(base + " " + strings.ToLower(t))
		}
	}
	return out
}

// FuseMultiQuery merges per-variant HybridResult lists with reciprocal rank
// fusion: score(doc) = sum over lists of 1/(k+rank). k <= 0 defaults to
// DefaultRRFK. Results are deduplicated by question ID, keep the first-seen
// question payload, union the Sources, and are sorted by fused score
// descending (ties broken by question ID for determinism).
func FuseMultiQuery(lists [][]HybridResult, k int) []HybridResult {
	if k <= 0 {
		k = DefaultRRFK
	}
	type entry struct {
		question questions.Question
		score    float64
		sources  map[string]bool
	}
	merged := map[string]*entry{}
	order := []string{}
	for _, list := range lists {
		for i, r := range list {
			rank := i + 1
			id := r.Question.ID
			e, ok := merged[id]
			if !ok {
				e = &entry{question: r.Question, sources: map[string]bool{}}
				merged[id] = e
				order = append(order, id)
			}
			e.score += 1.0 / float64(k+rank)
			for _, s := range r.Sources {
				e.sources[s] = true
			}
			if len(r.Sources) == 0 {
				e.sources["multi"] = true
			}
		}
	}
	out := make([]HybridResult, 0, len(merged))
	for _, id := range order {
		e := merged[id]
		sources := make([]string, 0, len(e.sources))
		for s := range e.sources {
			sources = append(sources, s)
		}
		sort.Strings(sources)
		out = append(out, HybridResult{
			Question:      e.question,
			CombinedScore: e.score,
			Sources:       sources,
		})
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].CombinedScore == out[b].CombinedScore {
			return out[a].Question.ID < out[b].Question.ID
		}
		return out[a].CombinedScore > out[b].CombinedScore
	})
	return out
}

// ChildHit is a single child result with its parent document context.
type ChildHit struct {
	Question questions.Question `json:"question"`
	Score    float64            `json:"score"`
	Context  string             `json:"context"`
}

// ParentGroup is the parent-level view of one document's child hits.
type ParentGroup struct {
	DocumentID string     `json:"document_id"`
	Count      int        `json:"count"`
	TopScore   float64    `json:"top_score"`
	Children   []ChildHit `json:"children"`
}

// ParentChildResponse groups hybrid hits by parent document.
type ParentChildResponse struct {
	Query   string        `json:"query"`
	Parents []ParentGroup `json:"parents"`
	Count   int           `json:"count"`
}

// DocumentContext builds a short human-readable context line for a question:
// "doc:<id> subject:<s> year:<y> type:<t> difficulty:<d> page:<p>".
// Missing fields are skipped.
func DocumentContext(q questions.Question) string {
	parts := []string{"doc:" + q.DocumentID}
	if q.Subject != nil && strings.TrimSpace(*q.Subject) != "" {
		parts = append(parts, "subject:"+strings.TrimSpace(*q.Subject))
	}
	if q.Year != nil {
		parts = append(parts, "year:"+itoa(*q.Year))
	}
	if q.QuestionType != nil && strings.TrimSpace(*q.QuestionType) != "" {
		parts = append(parts, "type:"+strings.TrimSpace(*q.QuestionType))
	}
	if q.Difficulty != nil && strings.TrimSpace(*q.Difficulty) != "" {
		parts = append(parts, "difficulty:"+strings.TrimSpace(*q.Difficulty))
	}
	if page := questionPage(q); page != "" {
		parts = append(parts, "page:"+page)
	}
	return strings.Join(parts, " ")
}

func questionPage(q questions.Question) string {
	if q.StartPage != nil {
		return itoa(*q.StartPage)
	}
	if q.PageNumber != nil {
		return itoa(*q.PageNumber)
	}
	return ""
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// GroupByDocument groups hybrid results by Question.DocumentID. Parents are
// sorted by TopScore descending; children within a parent are sorted by score
// descending. Each child carries DocumentContext for its question.
func GroupByDocument(query string, results []HybridResult) ParentChildResponse {
	groups := map[string]*ParentGroup{}
	order := []string{}
	for _, r := range results {
		docID := r.Question.DocumentID
		if docID == "" {
			docID = "(unknown)"
		}
		g, ok := groups[docID]
		if !ok {
			g = &ParentGroup{DocumentID: docID}
			groups[docID] = g
			order = append(order, docID)
		}
		g.Children = append(g.Children, ChildHit{
			Question: r.Question,
			Score:    r.CombinedScore,
			Context:  DocumentContext(r.Question),
		})
	}
	parents := make([]ParentGroup, 0, len(groups))
	for _, docID := range order {
		g := groups[docID]
		sort.SliceStable(g.Children, func(a, b int) bool {
			return g.Children[a].Score > g.Children[b].Score
		})
		g.Count = len(g.Children)
		if len(g.Children) > 0 {
			g.TopScore = g.Children[0].Score
		}
		parents = append(parents, *g)
	}
	sort.SliceStable(parents, func(a, b int) bool {
		if parents[a].TopScore == parents[b].TopScore {
			return parents[a].DocumentID < parents[b].DocumentID
		}
		return parents[a].TopScore > parents[b].TopScore
	})
	return ParentChildResponse{Query: query, Parents: parents, Count: len(parents)}
}

// BuildContextualQuery prepends subject/topic/document context to the query
// as bracketed prefixes, e.g. "[subject: OS] [topic: paging] [document: d1]
// <query>". Empty context fields are skipped.
func BuildContextualQuery(query, subject, topic, documentID string) string {
	query = strings.TrimSpace(query)
	prefixes := make([]string, 0, 3)
	if strings.TrimSpace(subject) != "" {
		prefixes = append(prefixes, "[subject: "+strings.TrimSpace(subject)+"]")
	}
	if strings.TrimSpace(topic) != "" {
		prefixes = append(prefixes, "[topic: "+strings.TrimSpace(topic)+"]")
	}
	if strings.TrimSpace(documentID) != "" {
		prefixes = append(prefixes, "[document: "+strings.TrimSpace(documentID)+"]")
	}
	if len(prefixes) == 0 {
		return query
	}
	if query == "" {
		return strings.Join(prefixes, " ")
	}
	return strings.Join(prefixes, " ") + " " + query
}

// ContextHit pairs a hybrid result with a context snippet.
type ContextHit struct {
	HybridResult
	ContextSnippet string `json:"context_snippet"`
}

// BuildSnippet renders a short context snippet for a question from its
// metadata plus a truncated prefix of the question text.
func BuildSnippet(q questions.Question) string {
	ctx := DocumentContext(q)
	text := ""
	if q.QuestionText != nil {
		text = strings.TrimSpace(*q.QuestionText)
	}
	const maxText = 160
	if len(text) > maxText {
		text = text[:maxText-3] + "..."
	}
	if text == "" {
		return ctx
	}
	return ctx + " :: " + text
}

// EnrichResults attaches a context snippet to every hybrid result.
func EnrichResults(results []HybridResult) []ContextHit {
	out := make([]ContextHit, 0, len(results))
	for _, r := range results {
		out = append(out, ContextHit{
			HybridResult:   r,
			ContextSnippet: BuildSnippet(r.Question),
		})
	}
	return out
}

// TokenOverlapReranker is a lightweight cross-encoder-style reranker: it
// scores the token overlap between the query and each result's question text
// (fraction of distinct query tokens present in the result) and adds
// Weight*overlap to CombinedScore, recording the delta in RerankBoost.
type TokenOverlapReranker struct {
	Weight float64
}

// NewTokenOverlapReranker creates the overlap reranker.
func NewTokenOverlapReranker(weight float64) *TokenOverlapReranker {
	return &TokenOverlapReranker{Weight: weight}
}

// OverlapScore returns the fraction of distinct query tokens found in text.
func OverlapScore(query, text string) float64 {
	qTokens := uniqueTokens(tokenize(query))
	if len(qTokens) == 0 {
		return 0
	}
	docSet := map[string]bool{}
	for _, t := range tokenize(text) {
		docSet[t] = true
	}
	hits := 0
	for t := range qTokens {
		if docSet[t] {
			hits++
		}
	}
	return float64(hits) / float64(len(qTokens))
}

func uniqueTokens(tokens []string) map[string]bool {
	out := map[string]bool{}
	for _, t := range tokens {
		out[t] = true
	}
	return out
}

// Rerank implements Reranker.
func (r *TokenOverlapReranker) Rerank(query string, results []HybridResult) []HybridResult {
	if r == nil || r.Weight == 0 {
		return results
	}
	if strings.TrimSpace(query) == "" {
		return results
	}
	out := make([]HybridResult, len(results))
	copy(out, results)
	for i := range out {
		text := ""
		if out[i].Question.QuestionText != nil {
			text = *out[i].Question.QuestionText
		}
		boost := r.Weight * OverlapScore(query, text)
		out[i].CombinedScore += boost
		out[i].RerankBoost += boost
	}
	sort.SliceStable(out, func(a, b int) bool {
		return out[a].CombinedScore > out[b].CombinedScore
	})
	return out
}

// ChainedReranker composes multiple rerankers (e.g. ExactMatchReranker +
// TokenOverlapReranker) applied in order.
type ChainedReranker struct {
	Rerankers []Reranker
}

// NewChainedReranker creates a chained reranker; nil entries are skipped.
func NewChainedReranker(rerankers ...Reranker) *ChainedReranker {
	out := make([]Reranker, 0, len(rerankers))
	for _, r := range rerankers {
		if r != nil {
			out = append(out, r)
		}
	}
	return &ChainedReranker{Rerankers: out}
}

// Rerank implements Reranker by applying each chained reranker in order.
func (c *ChainedReranker) Rerank(query string, results []HybridResult) []HybridResult {
	if c == nil {
		return results
	}
	out := results
	for _, r := range c.Rerankers {
		if r == nil {
			continue
		}
		out = r.Rerank(query, out)
	}
	return out
}
