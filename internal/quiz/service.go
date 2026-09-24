package quiz

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/suryanshu-09/holy_grail/internal/questions"
	"github.com/suryanshu-09/holy_grail/internal/search"
)

// DefaultMaxAttempts is the number of LLM attempts before falling back to
// deterministic Original PYQs (initial try + retries).
const DefaultMaxAttempts = 3

// Retriever supplies the source PYQs a quiz is generated from.
//
// Production implementations are backed by the existing retrieval stack:
//   - HybridRetriever wraps the hybrid (vector + keyword + metadata) search.
//   - QuestionRetriever wraps the questions list API with optional topic
//     resolution via the topics service.
//
// The retriever is expected to honor QuizRequest filtering (length as the
// retrieval top-K, difficulty, topics, subject, question type, year range,
// exclude/only source IDs) on a best-effort basis; the
// generator additionally applies deterministic in-memory filtering for
// difficulty/subject so quiz length and filtering hold even with a naive
// retriever (e.g. fakes in tests).
type Retriever interface {
	Retrieve(ctx context.Context, req QuizRequest) ([]questions.Question, error)
}

// RetrieveFunc adapts a plain function to a Retriever.
type RetrieveFunc func(ctx context.Context, req QuizRequest) ([]questions.Question, error)

// Retrieve implements Retriever.
func (f RetrieveFunc) Retrieve(ctx context.Context, req QuizRequest) ([]questions.Question, error) {
	return f(ctx, req)
}

// QuizLLM generates the structured quiz JSON for a prompt built by
// BuildQuizPrompt. The returned string is raw LLM output that the generator
// strictly parses with ParseQuizResponse (malformed output is rejected and
// retried, never silently repaired).
type QuizLLM interface {
	GenerateQuiz(ctx context.Context, prompt string) (string, error)
}

// LLMFunc adapts a plain function to a QuizLLM.
type LLMFunc func(ctx context.Context, prompt string) (string, error)

// GenerateQuiz implements QuizLLM.
func (f LLMFunc) GenerateQuiz(ctx context.Context, prompt string) (string, error) {
	return f(ctx, prompt)
}

// HybridSearcher is the subset of search.HybridService used by HybridRetriever.
// *search.HybridService satisfies it, so it can be wired directly.
type HybridSearcher interface {
	Search(ctx context.Context, query string, filter search.HybridFilter) (search.HybridResponse, error)
}

// HybridRetriever backs quiz retrieval with hybrid (vector + keyword +
// metadata) search. Topic filtering fans out one search per requested topic
// (the search filter carries a single topic) and merges deterministically;
// difficulty/subject are pushed into the search filter and re-checked
// in-memory by the generator.
type HybridRetriever struct {
	Hybrid HybridSearcher
}

// Retrieve implements Retriever.
func (r *HybridRetriever) Retrieve(ctx context.Context, req QuizRequest) ([]questions.Question, error) {
	if r == nil || r.Hybrid == nil {
		return nil, fmt.Errorf("quiz: hybrid searcher is required")
	}
	n := req.NumQuestions
	if n <= 0 {
		n = DefaultNumQuestions
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		query = strings.Join(req.Topics, " ")
	}
	if query == "" {
		query = strings.TrimSpace(req.Subject)
	}
	if query == "" {
		return nil, fmt.Errorf("quiz: hybrid retrieval needs a query, topic, or subject")
	}

	topics := distinctNonEmpty(req.Topics)
	if len(topics) == 0 {
		got, err := r.searchOnce(ctx, query, "", req, n)
		if err != nil {
			return nil, err
		}
		return applyIDFilters(got, req), nil
	}
	// One branch per topic (OR semantics), merged deterministically.
	seen := make(map[string]struct{})
	out := make([]questions.Question, 0, n)
	for _, t := range topics {
		got, err := r.searchOnce(ctx, query, t, req, n)
		if err != nil {
			return nil, err
		}
		for _, q := range got {
			if _, dup := seen[q.ID]; dup {
				continue
			}
			seen[q.ID] = struct{}{}
			out = append(out, q)
			if len(out) >= n {
				return applyIDFilters(out, req), nil
			}
		}
	}
	return applyIDFilters(out, req), nil
}

func (r *HybridRetriever) searchOnce(ctx context.Context, query, topic string, req QuizRequest, n int) ([]questions.Question, error) {
	filter := search.HybridFilter{}
	filter.Filter.Subject = strings.TrimSpace(req.Subject)
	filter.Filter.Difficulty = strings.ToLower(strings.TrimSpace(req.Difficulty))
	filter.Filter.Topic = strings.TrimSpace(topic)
	filter.Filter.QuestionType = strings.TrimSpace(req.QuestionType)
	filter.Filter.YearMin = req.YearMin
	filter.Filter.YearMax = req.YearMax
	filter.Filter.Limit = n
	resp, err := r.Hybrid.Search(ctx, query, filter)
	if err != nil {
		return nil, fmt.Errorf("quiz: hybrid retrieval: %w", err)
	}
	out := make([]questions.Question, 0, len(resp.Results))
	for _, hit := range resp.Results {
		out = append(out, hit.Question)
	}
	return out, nil
}

// QuestionLister is the subset of questions.Service used by QuestionRetriever.
// *questions.Service satisfies it, so it can be wired directly.
type QuestionLister interface {
	List(ctx context.Context, f questions.Filter) ([]questions.Question, error)
}

// QuestionRetriever backs quiz retrieval with the questions list API. Subject
// is pushed into the list filter; difficulty is applied in-memory (the list
// filter has no difficulty field). Topic filtering needs TopicsForQuestion
// (backed by the topics service); when nil, topic filtering is skipped here
// and left to the caller.
type QuestionRetriever struct {
	Questions          QuestionLister
	TopicsForQuestion func(ctx context.Context, questionID string) ([]string, error)
}

// Retrieve implements Retriever.
func (r *QuestionRetriever) Retrieve(ctx context.Context, req QuizRequest) ([]questions.Question, error) {
	if r == nil || r.Questions == nil {
		return nil, fmt.Errorf("quiz: questions lister is required")
	}
	n := req.NumQuestions
	if n <= 0 {
		n = DefaultNumQuestions
	}
	// Over-fetch so in-memory difficulty/topic filtering can still fill the quiz.
	overfetch := n * 3
	if overfetch < n+10 {
		overfetch = n + 10
	}
	if overfetch > 100 {
		overfetch = 100
	}
	got, err := r.Questions.List(ctx, questions.Filter{
		Subject: strings.TrimSpace(req.Subject),
		Limit:   overfetch,
	})
	if err != nil {
		return nil, fmt.Errorf("quiz: list questions: %w", err)
	}
	wantTopics := distinctLower(req.Topics)
	exclude := toIDSet(req.ExcludeSourceIDs)
	only := toIDSet(req.OnlySourceIDs)
	filtered := make([]questions.Question, 0, n)
	for _, q := range got {
		if !matchesDifficulty(q, req.Difficulty) {
			continue
		}
		if !matchesQuestionType(q, req.QuestionType) {
			continue
		}
		if !matchesYearRange(q, req.YearMin, req.YearMax) {
			continue
		}
		if _, bad := exclude[strings.TrimSpace(q.ID)]; bad {
			continue
		}
		if len(only) > 0 {
			if _, ok := only[strings.TrimSpace(q.ID)]; !ok {
				continue
			}
		}
		if len(wantTopics) > 0 {
			if r.TopicsForQuestion == nil {
				continue
			}
			names, err := r.TopicsForQuestion(ctx, q.ID)
			if err != nil {
				return nil, fmt.Errorf("quiz: resolve topics for %q: %w", q.ID, err)
			}
			if !matchesAnyTopic(names, wantTopics) {
				continue
			}
		}
		filtered = append(filtered, q)
		if len(filtered) >= n {
			break
		}
	}
	return filtered, nil
}

// QuizGenerator generates quizzes from retrieved PYQs.
//
// Pipeline: validate request -> retrieve sources -> deterministic in-memory
// filtering (length/difficulty/subject, dedupe by source ID) -> per-mode
// prompt dispatch (BuildQuizPrompt) -> LLM with retries (malformed output is
// rejected, never repaired) -> deterministic fallback to Original PYQs when
// the LLM is unavailable or keeps returning malformed output.
type QuizGenerator struct {
	Retriever Retriever
	LLM       QuizLLM // nil means "no LLM": always use the deterministic fallback.

	// MaxAttempts is the total LLM tries before falling back. <=0 means DefaultMaxAttempts.
	MaxAttempts int
	// Backoff maps the 1-based failed attempt number to the wait before the
	// next try. Nil means DefaultBackoff.
	Backoff func(failedAttempt int) time.Duration
	// Sleep waits between attempts. Nil means time.Sleep; tests inject a no-op.
	// It must honor ctx cancellation (the default select does).
	Sleep func(time.Duration)
}

// DefaultBackoff waits 100ms, 200ms, 400ms, ... between attempts.
func DefaultBackoff(failedAttempt int) time.Duration {
	if failedAttempt < 1 {
		failedAttempt = 1
	}
	return time.Duration(100<<uint(failedAttempt-1)) * time.Millisecond
}

// Result is the outcome of GenerateWithMeta.
type Result struct {
	Quiz      QuizResponse
	Fallback  bool // true when the deterministic Original-PYQ fallback was used.
	Attempts  int  // LLM attempts made (0 when LLM is nil or mode skips it).
}

func (g *QuizGenerator) maxAttempts() int {
	if g == nil || g.MaxAttempts <= 0 {
		return DefaultMaxAttempts
	}
	return g.MaxAttempts
}

func (g *QuizGenerator) backoff() func(int) time.Duration {
	if g != nil && g.Backoff != nil {
		return g.Backoff
	}
	return DefaultBackoff
}

func (g *QuizGenerator) sleep(d time.Duration) {
	if g != nil && g.Sleep != nil {
		g.Sleep(d)
		return
	}
	time.Sleep(d)
}

// Generate builds a quiz for req, falling back to Original PYQs when needed.
func (g *QuizGenerator) Generate(ctx context.Context, req QuizRequest) (QuizResponse, error) {
	res, err := g.GenerateWithMeta(ctx, req)
	if err != nil {
		return QuizResponse{}, err
	}
	return res.Quiz, nil
}

// GenerateWithMeta is Generate plus fallback/attempt metadata.
func (g *QuizGenerator) GenerateWithMeta(ctx context.Context, req QuizRequest) (Result, error) {
	if g == nil || g.Retriever == nil {
		return Result{}, fmt.Errorf("quiz: retriever is required")
	}
	r := req
	if err := r.Validate(); err != nil {
		return Result{}, err
	}
	sources, err := g.Retriever.Retrieve(ctx, r)
	if err != nil {
		return Result{}, err
	}
	// Deterministic in-memory filtering: dedupe by source ID (keep first),
	// drop empty stems, enforce difficulty/subject. Topic filtering is
	// retriever-side (the question record carries no topic names).
	sources = prepareSources(sources, r)
	if len(sources) == 0 {
		return Result{}, fmt.Errorf("quiz: no source questions match the requested filters")
	}
	want := r.NumQuestions
	if want > len(sources) {
		want = len(sources)
	}
	promptSources := sources[:want]

	promptReq := r
	promptReq.NumQuestions = want
	prompt, err := BuildQuizPrompt(promptReq, promptSources)
	if err != nil {
		return Result{}, err
	}

	if g.LLM == nil {
		return Result{Quiz: BuildOriginalQuiz(promptSources), Fallback: true}, nil
	}

	backoff := g.backoff()
	var lastErr error
	for attempt := 1; attempt <= g.maxAttempts(); attempt++ {
		raw, err := g.LLM.GenerateQuiz(ctx, prompt)
		if err != nil {
			lastErr = fmt.Errorf("quiz: llm attempt %d: %w", attempt, err)
		} else {
			resp, perr := ParseQuizResponse(raw, promptSources)
			if perr != nil {
				lastErr = fmt.Errorf("quiz: llm attempt %d malformed: %w", attempt, perr)
			} else if len(resp.Questions) != want {
				lastErr = fmt.Errorf("quiz: llm attempt %d malformed: got %d questions, want %d", attempt, len(resp.Questions), want)
			} else {
				assignQuizIDs(resp.Questions)
				return Result{Quiz: resp, Attempts: attempt}, nil
			}
		}
		if attempt < g.maxAttempts() {
			wait := backoff(attempt)
			if wait > 0 {
				select {
				case <-ctx.Done():
					return Result{}, ctx.Err()
				default:
				}
				g.sleep(wait)
			}
		}
	}
	// Deterministic fallback: Original PYQs, always valid by construction.
	fallback := BuildOriginalQuiz(promptSources)
	_ = lastErr
	return Result{Quiz: fallback, Fallback: true, Attempts: g.maxAttempts()}, nil
}

// prepareSources dedupes by source ID (first wins), drops questions with an
// empty ID or empty stem, and applies difficulty/subject/question-type/year/
// exclude/only-source filters. Order is preserved so retrieval ranking (and
// the fallback) stays deterministic.
func prepareSources(srcs []questions.Question, req QuizRequest) []questions.Question {
	seen := make(map[string]struct{}, len(srcs))
	exclude := toIDSet(req.ExcludeSourceIDs)
	only := toIDSet(req.OnlySourceIDs)
	out := make([]questions.Question, 0, len(srcs))
	for _, q := range srcs {
		if strings.TrimSpace(q.ID) == "" {
			continue
		}
		if _, dup := seen[q.ID]; dup {
			continue
		}
		seen[q.ID] = struct{}{}
		if strings.TrimSpace(questionText(q)) == "" {
			continue
		}
		if !matchesDifficulty(q, req.Difficulty) {
			continue
		}
		if !matchesSubject(q, req.Subject) {
			continue
		}
		if !matchesQuestionType(q, req.QuestionType) {
			continue
		}
		if !matchesYearRange(q, req.YearMin, req.YearMax) {
			continue
		}
		if _, bad := exclude[q.ID]; bad {
			continue
		}
		if len(only) > 0 {
			if _, ok := only[q.ID]; !ok {
				continue
			}
		}
		out = append(out, q)
	}
	return out
}

func questionText(q questions.Question) string {
	if q.QuestionText == nil {
		return ""
	}
	return strings.TrimSpace(*q.QuestionText)
}

// matchesDifficulty reports whether q satisfies the requested difficulty
// (case-insensitive; empty means any).
func matchesDifficulty(q questions.Question, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	if want == "" {
		return true
	}
	if q.Difficulty == nil {
		return false
	}
	return strings.ToLower(strings.TrimSpace(*q.Difficulty)) == want
}

// matchesSubject reports whether q satisfies the requested subject
// (case-insensitive; empty means any).
func matchesSubject(q questions.Question, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return true
	}
	if q.Subject == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(*q.Subject), want)
}

// matchesQuestionType reports whether q satisfies the requested question
// type (case-insensitive; empty means any). Questions without a recorded
// type only match the unfiltered request.
func matchesQuestionType(q questions.Question, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	if want == "" {
		return true
	}
	if q.QuestionType == nil {
		return false
	}
	return strings.ToLower(strings.TrimSpace(*q.QuestionType)) == want
}

// matchesYearRange reports whether q.Year falls inside the inclusive
// [min,max] bounds. When a bound is set, questions without a recorded year
// do not match.
func matchesYearRange(q questions.Question, min, max *int) bool {
	if min == nil && max == nil {
		return true
	}
	if q.Year == nil {
		return false
	}
	if min != nil && *q.Year < *min {
		return false
	}
	if max != nil && *q.Year > *max {
		return false
	}
	return true
}

// toIDSet builds a lookup set from ID lists (IDs are used verbatim;
// Validate trims/dedupes them on the request path).
func toIDSet(ids []string) map[string]struct{} {
	out := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) != "" {
			out[id] = struct{}{}
		}
	}
	return out
}

// applyIDFilters enforces ExcludeSourceIDs/OnlySourceIDs on retriever output
// for backends that ignore those metadata filters. Order is preserved.
func applyIDFilters(srcs []questions.Question, req QuizRequest) []questions.Question {
	if len(req.ExcludeSourceIDs) == 0 && len(req.OnlySourceIDs) == 0 {
		return srcs
	}
	exclude := toIDSet(req.ExcludeSourceIDs)
	only := toIDSet(req.OnlySourceIDs)
	out := make([]questions.Question, 0, len(srcs))
	for _, q := range srcs {
		if _, bad := exclude[strings.TrimSpace(q.ID)]; bad {
			continue
		}
		if len(only) > 0 {
			if _, ok := only[strings.TrimSpace(q.ID)]; !ok {
				continue
			}
		}
		out = append(out, q)
	}
	return out
}

// assignQuizIDs fills empty quiz item IDs deterministically from the source
// ID so every item is traceable even when the LLM omits the optional id.
func assignQuizIDs(qs []QuizQuestion) {
	for i := range qs {
		if strings.TrimSpace(qs[i].ID) == "" {
			qs[i].ID = "quiz-" + qs[i].SourceQuestionID
		}
	}
}

// BuildOriginalQuiz deterministically converts retrieved PYQs into quiz items:
// the original wording is the stem, options come from stored options when
// usable (else fixed placeholders), the correct answer is index 0, and the
// explanation cites the source. The output always passes ValidateAgainstSources.
// It is image-aware: when a source carries ImagesJSON with vision
// descriptions, a compact "[Figure: <name> (<figure_type>): <desc>]" hint is
// appended to the stem and the explanation notes the figure so visual
// information is not silently dropped.
func BuildOriginalQuiz(sources []questions.Question) QuizResponse {
	out := make([]QuizQuestion, 0, len(sources))
	seen := make(map[string]struct{}, len(sources))
	for _, q := range sources {
		if _, dup := seen[q.ID]; dup {
			continue // duplicate prevention in the fallback too.
		}
		seen[q.ID] = struct{}{}
		text := questionText(q)
		if text == "" {
			continue
		}
		if q.ImagesJSON != nil {
			if hint := fallbackFigureHint(*q.ImagesJSON); hint != "" {
				text = text + " " + hint
			}
		}
		opts := fallbackOptions(q)
		expl := fmt.Sprintf("Original PYQ %s from document %s (deterministic fallback; LLM unavailable).", q.ID, q.DocumentID)
		if n := questionNumber(q); n != "" {
			expl = fmt.Sprintf("Original PYQ %s (question %s) from document %s (deterministic fallback; LLM unavailable).", q.ID, n, q.DocumentID)
		}
		if q.ImagesJSON != nil {
			if fig := fallbackFigureSummary(*q.ImagesJSON); fig != "" {
				expl = expl + " " + fig
			}
		}
		out = append(out, QuizQuestion{
			ID:               "quiz-" + q.ID,
			SourceQuestionID: q.ID,
			DocumentID:       q.DocumentID,
			Question:         text,
			Options:          opts,
			CorrectAnswer:    0,
			Explanation:      expl,
		})
	}
	return QuizResponse{Questions: out}
}

func questionNumber(q questions.Question) string {
	if q.QuestionNumber == nil {
		return ""
	}
	return strings.TrimSpace(*q.QuestionNumber)
}

// fallbackFigureHint renders a compact visual cue for the deterministic
// fallback stem: "[Figure: <name> (<figure_type>): <description>]". It
// degrades gracefully: name-only when no description/type is stored, empty
// when there are no images. parseImagesJSON (prompt.go) handles both the
// legacy string-array and vision-enriched object-array ImagesJSON formats.
func fallbackFigureHint(imagesJSON string) string {
	imgs := parseImagesJSON(imagesJSON)
	if len(imgs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(imgs))
	for _, img := range imgs {
		label := img.Name
		if img.FigureType != "" && img.Description != "" {
			label = fmt.Sprintf("%s (%s): %s", img.Name, img.FigureType, img.Description)
		} else if img.FigureType != "" {
			label = fmt.Sprintf("%s (%s)", img.Name, img.FigureType)
		} else if img.Description != "" {
			label = fmt.Sprintf("%s: %s", img.Name, img.Description)
		}
		parts = append(parts, "[Figure: "+label+"]")
	}
	return strings.Join(parts, " ")
}

// fallbackFigureSummary renders a one-line visual note for the deterministic
// fallback explanation so reviewers can see which figures were preserved.
func fallbackFigureSummary(imagesJSON string) string {
	imgs := parseImagesJSON(imagesJSON)
	if len(imgs) == 0 {
		return ""
	}
	names := make([]string, 0, len(imgs))
	for _, img := range imgs {
		n := img.Name
		if img.FigureType != "" {
			n = n + " (" + img.FigureType + ")"
		}
		names = append(names, n)
	}
	return "Visual reference(s): " + strings.Join(names, ", ") + "."
}

// fallbackOptions returns exactly 4 distinct non-empty options: stored options
// when they parse to >=4 usable entries, else deterministic placeholders.
func fallbackOptions(q questions.Question) []string {
	if q.OptionsJSON != nil && strings.TrimSpace(*q.OptionsJSON) != "" {
		if opts := parseStoredOptions(*q.OptionsJSON); len(opts) == RequiredOptions {
			return opts
		}
	}
	return []string{"Option A", "Option B", "Option C", "Option D"}
}

// parseStoredOptions extracts the first 4 distinct non-empty strings from a
// JSON array of strings (or array of {text,label} objects). It returns nil
// unless exactly RequiredOptions usable options are found.
func parseStoredOptions(raw string) []string {
	var arr []string
	if err := json.Unmarshal([]byte(raw), &arr); err == nil {
		return firstDistinct(arr)
	}
	var objs []map[string]any
	if err := json.Unmarshal([]byte(raw), &objs); err == nil {
		strs := make([]string, 0, len(objs))
		for _, o := range objs {
			for _, k := range []string{"text", "label", "option"} {
				if v, ok := o[k].(string); ok && strings.TrimSpace(v) != "" {
					strs = append(strs, v)
					break
				}
			}
		}
		return firstDistinct(strs)
	}
	return nil
}

func firstDistinct(strs []string) []string {
	seen := make(map[string]struct{}, len(strs))
	out := make([]string, 0, RequiredOptions)
	for _, s := range strs {
		if strings.TrimSpace(s) == "" {
			continue
		}
		k := strings.ToLower(strings.Join(strings.Fields(s), " "))
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, strings.TrimSpace(s))
		if len(out) == RequiredOptions {
			return out
		}
	}
	return nil
}

func distinctNonEmpty(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			if _, dup := seen[t]; !dup {
				seen[t] = struct{}{}
				out = append(out, t)
			}
		}
	}
	return out
}

func distinctLower(in []string) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for _, s := range in {
		if t := strings.ToLower(strings.TrimSpace(s)); t != "" {
			out[t] = struct{}{}
		}
	}
	return out
}

func matchesAnyTopic(names []string, want map[string]struct{}) bool {
	for _, n := range names {
		if _, ok := want[strings.ToLower(strings.TrimSpace(n))]; ok {
			return true
		}
	}
	return false
}

// SortedQuestionsByID deterministically orders questions by ID. It is a helper
// for callers (and tests) that need a stable order before generating.
func SortedQuestionsByID(srcs []questions.Question) []questions.Question {
	out := append([]questions.Question{}, srcs...)
	sort.SliceStable(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out
}
