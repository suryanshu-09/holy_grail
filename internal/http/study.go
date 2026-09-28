package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/suryanshu-09/holy_grail/internal/httpx"
	"github.com/suryanshu-09/holy_grail/internal/questions"
	"github.com/suryanshu-09/holy_grail/internal/quiz"
	"github.com/suryanshu-09/holy_grail/internal/search"
	"github.com/suryanshu-09/holy_grail/internal/study"
	"github.com/suryanshu-09/holy_grail/internal/topics"
)

// Study Mode endpoints (Phase 27: Optional Advanced Features) serve the
// Topic -> Explanation -> Example -> PYQs -> Quiz flow:
//
//	GET  /api/v1/study/guide?topic=<name>&subject=<s>&limit=<n>&weak=<bool>
//	POST /api/v1/study/guide  {"topic": "...", "subject": "...", "limit": n, "weak": bool}
//
// The guide itself is built deterministically by study.BuildStudyGuide over
// PYQs retrieved with the Searcher; Topics fills in the subject when the
// caller omits it; Quiz optionally upgrades the suggested practice quiz and
// otherwise falls back to the deterministic quiz.BuildOriginalQuiz inside
// the guide. All deps are optional (nil-safe): a missing Searcher yields a
// PYQ-less guide, a missing Quiz keeps the deterministic NextQuiz.

// StudyTopicResolver resolves a topic by name for study guides.
// *topics.Service satisfies it, so it can be wired directly.
type StudyTopicResolver interface {
	GetByName(ctx context.Context, name string, subject *string) (topics.Topic, error)
}

// StudyDeps wires the study guide handlers. All fields are optional; nil
// fields degrade gracefully (see above).
type StudyDeps struct {
	Topics   StudyTopicResolver
	Searcher Searcher
	Quiz     QuizGenerator
}

// studyGuideParams is the shared GET/POST input for a study guide.
type studyGuideParams struct {
	Topic   string
	Subject string
	Limit   int
	Weak    bool
}

// parseStudyGuideParams validates query params for GET /api/v1/study/guide.
func parseStudyGuideParams(r *http.Request) (studyGuideParams, error) {
	p := studyGuideParams{Limit: 10}
	q := r.URL.Query()
	p.Topic = strings.TrimSpace(q.Get("topic"))
	p.Subject = strings.TrimSpace(q.Get("subject"))
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 50 {
			return p, errStudyLimit
		}
		p.Limit = n
	}
	if raw := strings.TrimSpace(q.Get("weak")); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return p, errStudyWeak
		}
		p.Weak = b
	}
	if p.Topic == "" {
		return p, errStudyTopic
	}
	return p, nil
}

var (
	errStudyTopic = studyParamError("topic is required")
	errStudyLimit = studyParamError("limit must be an integer in [1,50]")
	errStudyWeak  = studyParamError("weak must be a boolean")
)

// studyParamError is a 400-class validation error for study params.
type studyParamError string

// Error implements error.
func (e studyParamError) Error() string { return string(e) }

// studyGuideRequest is the JSON body for POST /api/v1/study/guide.
type studyGuideRequest struct {
	Topic   string `json:"topic"`
	Subject string `json:"subject"`
	Limit   *int   `json:"limit"`
	Weak    *bool  `json:"weak"`
}

func parseStudyGuideBody(r *http.Request) (studyGuideParams, error) {
	p := studyGuideParams{Limit: 10}
	var body studyGuideRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		return p, studyParamError("invalid request body")
	}
	p.Topic = strings.TrimSpace(body.Topic)
	p.Subject = strings.TrimSpace(body.Subject)
	if body.Limit != nil {
		if *body.Limit < 1 || *body.Limit > 50 {
			return p, errStudyLimit
		}
		p.Limit = *body.Limit
	}
	if body.Weak != nil {
		p.Weak = *body.Weak
	}
	if p.Topic == "" {
		return p, errStudyTopic
	}
	return p, nil
}

// buildStudyGuide resolves the subject, retrieves PYQs, and builds the
// guide, upgrading NextQuiz via the Quiz generator when available.
func buildStudyGuide(ctx context.Context, deps StudyDeps, p studyGuideParams) (study.StudyGuide, error) {
	subject := p.Subject
	if subject == "" && deps.Topics != nil {
		if t, err := deps.Topics.GetByName(ctx, p.Topic, nil); err == nil && t.Subject != nil {
			subject = strings.TrimSpace(*t.Subject)
		}
	}
	var pyqs []questions.Question
	if deps.Searcher != nil {
		resp, err := deps.Searcher.Search(ctx, p.Topic, search.Filter{
			Subject: subject,
			Topic:   p.Topic,
			Limit:   p.Limit,
		})
		if err != nil {
			return study.StudyGuide{}, err
		}
		for _, hit := range resp.Results {
			pyqs = append(pyqs, hit.Question)
		}
	}
	guide := study.BuildStudyGuide(p.Topic, subject, pyqs, p.Weak)
	if deps.Quiz != nil {
		n := len(pyqs)
		if n > study.MaxNextQuiz {
			n = study.MaxNextQuiz
		}
		if n <= 0 {
			n = study.MaxNextQuiz
		}
		if gen, err := deps.Quiz.Generate(ctx, quiz.QuizRequest{
			Mode:         quiz.ModeOriginal,
			NumQuestions: n,
			Topics:       []string{p.Topic},
			Subject:      subject,
		}); err == nil && len(gen.Questions) > 0 {
			guide.NextQuiz = gen
		}
	}
	return guide, nil
}

// handleStudyGuide handles GET and POST /api/v1/study/guide.
func handleStudyGuide(deps StudyDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
			return
		}
		var (
			p   studyGuideParams
			err error
		)
		if r.Method == http.MethodGet {
			p, err = parseStudyGuideParams(r)
		} else {
			p, err = parseStudyGuideBody(r)
		}
		if err != nil {
			if _, ok := err.(studyParamError); ok {
				httpx.Error(w, http.StatusBadRequest, err.Error())
				return
			}
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		guide, err := buildStudyGuide(r.Context(), deps, p)
		if err != nil {
			httpx.LogError("study guide failed", err)
			httpx.Error(w, http.StatusInternalServerError, "study guide failed")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, guide)
	})
}
