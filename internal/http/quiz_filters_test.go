package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleQuizGenerateGET_NewFilters(t *testing.T) {
	mock := &mockQuizGenerator{resp: quizOKResponse()}
	handler := handleQuizGenerate(mock)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/quiz/generate?mode=mcq&question_type=MCQ&year_min=2020&year_max=2023&only_unseen=true&exclude_source_ids=q1,q2&only_source_ids=q3",
		nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d %s", w.Code, w.Body.String())
	}
	got := mock.got[0]
	if got.QuestionType != "mcq" {
		t.Errorf("question_type = %q want mcq (normalized)", got.QuestionType)
	}
	if got.YearMin == nil || *got.YearMin != 2020 {
		t.Errorf("year_min not parsed: %+v", got.YearMin)
	}
	if got.YearMax == nil || *got.YearMax != 2023 {
		t.Errorf("year_max not parsed: %+v", got.YearMax)
	}
	if !got.OnlyUnseen {
		t.Errorf("only_unseen not parsed: %+v", got.OnlyUnseen)
	}
	if len(got.ExcludeSourceIDs) != 2 || got.ExcludeSourceIDs[0] != "q1" || got.ExcludeSourceIDs[1] != "q2" {
		t.Errorf("exclude_source_ids = %v", got.ExcludeSourceIDs)
	}
	if len(got.OnlySourceIDs) != 1 || got.OnlySourceIDs[0] != "q3" {
		t.Errorf("only_source_ids = %v", got.OnlySourceIDs)
	}
}

func TestHandleQuizGenerateGET_NewFiltersCamelCase(t *testing.T) {
	mock := &mockQuizGenerator{resp: quizOKResponse()}
	handler := handleQuizGenerate(mock)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/quiz/generate?mode=mcq&questionType=descriptive&yearMin=2021&yearMax=2022&onlyIncorrect=1&excludeSourceIds=q9&onlySourceIds=q1&onlySourceIds=q2",
		nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d %s", w.Code, w.Body.String())
	}
	got := mock.got[0]
	if got.QuestionType != "descriptive" {
		t.Errorf("questionType alias = %q", got.QuestionType)
	}
	if got.YearMin == nil || *got.YearMin != 2021 || got.YearMax == nil || *got.YearMax != 2022 {
		t.Errorf("yearMin/yearMax aliases not parsed: %+v", got)
	}
	if !got.OnlyIncorrect {
		t.Errorf("onlyIncorrect alias not parsed")
	}
	if len(got.ExcludeSourceIDs) != 1 || got.ExcludeSourceIDs[0] != "q9" {
		t.Errorf("excludeSourceIds alias = %v", got.ExcludeSourceIDs)
	}
	if len(got.OnlySourceIDs) != 2 {
		t.Errorf("repeated onlySourceIds = %v", got.OnlySourceIDs)
	}
}

func TestHandleQuizGeneratePOST_NewFilters(t *testing.T) {
	mock := &mockQuizGenerator{resp: quizOKResponse()}
	handler := handleQuizGenerate(mock)
	body, _ := json.Marshal(map[string]any{
		"mode": "mcq", "question_type": "MCQ",
		"year_min": 2020, "year_max": 2023,
		"only_unseen": true, "exclude_source_ids": []string{"q1"},
		"only_source_ids": []string{"q2"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/quiz/generate", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d %s", w.Code, w.Body.String())
	}
	got := mock.got[0]
	if got.QuestionType != "mcq" {
		t.Errorf("question_type = %q want mcq", got.QuestionType)
	}
	if got.YearMin == nil || *got.YearMin != 2020 || got.YearMax == nil || *got.YearMax != 2023 {
		t.Errorf("year range not parsed: %+v", got)
	}
	if !got.OnlyUnseen || got.OnlyIncorrect {
		t.Errorf("flags not parsed: unseen=%v incorrect=%v", got.OnlyUnseen, got.OnlyIncorrect)
	}
	if len(got.ExcludeSourceIDs) != 1 || got.ExcludeSourceIDs[0] != "q1" {
		t.Errorf("exclude_source_ids = %v", got.ExcludeSourceIDs)
	}
	if len(got.OnlySourceIDs) != 1 || got.OnlySourceIDs[0] != "q2" {
		t.Errorf("only_source_ids = %v", got.OnlySourceIDs)
	}
}

func TestHandleQuizGeneratePOST_NewFiltersCamelCase(t *testing.T) {
	mock := &mockQuizGenerator{resp: quizOKResponse()}
	handler := handleQuizGenerate(mock)
	body, _ := json.Marshal(map[string]any{
		"mode": "mcq", "questionType": "msq",
		"yearMin": 2021, "yearMax": 2022,
		"onlyIncorrect": true, "onlySourceIds": []string{"q7"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/quiz/generate", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d %s", w.Code, w.Body.String())
	}
	got := mock.got[0]
	if got.QuestionType != "msq" {
		t.Errorf("questionType alias = %q", got.QuestionType)
	}
	if got.YearMin == nil || *got.YearMin != 2021 || got.YearMax == nil || *got.YearMax != 2022 {
		t.Errorf("yearMin/yearMax aliases not parsed: %+v", got)
	}
	if !got.OnlyIncorrect {
		t.Errorf("onlyIncorrect alias not parsed")
	}
	if len(got.OnlySourceIDs) != 1 || got.OnlySourceIDs[0] != "q7" {
		t.Errorf("onlySourceIds alias = %v", got.OnlySourceIDs)
	}
}

func TestHandleQuizGenerate_NewFiltersValidation(t *testing.T) {
	cases := []struct {
		name string
		url  string
		body string
	}{
		{"inverted year range GET", "/api/v1/quiz/generate?mode=mcq&year_min=2023&year_max=2020", ""},
		{"non-int year_min GET", "/api/v1/quiz/generate?mode=mcq&year_min=many", ""},
		{"non-bool only_unseen GET", "/api/v1/quiz/generate?mode=mcq&only_unseen=maybe", ""},
		{"conflicting flags GET", "/api/v1/quiz/generate?mode=mcq&only_unseen=true&only_incorrect=true", ""},
		{"inverted year range POST", "", `{"mode":"mcq","year_min":2023,"year_max":2020}`},
		{"conflicting flags POST", "", `{"mode":"mcq","only_unseen":true,"only_incorrect":true}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockQuizGenerator{resp: quizOKResponse()}
			handler := handleQuizGenerate(mock)
			var req *http.Request
			if tc.body != "" {
				req = httptest.NewRequest(http.MethodPost, "/api/v1/quiz/generate", bytes.NewReader([]byte(tc.body)))
			} else {
				req = httptest.NewRequest(http.MethodGet, tc.url, nil)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 got %d %s", w.Code, w.Body.String())
			}
			if mock.called != 0 {
				t.Errorf("generator must not be called on validation failure")
			}
		})
	}
}
