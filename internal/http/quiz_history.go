package http

import (
	"context"
	"net/http"

	"github.com/suryanshu-09/holy_grail/internal/auth"
	"github.com/suryanshu-09/holy_grail/internal/httpx"
	"github.com/suryanshu-09/holy_grail/internal/quiz"
)

// QuizHistoryLister is the history capability needed by the quiz-history
// endpoint. *quiz.EvaluationService satisfies it.
type QuizHistoryLister interface {
	ListSessions(ctx context.Context, limit, offset int) ([]quiz.QuizSession, error)
}

// handleQuizHistory handles GET /api/v1/quiz/sessions: the authenticated
// user's quiz history, newest first. Anonymous callers get an empty list
// (never another user's sessions).
func handleQuizHistory(lister QuizHistoryLister) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if lister == nil {
			httpx.Error(w, http.StatusServiceUnavailable, "quiz evaluation not configured")
			return
		}
		if _, ok := auth.UserFromContext(r.Context()); !ok {
			httpx.WriteJSON(w, http.StatusOK, []quizSessionDTO{})
			return
		}
		pg, err := httpx.ParsePagination(r)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		sessions, err := lister.ListSessions(r.Context(), pg.Limit, pg.Offset)
		if err != nil {
			httpx.LogError("quiz history failed", err)
			httpx.Error(w, http.StatusServiceUnavailable, "quiz history not available")
			return
		}
		dtos := make([]quizSessionDTO, 0, len(sessions))
		for _, s := range sessions {
			dtos = append(dtos, toQuizSessionDTO(s))
		}
		httpx.WriteJSON(w, http.StatusOK, dtos)
	})
}
