package http

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/suryanshu-09/holy_grail/internal/analytics"
	"github.com/suryanshu-09/holy_grail/internal/httpx"
	"github.com/suryanshu-09/holy_grail/internal/questions"
	"github.com/suryanshu-09/holy_grail/internal/quiz"
)

// Analytics endpoints (Phase 27: Optional Advanced Features) summarize quiz
// performance without any schema change: attempts are loaded through the
// existing QuizEvaluator and folded by the pure internal/analytics package.
//
// Scope selection per endpoint (GET):
//   - ?session_id=<id>: single-session scope via QuizEvaluator.GetResult.
//   - otherwise: user scope — the authenticated user's recent sessions via
//     QuizHistoryLister (each resolved with GetResult and merged, most
//     recent first, bounded by ?limit=, default 20, max 100).
//
// A nil evaluator maps to 503; a nil history lister with no session_id maps
// to 503; unknown sessions map to 404; invalid query params map to 400.

// QuestionDifficultyGetter resolves a question record for difficulty
// enrichment. *questions.Service satisfies it, so it can be wired directly.
// A nil getter degrades difficulty labels to "unknown".
type QuestionDifficultyGetter interface {
	Get(ctx context.Context, id string) (questions.Question, error)
}

// AnalyticsDeps wires the analytics handlers. All fields are optional; nil
// fields degrade gracefully (503 for missing evaluator/history, "unknown"
// difficulty labels for a missing Questions getter).
type AnalyticsDeps struct {
	Eval      QuizEvaluator
	History   QuizHistoryLister
	Questions QuestionDifficultyGetter
}

// analyticsScope describes which attempts back a response.
type analyticsScope struct {
	SessionID string `json:"session_id,omitempty"`
	Sessions  int    `json:"sessions"`
	Attempts  int    `json:"attempts"`
}

// resolveAnalyticsAttempts loads the attempts for a request: single-session
// scope when ?session_id= is set, else the user's recent-session scope.
// It returns the merged attempts, the scope descriptor, or an error already
// mapped to an HTTP status by the caller via writeEvaluationError for
// session errors.
func resolveAnalyticsAttempts(r *http.Request, deps AnalyticsDeps) ([]quiz.QuizAttempt, analyticsScope, error) {
	if deps.Eval == nil {
		return nil, analyticsScope{}, errAnalyticsUnavailable
	}
	if id := strings.TrimSpace(r.URL.Query().Get("session_id")); id != "" {
		res, err := deps.Eval.GetResult(r.Context(), id)
		if err != nil {
			return nil, analyticsScope{}, err
		}
		return res.Attempts, analyticsScope{SessionID: id, Sessions: 1, Attempts: len(res.Attempts)}, nil
	}
	if deps.History == nil {
		return nil, analyticsScope{}, errAnalyticsUnavailable
	}
	pg, err := httpx.ParsePagination(r)
	if err != nil {
		return nil, analyticsScope{}, err
	}
	sessions, err := deps.History.ListSessions(r.Context(), pg.Limit, pg.Offset)
	if err != nil {
		return nil, analyticsScope{}, err
	}
	merged := make([]quiz.QuizAttempt, 0)
	for _, s := range sessions {
		res, err := deps.Eval.GetResult(r.Context(), s.ID)
		if err != nil {
			// Skip sessions that vanished or became inaccessible; the
			// summary covers what is still readable.
			continue
		}
		merged = append(merged, res.Attempts...)
	}
	return merged, analyticsScope{Sessions: len(sessions), Attempts: len(merged)}, nil
}

// errAnalyticsUnavailable marks a missing evaluator/history dependency.
var errAnalyticsUnavailable = errUnavailable("analytics not configured")

// errUnavailable is a sentinel error type for unconfigured dependencies.
type errUnavailable string

// Error implements error.
func (e errUnavailable) Error() string { return string(e) }

// writeAnalyticsError maps resolution errors to status codes.
func writeAnalyticsError(w http.ResponseWriter, err error) {
	if err == errAnalyticsUnavailable {
		httpx.Error(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeEvaluationError(w, err)
}

// analyticsDifficultyOf builds the difficulty resolver for attempts: source
// question lookup (falling back to the quiz question ID) via the Questions
// getter with per-ID caching. A nil getter yields nil, grouping everything
// under "unknown".
func analyticsDifficultyOf(ctx context.Context, deps AnalyticsDeps, attempts []quiz.QuizAttempt) func(quiz.QuizAttempt) string {
	if deps.Questions == nil {
		return nil
	}
	cache := make(map[string]string, len(attempts))
	return func(a quiz.QuizAttempt) string {
		for _, id := range []string{
			strings.TrimSpace(a.SourceQuestionID),
			strings.TrimSpace(a.QuestionID),
		} {
			if id == "" {
				continue
			}
			if d, ok := cache[id]; ok {
				return d
			}
			d := "unknown"
			if q, err := deps.Questions.Get(ctx, id); err == nil && q.Difficulty != nil {
				if s := strings.ToLower(strings.TrimSpace(*q.Difficulty)); s != "" {
					d = s
				}
			}
			cache[id] = d
			return d
		}
		return "unknown"
	}
}

// handleAnalyticsMastery handles GET /api/v1/analytics/mastery.
func handleAnalyticsMastery(deps AnalyticsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		attempts, scope, err := resolveAnalyticsAttempts(r, deps)
		if err != nil {
			writeAnalyticsError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"scope":  scope,
			"topics": analytics.ComputeTopicMastery(quiz.ComputeTopicBreakdown(attempts)),
		})
	})
}

// handleAnalyticsHistory handles GET /api/v1/analytics/history.
func handleAnalyticsHistory(deps AnalyticsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		attempts, scope, err := resolveAnalyticsAttempts(r, deps)
		if err != nil {
			writeAnalyticsError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"scope": scope,
			"days":  analytics.HistoricalAccuracy(attempts),
		})
	})
}

// handleAnalyticsDifficulty handles GET /api/v1/analytics/difficulty.
func handleAnalyticsDifficulty(deps AnalyticsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		attempts, scope, err := resolveAnalyticsAttempts(r, deps)
		if err != nil {
			writeAnalyticsError(w, err)
			return
		}
		rows := analytics.QuestionDifficultyStats(attempts, analyticsDifficultyOf(r.Context(), deps, attempts))
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"scope":        scope,
			"difficulties": rows,
		})
	})
}

// handleAnalyticsTiming handles GET /api/v1/analytics/timing.
func handleAnalyticsTiming(deps AnalyticsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		attempts, scope, err := resolveAnalyticsAttempts(r, deps)
		if err != nil {
			writeAnalyticsError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"scope":  scope,
			"timing": analytics.TimePerQuestion(attempts),
		})
	})
}

// handleAnalyticsReadiness handles GET /api/v1/analytics/readiness.
// ?total_topics=<n> sizes coverage against the known curriculum (default:
// coverage is 1 when anything was attempted, else 0).
func handleAnalyticsReadiness(deps AnalyticsDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		attempts, scope, err := resolveAnalyticsAttempts(r, deps)
		if err != nil {
			writeAnalyticsError(w, err)
			return
		}
		total := 0
		if raw := strings.TrimSpace(r.URL.Query().Get("total_topics")); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 0 {
				httpx.Error(w, http.StatusBadRequest, "total_topics must be a non-negative integer")
				return
			}
			total = n
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"scope": scope,
			"readiness": analytics.ExamReadinessScore(analytics.ReadinessInput{
				Attempts:    attempts,
				TotalTopics: total,
			}),
		})
	})
}
