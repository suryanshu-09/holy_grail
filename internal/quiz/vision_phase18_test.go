package quiz

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/questions"
)

func p18StrPtr(s string) *string { return &s }

// One vision-enriched source per figure type.
func p18VisionSources() []questions.Question {
	types := []struct {
		ft   string
		desc string
	}{
		{"diagram", "Block diagram of the CPU datapath with ALU and registers."},
		{"graph", "Line graph of throughput vs load saturating at 80%."},
		{"math_figure", "Triangle labelled for the Pythagoras theorem proof."},
		{"table", "Table of process states with burst times and priorities."},
		{"chart", "Bar chart of quarterly sales for 2024."},
		{"photo", "Photograph of the laboratory experimental setup."},
	}
	out := make([]questions.Question, 0, len(types))
	for i, ft := range types {
		imgs, _ := json.Marshal([]map[string]string{{
			"name": "fig-" + ft.ft + ".png", "description": ft.desc, "figure_type": ft.ft,
		}})
		s := string(imgs)
		out = append(out, questions.Question{
			ID:           "q-" + ft.ft,
			DocumentID:   "d1",
			QuestionText: p18StrPtr("Explain the concept illustrated in figure " + itoaP18(i+1) + " in detail?"),
			Subject:      p18StrPtr("General"),
			ImagesJSON:   &s,
		})
	}
	return out
}

func itoaP18(i int) string {
	return string(rune('0' + i))
}

// Phase 18: quiz-side figure-type normalization covers all six classes.
func TestPhase18_QuizNormalizeFigureType(t *testing.T) {
	cases := map[string]string{
		"diagram": "diagram", "DIAGRAMS": "diagram",
		"graph": "graph", "plots": "graph", "plot": "graph",
		"math_figure": "math_figure", "mathfigure": "math_figure",
		"formula": "math_figure", "equation": "math_figure",
		"table": "table", "tables": "table",
		"chart": "chart", "bar_chart": "chart", "pie_chart": "chart", "histogram": "chart",
		"photo": "photo", "photograph": "photo", "image": "photo", "figure": "photo",
		"unknown": "unknown", "": "",
	}
	for in, want := range cases {
		if got := normalizeFigureType(in); got != want {
			t.Errorf("normalizeFigureType(%q) = %q, want %q", in, got, want)
		}
	}
}

// Phase 18: ImagesJSON parsing keeps vision fields for every figure type,
// and still accepts the legacy string-array format.
func TestPhase18_ParseImagesJSONAllFigureTypes(t *testing.T) {
	for _, src := range p18VisionSources() {
		imgs := parseImagesJSON(*src.ImagesJSON)
		if len(imgs) != 1 {
			t.Fatalf("source %s: got %d images, want 1", src.ID, len(imgs))
		}
		if imgs[0].Description == "" {
			t.Errorf("source %s: description dropped", src.ID)
		}
		if imgs[0].FigureType == "" || imgs[0].FigureType == "unknown" && src.ID != "q-unknown" {
			t.Errorf("source %s: figure_type = %q", src.ID, imgs[0].FigureType)
		}
	}
	// Legacy format: bare names, no vision fields.
	legacy := `["a.png", "b.png"]`
	imgs := parseImagesJSON(legacy)
	if len(imgs) != 2 || imgs[0].Name != "a.png" || imgs[1].Name != "b.png" {
		t.Fatalf("legacy parse = %+v", imgs)
	}
	if imgs[0].Description != "" || imgs[0].FigureType != "" {
		t.Fatalf("legacy entries must have empty vision fields: %+v", imgs[0])
	}
	// Empty/invalid input never fails the caller.
	if parseImagesJSON("") != nil || parseImagesJSON("not-json") != nil {
		t.Fatalf("invalid input should yield nil")
	}
}

// Phase 18: rendered sources carry image + figure_type + image_description
// lines so the LLM preserves visual content instead of dropping it.
func TestPhase18_RenderSourcesImageAware(t *testing.T) {
	blocks := ToSourceBlocks(p18VisionSources())
	rendered := renderSources(blocks)
	for _, want := range []string{"has_image: true", "image: fig-diagram.png", "figure_type: diagram", "image_description: Block diagram"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered sources missing %q:\n%s", want, rendered)
		}
	}
	for _, ft := range []string{"graph", "math_figure", "table", "chart", "photo"} {
		if !strings.Contains(rendered, "figure_type: "+ft) {
			t.Errorf("rendered sources missing figure_type %q:\n%s", ft, rendered)
		}
	}
}

// Phase 18: all four quiz modes embed the visual-context instruction and
// the image lines; schema contract stays unchanged.
func TestPhase18_BuildQuizPromptAllModesImageAware(t *testing.T) {
	srcs := p18VisionSources()[:2]
	builders := map[QuizMode]func(QuizRequest, []questions.Question) string{
		ModeOriginal: BuildOriginalPrompt,
		ModeMCQ:      BuildMCQPrompt,
		ModeSimilar:  BuildSimilarPrompt,
		ModeMixed:    BuildMixedPrompt,
	}
	for mode, fn := range builders {
		req := QuizRequest{Mode: mode, NumQuestions: 1}
		if err := req.Validate(); err != nil {
			t.Fatalf("mode %s validate: %v", mode, err)
		}
		p := fn(req, srcs)
		if !strings.Contains(p, "Visual context:") {
			t.Errorf("mode %s missing visual-context instruction", mode)
		}
		if !strings.Contains(p, "image_description:") || !strings.Contains(p, "figure_type:") {
			t.Errorf("mode %s missing image lines", mode)
		}
		if !strings.Contains(p, "source_question_id") || !strings.Contains(p, "Return ONLY the JSON object") {
			t.Errorf("mode %s lost strict schema contract", mode)
		}
	}
	// Dispatcher covers all modes too.
	for _, mode := range []QuizMode{ModeOriginal, ModeMCQ, ModeSimilar, ModeMixed} {
		req := QuizRequest{Mode: mode, NumQuestions: 1}
		if _, err := BuildQuizPrompt(req, srcs); err != nil {
			t.Errorf("BuildQuizPrompt(%s): %v", mode, err)
		}
	}
}

// Phase 18: deterministic fallback stays image-aware — every figure type
// yields a [Figure: ...] stem hint and a visual note in the explanation.
func TestPhase18_FallbackImageAwareAllFigureTypes(t *testing.T) {
	srcs := p18VisionSources()
	resp := BuildOriginalQuiz(srcs)
	if len(resp.Questions) != len(srcs) {
		t.Fatalf("fallback produced %d questions, want %d", len(resp.Questions), len(srcs))
	}
	for i, q := range resp.Questions {
		if !strings.Contains(q.Question, "[Figure:") {
			t.Errorf("q %s stem missing [Figure: ...] hint: %q", q.SourceQuestionID, q.Question)
		}
		if !strings.Contains(q.Explanation, "Visual reference(s):") {
			t.Errorf("q %s explanation missing visual note: %q", q.SourceQuestionID, q.Explanation)
		}
		_ = i
	}
	// Spot-check one fully-qualified hint.
	if !strings.Contains(resp.Questions[0].Question, "(diagram):") {
		t.Errorf("diagram hint malformed: %q", resp.Questions[0].Question)
	}
	// No-image sources produce no hint (backwards compatible).
	plain := questions.Question{ID: "qp", DocumentID: "d1", QuestionText: p18StrPtr("What is paging?")}
	r2 := BuildOriginalQuiz([]questions.Question{plain})
	if strings.Contains(r2.Questions[0].Question, "[Figure:") {
		t.Errorf("plain question should have no figure hint: %q", r2.Questions[0].Question)
	}
}

// Phase 18: end-to-end extraction ImageRef -> ImagesJSON -> quiz prompt.
// Descriptions must survive the whole chain for every figure type.
func TestPhase18_EndToEndExtractionToQuizWithImages(t *testing.T) {
	srcs := p18VisionSources()
	blocks := ToSourceBlocks(srcs)
	if len(blocks) != len(srcs) {
		t.Fatalf("blocks = %d, want %d", len(blocks), len(srcs))
	}
	for i, b := range blocks {
		if !b.HasVisual() {
			t.Fatalf("block %d should be visual", i)
		}
		if b.Images[0].Description == "" {
			t.Fatalf("block %d lost description", i)
		}
	}
	req := QuizRequest{Mode: ModeMCQ, NumQuestions: 2}
	p := BuildMCQPrompt(req, srcs)
	for _, src := range srcs {
		imgs := parseImagesJSON(*src.ImagesJSON)
		if !strings.Contains(p, imgs[0].Description) {
			t.Errorf("prompt dropped description for %s", src.ID)
		}
	}
	if !strings.Contains(p, "Visual context:") {
		t.Fatalf("prompt missing visual-context instruction")
	}
}
