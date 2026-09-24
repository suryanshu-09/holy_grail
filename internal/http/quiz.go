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
	return req
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
