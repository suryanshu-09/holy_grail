package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIClientExtractQuestionsFromText(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// return a minimal choices payload
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"[ { \"number\": \"1\", \"text\": \"Q1?\", \"start_page\": 1, \"end_page\": 1 } ]" }}]}`))
	}))
	defer ts.Close()

	c, err := NewOpenAIClient("fake-key", "gpt-test", ts.URL)
	if err != nil {
		t.Fatalf("NewOpenAIClient: %v", err)
	}
	c.Client = ts.Client()
	res, err := c.ExtractQuestionsFromText(context.Background(), "please extract questions")
	if err != nil {
		t.Fatalf("ExtractQuestionsFromText: %v", err)
	}
	if res == "" {
		t.Fatalf("empty response")
	}
}
