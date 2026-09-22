package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestVisionModelFromEnvDefault(t *testing.T) {
	os.Unsetenv("OPENAI_VISION_MODEL")
	if got := VisionModelFromEnv(); got != DefaultVisionModel {
		t.Fatalf("default = %q, want %q", got, DefaultVisionModel)
	}
}

func TestVisionModelFromEnvOverride(t *testing.T) {
	t.Setenv("OPENAI_VISION_MODEL", "gpt-4o")
	if got := VisionModelFromEnv(); got != "gpt-4o" {
		t.Fatalf("override = %q, want gpt-4o", got)
	}
}

func TestNewOpenAIVisionDescriberDefaults(t *testing.T) {
	t.Setenv("OPENAI_VISION_MODEL", "gpt-4o-mini-test")
	d, err := NewOpenAIVisionDescriber("key", "", "")
	if err != nil {
		t.Fatalf("NewOpenAIVisionDescriber: %v", err)
	}
	if d.Model != "gpt-4o-mini-test" {
		t.Fatalf("model = %q, want env default", d.Model)
	}
	if d.BaseURL != "https://api.openai.com" {
		t.Fatalf("baseURL = %q", d.BaseURL)
	}
}

func TestNewOpenAIVisionDescriberNoKey(t *testing.T) {
	if _, err := NewOpenAIVisionDescriber("", "gpt-4o-mini", ""); err == nil {
		t.Fatalf("expected error for empty key")
	}
}

func TestNewVisionDescriberNoKeyFallback(t *testing.T) {
	d := NewVisionDescriber("", "", "")
	if d == nil {
		t.Fatalf("expected non-nil fallback")
	}
	out, err := d.Describe(context.Background(), VisionDescribeInput{Name: "x.png"})
	if err != nil {
		t.Fatalf("noop Describe: %v", err)
	}
	if out.FigureType != VisionFigureUnknown || out.DescribedBy != "noop" {
		t.Fatalf("noop output = %+v", out)
	}
}

func TestNormalizeVisionFigureType(t *testing.T) {
	cases := map[string]string{
		"diagram": "diagram", "DIAGRAMS": "diagram",
		"graph": "graph", "plots": "graph", "Plot": "graph",
		"math_figure": "math_figure", "mathematical figure": "math_figure", "equation": "math_figure",
		"table": "table", "tabular": "table",
		"chart": "chart", "bar chart": "chart", "piechart": "chart", "histogram": "chart",
		"photo": "photo", "photograph": "photo",
		"unknown": "unknown", "": "unknown", "nonsense": "unknown",
	}
	for in, want := range cases {
		if got := NormalizeVisionFigureType(in); got != want {
			t.Errorf("NormalizeVisionFigureType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildVisionPrompt(t *testing.T) {
	p := BuildVisionPrompt(VisionDescribeInput{Name: "a.png", Context: "cell division"})
	for _, want := range []string{"diagram, graph, math_figure, table, chart, photo, unknown", "strict JSON", "cell division"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q: %s", want, p)
		}
	}
	// Long context truncated to 500 chars.
	long := strings.Repeat("x", 1000)
	p2 := BuildVisionPrompt(VisionDescribeInput{Context: long})
	if strings.Contains(p2, long) {
		t.Errorf("context not truncated")
	}
}

func TestParseVisionJSON(t *testing.T) {
	out, err := parseVisionJSON(`{"description": "A bar chart of sales.", "figure_type": "bar chart"}`, "gpt-4o-mini")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if out.Description != "A bar chart of sales." {
		t.Errorf("description = %q", out.Description)
	}
	if out.FigureType != VisionFigureChart {
		t.Errorf("figure_type = %q, want chart", out.FigureType)
	}
	if out.DescribedBy != "gpt-4o-mini" {
		t.Errorf("described_by = %q", out.DescribedBy)
	}
}

func TestParseVisionJSONFences(t *testing.T) {
	out, err := parseVisionJSON("```json\n{\"description\": \"Directed graph with A B C.\", \"figure_type\": \"graph\"}\n```", "m")
	if err != nil {
		t.Fatalf("parse fences: %v", err)
	}
	if out.FigureType != VisionFigureGraph {
		t.Errorf("figure_type = %q", out.FigureType)
	}
}

func TestParseVisionJSONErrors(t *testing.T) {
	for _, s := range []string{"", `{"figure_type":"graph"}`, "not json", `{"description":"x","figure_type":"graph","extra":1}`} {
		if _, err := parseVisionJSON(s, "m"); err == nil {
			t.Errorf("expected error for %q", s)
		}
	}
}

func TestVisionDataURL(t *testing.T) {
	u := visionDataURL([]byte{1, 2, 3}, "png")
	if !strings.HasPrefix(u, "data:image/png;base64,") {
		t.Fatalf("url = %q", u)
	}
	if !strings.HasSuffix(u, base64.StdEncoding.EncodeToString([]byte{1, 2, 3})) {
		t.Fatalf("payload not base64: %q", u)
	}
	if m := visionMIME("jpg"); m != "image/jpeg" {
		t.Fatalf("mime jpg = %q", m)
	}
}

func TestOpenAIVisionDescribeRoundTrip(t *testing.T) {
	var gotModel string
	var gotImageURL string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content []struct {
					Type     string `json:"type"`
					Text     string `json:"text"`
					ImageURL *struct {
						URL string `json:"url"`
					} `json:"image_url"`
				} `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		gotModel = req.Model
		for _, m := range req.Messages {
			for _, c := range m.Content {
				if c.Type == "image_url" && c.ImageURL != nil {
					gotImageURL = c.ImageURL.URL
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"description\": \"Directed graph with nodes A, B, C.\", \"figure_type\": \"diagram\"}"}}]}`))
	}))
	defer ts.Close()

	d, err := NewOpenAIVisionDescriber("fake-key", "gpt-4o-mini", ts.URL)
	if err != nil {
		t.Fatalf("NewOpenAIVisionDescriber: %v", err)
	}
	d.Client = ts.Client()
	out, err := d.Describe(context.Background(), VisionDescribeInput{Name: "fig.png", Format: "png", Data: []byte{9, 9}})
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if gotModel != "gpt-4o-mini" {
		t.Errorf("model = %q", gotModel)
	}
	if !strings.HasPrefix(gotImageURL, "data:image/png;base64,") {
		t.Errorf("image_url = %q", gotImageURL)
	}
	if out.Description != "Directed graph with nodes A, B, C." {
		t.Errorf("description = %q", out.Description)
	}
	if out.FigureType != VisionFigureDiagram {
		t.Errorf("figure_type = %q", out.FigureType)
	}
	if out.DescribedBy != "gpt-4o-mini" {
		t.Errorf("described_by = %q", out.DescribedBy)
	}
}

func TestOpenAIVisionDescribeNoData(t *testing.T) {
	d, _ := NewOpenAIVisionDescriber("k", "m", "http://example.com")
	if _, err := d.Describe(context.Background(), VisionDescribeInput{}); err == nil {
		t.Fatalf("expected error for empty image")
	}
}

func TestOpenAIVisionDescribeServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad"))
	}))
	defer ts.Close()
	d, _ := NewOpenAIVisionDescriber("k", "m", ts.URL)
	d.Client = ts.Client()
	if _, err := d.Describe(context.Background(), VisionDescribeInput{Data: []byte{1}}); err == nil {
		t.Fatalf("expected error on 400")
	}
}
