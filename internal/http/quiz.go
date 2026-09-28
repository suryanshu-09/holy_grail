package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/suryanshu-09/holy_grail/internal/httpx"
	"github.com/suryanshu-09/holy_grail/internal/quiz"
)

// QuizGenerator is the contract for quiz generation.
// *quiz.QuizGenerator satisfies it, so it can be wired directly.
type QuizGenerator interface {
	Generate(ctx context.Context, req quiz.QuizRequest) (quiz.QuizResponse, error)
}

// quizGenerateRequest is the JSON body for POST /api/v1/quiz/generate.
// Length/limit/num_questions are aliases for the quiz length; query/q are
// aliases for the user request text; topic/topics/subject filter sources.
// Question type, year range, unseen/incorrect flags and exclude/only source
// ID lists accept snake_case and camelCase keys (snake wins when both set).
type quizGenerateRequest struct {
	Query           string   `json:"query"`
	Q               string   `json:"q"`
	Topic           string   `json:"topic"`
	Topics          []string `json:"topics"`
	Subject         string   `json:"subject"`
	Difficulty      string   `json:"difficulty"`
	Mode            string   `json:"mode"`
	Length          *int     `json:"length"`
	Limit           *int     `json:"limit"`
	NumQuestions    *int     `json:"num_questions"`
	NumQuestionsAlt *int     `json:"numQuestions"`
	QuestionType    string   `json:"question_type"`
	QuestionTypeAlt string   `json:"questionType"`
	YearMin         *int     `json:"year_min"`
	YearMinAlt      *int     `json:"yearMin"`
	YearMax         *int     `json:"year_max"`
	YearMaxAlt      *int     `json:"yearMax"`
	OnlyUnseen      *bool    `json:"only_unseen"`
	OnlyUnseenAlt   *bool    `json:"onlyUnseen"`
	OnlyIncorrect   *bool    `json:"only_incorrect"`
	OnlyIncorrectAlt *bool   `json:"onlyIncorrect"`
	ExcludeSourceIDs    []string `json:"exclude_source_ids"`
	ExcludeSourceIDsAlt []string `json:"excludeSourceIds"`
	OnlySourceIDs       []string `json:"only_source_ids"`
	OnlySourceIDsAlt    []string `json:"onlySourceIds"`
	// Phase 27 smart-quiz knobs (all optional, defaults off for backward
	// compat). `adaptive` enables personalization; `weak_topics` merges extra
	// weak-topic names into Topics; `review_due` merges due review source IDs
	// into OnlySourceIDs. Tuning fields map onto quiz.AdaptiveOptions.
	Adaptive          *bool    `json:"adaptive"`
	AdaptiveOpts      *quiz.AdaptiveOptions `json:"adaptive_options"`
	AdaptiveOptsAlt   *quiz.AdaptiveOptions `json:"adaptiveOptions"`
	WeakTopics        []string `json:"weak_topics"`
	WeakTopicsAlt     []string `json:"weakTopics"`
	ReviewDue         []string `json:"review_due"`
	ReviewDueAlt      []string `json:"reviewDue"`
	WeakThreshold     *float64 `json:"weak_threshold"`
	WeakThresholdAlt  *float64 `json:"weakThreshold"`
	WeakBoost         *float64 `json:"weak_topic_boost"`
	WeakBoostAlt      *float64 `json:"weakTopicBoost"`
	PreferUnseen      *bool    `json:"prefer_unseen"`
	PreferUnseenAlt   *bool    `json:"preferUnseen"`
	PreferIncorrect   *bool    `json:"prefer_incorrect"`
	PreferIncorrectAlt *bool   `json:"preferIncorrect"`
	SkipDueReview     *bool    `json:"skip_due_review"`
	SkipDueReviewAlt  *bool    `json:"skipDueReview"`
}

func (r *quizGenerateRequest) effectiveQuery() string {
	if strings.TrimSpace(r.Query) != "" {
		return strings.TrimSpace(r.Query)
	}
	return strings.TrimSpace(r.Q)
}

func (r *quizGenerateRequest) effectiveTopics() []string {
	out := make([]string, 0, len(r.Topics)+1)
	if strings.TrimSpace(r.Topic) != "" {
		out = append(out, strings.TrimSpace(r.Topic))
	}
	for _, t := range r.Topics {
		if s := strings.TrimSpace(t); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (r *quizGenerateRequest) effectiveLength() *int {
	if r.Length != nil {
		return r.Length
	}
	if r.Limit != nil {
		return r.Limit
	}
	if r.NumQuestions != nil {
		return r.NumQuestions
	}
	return r.NumQuestionsAlt
}

func (r *quizGenerateRequest) toQuizRequest() quiz.QuizRequest {
	req := quiz.QuizRequest{
		Mode:         quiz.QuizMode(strings.TrimSpace(r.Mode)),
		Difficulty:   strings.TrimSpace(r.Difficulty),
		Topics:       r.effectiveTopics(),
		Subject:      strings.TrimSpace(r.Subject),
		Query:        r.effectiveQuery(),
		QuestionType: strings.TrimSpace(r.QuestionType),
		YearMin:      r.YearMin,
		YearMax:      r.YearMax,
	}
	if req.QuestionType == "" {
		req.QuestionType = strings.TrimSpace(r.QuestionTypeAlt)
	}
	if req.YearMin == nil {
		req.YearMin = r.YearMinAlt
	}
	if req.YearMax == nil {
		req.YearMax = r.YearMaxAlt
	}
	if r.OnlyUnseen != nil {
		req.OnlyUnseen = *r.OnlyUnseen
	} else if r.OnlyUnseenAlt != nil {
		req.OnlyUnseen = *r.OnlyUnseenAlt
	}
	if r.OnlyIncorrect != nil {
		req.OnlyIncorrect = *r.OnlyIncorrect
	} else if r.OnlyIncorrectAlt != nil {
		req.OnlyIncorrect = *r.OnlyIncorrectAlt
	}
	req.ExcludeSourceIDs = append(append([]string{}, r.ExcludeSourceIDs...), r.ExcludeSourceIDsAlt...)
	req.OnlySourceIDs = append(append([]string{}, r.OnlySourceIDs...), r.OnlySourceIDsAlt...)
	if n := r.effectiveLength(); n != nil {
		req.NumQuestions = *n
	}
	req.Adaptive, req.Topics, req.OnlySourceIDs = r.effectiveAdaptive(req.Topics, req.OnlySourceIDs)
	return req
}

// effectiveAdaptive builds the optional *quiz.AdaptiveOptions from the
// Phase 27 body knobs. Nil (default) preserves legacy behavior: adaptive is
// only enabled when `adaptive: true`, a nested adaptive_options object, or
// any tuning/merge field is present. weak_topics merge into topics and
// review_due merge into only-source-IDs (deduped, order-preserving).
func (r *quizGenerateRequest) effectiveAdaptive(topics, onlyIDs []string) (*quiz.AdaptiveOptions, []string, []string) {
	adaptiveOn := r.Adaptive != nil && *r.Adaptive
	opts := r.AdaptiveOpts
	if opts == nil {
		opts = r.AdaptiveOptsAlt
	}
	weakTopics := append(append([]string{}, r.WeakTopics...), r.WeakTopicsAlt...)
	reviewDue := append(append([]string{}, r.ReviewDue...), r.ReviewDueAlt...)
	hasTuning := r.WeakThreshold != nil || r.WeakThresholdAlt != nil ||
		r.WeakBoost != nil || r.WeakBoostAlt != nil ||
		r.PreferUnseen != nil || r.PreferUnseenAlt != nil ||
		r.PreferIncorrect != nil || r.PreferIncorrectAlt != nil ||
		r.SkipDueReview != nil || r.SkipDueReviewAlt != nil ||
		len(weakTopics) > 0 || len(reviewDue) > 0
	if !adaptiveOn && opts == nil && !hasTuning {
		return nil, topics, onlyIDs
	}
	out := &quiz.AdaptiveOptions{Enabled: true}
	if opts != nil {
		cp := *opts
		out = &cp
		out.Enabled = out.Enabled || adaptiveOn || hasTuning
		if !adaptiveOn && !hasTuning && opts.Enabled {
			out.Enabled = true
		}
	}
	if v := firstFloat(r.WeakThreshold, r.WeakThresholdAlt); v != nil {
		out.WeakThreshold = *v
	}
	if v := firstFloat(r.WeakBoost, r.WeakBoostAlt); v != nil {
		out.WeakBoost = *v
	}
	if v := firstBool(r.PreferUnseen, r.PreferUnseenAlt); v != nil {
		out.PreferUnseen = *v
	}
	if v := firstBool(r.PreferIncorrect, r.PreferIncorrectAlt); v != nil {
		out.PreferIncorrect = *v
	}
	if v := firstBool(r.SkipDueReview, r.SkipDueReviewAlt); v != nil {
		out.SkipDueReview = *v
	}
	// Merge weak topics + due review IDs (dedupe preserving order).
	if len(weakTopics) > 0 {
		seen := make(map[string]struct{}, len(topics)+len(weakTopics))
		for _, t := range topics {
			seen[strings.ToLower(strings.TrimSpace(t))] = struct{}{}
		}
		for _, t := range weakTopics {
			if s := strings.TrimSpace(t); s != "" {
				if _, dup := seen[strings.ToLower(s)]; !dup {
					seen[strings.ToLower(s)] = struct{}{}
					topics = append(topics, s)
				}
			}
		}
	}
	if len(reviewDue) > 0 && !out.SkipDueReview {
		seen := make(map[string]struct{}, len(onlyIDs)+len(reviewDue))
		for _, id := range onlyIDs {
			seen[id] = struct{}{}
		}
		for _, id := range reviewDue {
			if s := strings.TrimSpace(id); s != "" {
				if _, dup := seen[s]; !dup {
					seen[s] = struct{}{}
					onlyIDs = append(onlyIDs, s)
				}
			}
		}
	}
	return out, topics, onlyIDs
}

func firstFloat(vals ...*float64) *float64 {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

func firstBool(vals ...*bool) *bool {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

// parseQuizLengthParam parses length/limit/num_questions query params.
// The first present param wins (length > limit > num_questions > numQuestions).
func parseQuizLengthParam(q map[string][]string) (*int, error) {
	for _, key := range []string{"length", "limit", "num_questions", "numQuestions"} {
		vals, ok := q[key]
		if !ok || len(vals) == 0 || strings.TrimSpace(vals[0]) == "" {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(vals[0]))
		if err != nil {
			return nil, err
		}
		return &n, nil
	}
	return nil, nil
}

// parseQuizIntAlias parses an optional integer query param with a camelCase
// alias (first present key wins). Missing keys yield (nil, nil).
func parseQuizIntAlias(q map[string][]string, snake, camel string) (*int, error) {
	for _, key := range []string{snake, camel} {
		vals, ok := q[key]
		if !ok || len(vals) == 0 || strings.TrimSpace(vals[0]) == "" {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(vals[0]))
		if err != nil {
			return nil, err
		}
		return &n, nil
	}
	return nil, nil
}

// parseQuizBoolAlias parses an optional boolean query param with a camelCase
// alias (first present key wins). Accepts strconv.ParseBool values
// (true/false/1/0/...). Missing keys yield false with no error.
func parseQuizBoolAlias(q map[string][]string, snake, camel string) (bool, error) {
	for _, key := range []string{snake, camel} {
		vals, ok := q[key]
		if !ok || len(vals) == 0 || strings.TrimSpace(vals[0]) == "" {
			continue
		}
		b, err := strconv.ParseBool(strings.TrimSpace(vals[0]))
		if err != nil {
			return false, err
		}
		return b, nil
	}
	return false, nil
}

// parseQuizFloatAlias parses an optional float query param with a camelCase
// alias (first present key wins). Missing keys yield (nil, nil).
func parseQuizFloatAlias(q map[string][]string, snake, camel string) (*float64, error) {
	for _, key := range []string{snake, camel} {
		vals, ok := q[key]
		if !ok || len(vals) == 0 || strings.TrimSpace(vals[0]) == "" {
			continue
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(vals[0]), 64)
		if err != nil {
			return nil, err
		}
		return &f, nil
	}
	return nil, nil
}

// parseQuizIDsAlias collects repeated and comma-separated ID list params
// under snake_case and camelCase keys.
func parseQuizIDsAlias(q map[string][]string, snake, camel string) []string {
	var out []string
	for _, key := range []string{snake, camel} {
		for _, v := range q[key] {
			for _, part := range strings.Split(v, ",") {
				if s := strings.TrimSpace(part); s != "" {
					out = append(out, s)
				}
			}
		}
	}
	return out
}

// firstNonEmpty returns the first non-blank value for keys in order.
func firstNonEmpty(q map[string][]string, keys ...string) string {
	for _, key := range keys {
		if vals, ok := q[key]; ok {
			for _, v := range vals {
				if s := strings.TrimSpace(v); s != "" {
					return s
				}
			}
		}
	}
	return ""
}
// handleQuizGenerate handles GET and POST for quiz generation.
// GET  /api/v1/quiz/generate?query=...&topic=...&difficulty=...&mode=mcq&length=5
//      plus question_type/year_min/year_max/only_unseen/only_incorrect/
//      exclude_source_ids/only_source_ids (camelCase aliases accepted)
// POST /api/v1/quiz/generate  { "query": "...", "topic": "...", ... }
// Invalid mode/length/difficulty/filters map to 400; nil generator maps to 503.
func handleQuizGenerate(gen QuizGenerator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
			return
		}
		if gen == nil {
			httpx.Error(w, http.StatusServiceUnavailable, "quiz generator not configured")
			return
		}

		var req quiz.QuizRequest

		if r.Method == http.MethodGet {
			q := r.URL.Query()
			query := strings.TrimSpace(q.Get("query"))
			if query == "" {
				query = strings.TrimSpace(q.Get("q"))
			}
			var topics []string
			if t := strings.TrimSpace(q.Get("topic")); t != "" {
				topics = append(topics, t)
			}
			// Support repeated ?topic= and comma-separated ?topics= lists.
			for _, t := range q["topics"] {
				for _, part := range strings.Split(t, ",") {
					if s := strings.TrimSpace(part); s != "" {
						topics = append(topics, s)
					}
				}
			}
			for _, extra := range q["topic"] {
				// q["topic"] already includes the value read via Get; dedupe below.
				_ = extra
			}
		req = quiz.QuizRequest{
			Mode:             quiz.QuizMode(strings.TrimSpace(q.Get("mode"))),
			Difficulty:       strings.TrimSpace(q.Get("difficulty")),
			Topics:           topics,
			Subject:          strings.TrimSpace(q.Get("subject")),
			Query:            query,
			QuestionType:     firstNonEmpty(map[string][]string(q), "question_type", "questionType"),
			ExcludeSourceIDs: parseQuizIDsAlias(map[string][]string(q), "exclude_source_ids", "excludeSourceIds"),
			OnlySourceIDs:    parseQuizIDsAlias(map[string][]string(q), "only_source_ids", "onlySourceIds"),
		}
			// Dedupe topics preserving order (Get + map iteration may repeat).
			seen := make(map[string]struct{}, len(req.Topics))
			deduped := make([]string, 0, len(req.Topics))
			for _, t := range req.Topics {
				if _, ok := seen[t]; !ok {
					seen[t] = struct{}{}
					deduped = append(deduped, t)
				}
			}
			req.Topics = deduped
		n, err := parseQuizLengthParam(map[string][]string(q))
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "length must be an integer")
			return
		}
		if n != nil {
			req.NumQuestions = *n
		}
		qq := map[string][]string(q)
		yearMin, err := parseQuizIntAlias(qq, "year_min", "yearMin")
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "year_min must be an integer")
			return
		}
		yearMax, err := parseQuizIntAlias(qq, "year_max", "yearMax")
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "year_max must be an integer")
			return
		}
		req.YearMin = yearMin
		req.YearMax = yearMax
		unseen, err := parseQuizBoolAlias(qq, "only_unseen", "onlyUnseen")
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "only_unseen must be a boolean")
			return
		}
		incorrect, err := parseQuizBoolAlias(qq, "only_incorrect", "onlyIncorrect")
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "only_incorrect must be a boolean")
			return
		}
		req.OnlyUnseen = unseen
		req.OnlyIncorrect = incorrect
		// Phase 27 smart-quiz params (all optional, defaults off). `adaptive`
		// enables quiz.AdaptiveOptions; weak_topics/review_due merge lists.
		qq2 := map[string][]string(q)
		adaptiveOn, err := parseQuizBoolAlias(qq2, "adaptive", "adaptive")
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "adaptive must be a boolean")
			return
		}
		weakTopics := parseQuizIDsAlias(qq2, "weak_topics", "weakTopics")
		reviewDue := parseQuizIDsAlias(qq2, "review_due", "reviewDue")
		weakThreshold, err := parseQuizFloatAlias(qq2, "weak_threshold", "weakThreshold")
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "weak_threshold must be a number")
			return
		}
		weakBoost, err := parseQuizFloatAlias(qq2, "weak_topic_boost", "weakTopicBoost")
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "weak_topic_boost must be a number")
			return
		}
		if v, err := parseQuizFloatAlias(qq2, "weak_boost", "weakBoost"); err != nil {
			httpx.Error(w, http.StatusBadRequest, "weak_boost must be a number")
			return
		} else if v != nil && weakBoost == nil {
			weakBoost = v
		}
		preferUnseen, err := parseQuizBoolAlias(qq2, "prefer_unseen", "preferUnseen")
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "prefer_unseen must be a boolean")
			return
		}
		preferIncorrect, err := parseQuizBoolAlias(qq2, "prefer_incorrect", "preferIncorrect")
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "prefer_incorrect must be a boolean")
			return
		}
		skipDue, err := parseQuizBoolAlias(qq2, "skip_due_review", "skipDueReview")
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "skip_due_review must be a boolean")
			return
		}
		if adaptiveOn || len(weakTopics) > 0 || len(reviewDue) > 0 ||
			weakThreshold != nil || weakBoost != nil || preferUnseen || preferIncorrect || skipDue {
			opts := &quiz.AdaptiveOptions{Enabled: true}
			if weakThreshold != nil {
				opts.WeakThreshold = *weakThreshold
			}
			if weakBoost != nil {
				opts.WeakBoost = *weakBoost
			}
			opts.PreferUnseen = preferUnseen
			opts.PreferIncorrect = preferIncorrect
			opts.SkipDueReview = skipDue
			req.Adaptive = opts
			if len(weakTopics) > 0 {
				seen := make(map[string]struct{}, len(req.Topics)+len(weakTopics))
				for _, t := range req.Topics {
					seen[strings.ToLower(strings.TrimSpace(t))] = struct{}{}
				}
				for _, t := range weakTopics {
					if s := strings.TrimSpace(t); s != "" {
						if _, dup := seen[strings.ToLower(s)]; !dup {
							seen[strings.ToLower(s)] = struct{}{}
							req.Topics = append(req.Topics, s)
						}
					}
				}
			}
			if len(reviewDue) > 0 && !opts.SkipDueReview {
				seen := make(map[string]struct{}, len(req.OnlySourceIDs)+len(reviewDue))
				for _, id := range req.OnlySourceIDs {
					seen[id] = struct{}{}
				}
				for _, id := range reviewDue {
					if s := strings.TrimSpace(id); s != "" {
						if _, dup := seen[s]; !dup {
							seen[s] = struct{}{}
							req.OnlySourceIDs = append(req.OnlySourceIDs, s)
						}
					}
				}
			}
		}
		} else {
			var body quizGenerateRequest
			dec := json.NewDecoder(r.Body)
			dec.DisallowUnknownFields()
			if err := dec.Decode(&body); err != nil {
				httpx.Error(w, http.StatusBadRequest, "invalid request body")
				return
			}
			req = body.toQuizRequest()
		}

		// Pre-validate so mode/length/difficulty errors are 400 with a clear
		// message before touching the generator.
		if err := req.Validate(); err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}

		resp, err := gen.Generate(r.Context(), req)
		if err != nil {
			msg := err.Error()
			lower := strings.ToLower(msg)
			if strings.Contains(lower, "invalid") ||
				strings.Contains(lower, "out of range") ||
				strings.Contains(lower, "must be") ||
				strings.Contains(lower, "is required") ||
				strings.Contains(lower, "no source questions match") {
				httpx.Error(w, http.StatusBadRequest, msg)
				return
			}
			httpx.LogError("quiz generation failed", err)
			httpx.Error(w, http.StatusInternalServerError, "quiz generation failed")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, resp)
	})
}
