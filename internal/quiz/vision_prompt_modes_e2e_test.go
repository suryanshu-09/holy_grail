package quiz

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/questions"
)

func e2eStrPtr(s string) *string { return &s }

// e2eImageJSON builds an extraction-shaped ImagesJSON manifest entry (as
// persisted from extraction.ImageRef via pages.json/images.json), including
// storage_path/thumbnail/described_by alongside the vision fields.
func e2eImageJSON(name, desc, figureType string) string {
	raw, _ := json.Marshal([]map[string]string{{
		"name":           name,
		"storage_path":   "images/" + name,
		"thumbnail_path": "thumbs/" + name,
		"description":    desc,
		"figure_type":    figureType,
		"described_by":   "gpt-4o-mini",
	}})
	return string(raw)
}

// e2eFigureSources returns one image-bearing source per required figure type
// (diagrams, graphs, math figures, tables, charts).
func e2eFigureSources() []questions.Question {
	figs := []struct {
		ft   string
		desc string
	}{
		{"diagram", "Circuit diagram with resistor R1 in series with capacitor C1."},
		{"graph", "Directed graph with nodes A, B, C and weighted edges."},
		{"math_figure", "Right triangle with sides 3, 4, 5 for Pythagoras proof."},
		{"table", "Table of burst times and priorities for four processes."},
		{"chart", "Bar chart of quarterly revenue for 2024."},
	}
	out := make([]questions.Question, 0, len(figs))
	for i, f := range figs {
		imgs := e2eImageJSON("fig-"+f.ft+".png", f.desc, f.ft)
		qtext := "Study the figure and answer question " + string(rune('1'+i)) + " in detail?"
		sub := "General"
		out = append(out, questions.Question{
			ID:           "e2e-q-" + f.ft,
			DocumentID:   "e2e-d1",
			QuestionText: e2eStrPtr(qtext),
			Subject:      e2eStrPtr(sub),
			ImagesJSON:   e2eStrPtr(imgs),
		})
	}
	return out
}

// Phase 18 (Task 11): table-driven image-aware prompt rendering across all 4
// modes for every required figure type. Each mode must carry the image lines
// plus the visual-context instruction while keeping the strict schema.
func TestPhase18_ImageAwarePrompt_AllModesTableDriven(t *testing.T) {
	srcs := e2eFigureSources()
	modes := []struct {
		mode   QuizMode
		build  func(QuizRequest, []questions.Question) string
		marker string
	}{
		{ModeOriginal, BuildOriginalPrompt, "ORIGINAL"},
		{ModeMCQ, BuildMCQPrompt, "MCQ CONVERSION"},
		{ModeSimilar, BuildSimilarPrompt, "SIMILAR QUESTION"},
		{ModeMixed, BuildMixedPrompt, "MIXED QUIZ"},
	}
	for _, m := range modes {
		for _, src := range srcs {
			req := QuizRequest{Mode: m.mode, NumQuestions: 1}
			p := m.build(req, []questions.Question{src})
			imgs := parseImagesJSON(*src.ImagesJSON)
			if len(imgs) != 1 {
				t.Fatalf("mode %s %s: parseImagesJSON lost image", m.mode, src.ID)
			}
			ft := strings.TrimPrefix(src.ID, "e2e-q-")
			for _, want := range []string{
				"image: fig-" + ft + ".png",
				"figure_type: " + ft,
				"image_description: " + imgs[0].Description,
				"has_image: true",
				"Visual context:",
				m.marker,
				"Return ONLY the JSON object",
				"source_question_id",
			} {
				if !strings.Contains(p, want) {
					t.Errorf("mode %s %s: prompt missing %q", m.mode, src.ID, want)
				}
			}
		}
		// Dispatcher agrees with the direct builder for this mode.
		req := QuizRequest{Mode: m.mode, NumQuestions: 1}
		if _, err := BuildQuizPrompt(req, srcs[:1]); err != nil {
			t.Errorf("BuildQuizPrompt(%s): %v", m.mode, err)
		}
	}
}

// Phase 18 (Task 11): fallback stays image-aware through the generator, both
// when no LLM is configured and when the LLM keeps failing.
func TestPhase18_ImageAwareFallback_ViaGenerator(t *testing.T) {
	srcs := e2eFigureSources()[:2]

	// No LLM: deterministic fallback must preserve every figure hint.
	g := &QuizGenerator{Retriever: RetrieveFunc(func(context.Context, QuizRequest) ([]questions.Question, error) {
		return srcs, nil
	})}
	res, err := g.GenerateWithMeta(context.Background(), QuizRequest{Mode: ModeMCQ, NumQuestions: 2})
	if err != nil {
		t.Fatalf("nil-LLM GenerateWithMeta: %v", err)
	}
	if !res.Fallback {
		t.Fatalf("nil LLM must use fallback")
	}
	if len(res.Quiz.Questions) != 2 {
		t.Fatalf("fallback produced %d questions, want 2", len(res.Quiz.Questions))
	}
	for _, q := range res.Quiz.Questions {
		if !strings.Contains(q.Question, "[Figure:") {
			t.Errorf("fallback stem missing [Figure:] hint: %q", q.Question)
		}
		if !strings.Contains(q.Explanation, "Visual reference(s):") {
			t.Errorf("fallback explanation missing visual note: %q", q.Explanation)
		}
	}
	if err := ValidateAgainstSources(res.Quiz, srcs); err != nil {
		t.Fatalf("image-aware fallback must validate: %v", err)
	}

	// Failing LLM: after retries the same image-aware fallback applies.
	g3 := &QuizGenerator{
		Retriever: RetrieveFunc(func(context.Context, QuizRequest) ([]questions.Question, error) {
			return srcs, nil
		}),
		LLM:         LLMFunc(func(context.Context, string) (string, error) { return "", errE2EBoom{} }),
		MaxAttempts: 2,
	}
	// No-op sleep without importing time in signature mismatch: use default
	// backoff path but attempts are fast (100ms/200ms); keep MaxAttempts small.
	res2, err := g3.GenerateWithMeta(context.Background(), QuizRequest{Mode: ModeSimilar, NumQuestions: 1})
	if err != nil {
		t.Fatalf("failing-LLM GenerateWithMeta: %v", err)
	}
	if !res2.Fallback || len(res2.Quiz.Questions) != 1 {
		t.Fatalf("failing LLM must fall back to 1 question, got %+v", res2)
	}
	if !strings.Contains(res2.Quiz.Questions[0].Question, "[Figure:") {
		t.Errorf("failing-LLM fallback stem missing figure hint: %q", res2.Quiz.Questions[0].Question)
	}
}

type errE2EBoom struct{}

func (errE2EBoom) Error() string { return "boom" }

// Phase 18 (Task 11): end-to-end extraction-manifest -> quiz prompt with
// images. Extraction-shaped ImagesJSON (storage_path/described_by included)
// must survive ToSourceBlocks into every mode prompt plus the fallback.
func TestPhase18_ExtractionToQuiz_EndToEndWithImages(t *testing.T) {
	srcs := e2eFigureSources()

	blocks := ToSourceBlocks(srcs)
	if len(blocks) != len(srcs) {
		t.Fatalf("blocks = %d, want %d", len(blocks), len(srcs))
	}
	for i, b := range blocks {
		if !b.HasVisual() {
			t.Fatalf("block %d (%s) should be visual", i, srcs[i].ID)
		}
		if b.Images[0].Description == "" || b.Images[0].FigureType == "" {
			t.Fatalf("block %d lost vision fields: %+v", i, b.Images[0])
		}
	}

	builders := map[QuizMode]func(QuizRequest, []questions.Question) string{
		ModeOriginal: BuildOriginalPrompt,
		ModeMCQ:      BuildMCQPrompt,
		ModeSimilar:  BuildSimilarPrompt,
		ModeMixed:    BuildMixedPrompt,
	}
	for mode, fn := range builders {
		p := fn(QuizRequest{Mode: mode, NumQuestions: len(srcs)}, srcs)
		for _, src := range srcs {
			imgs := parseImagesJSON(*src.ImagesJSON)
			if !strings.Contains(p, imgs[0].Description) {
				t.Errorf("mode %s dropped description for %s", mode, src.ID)
			}
			if !strings.Contains(p, "figure_type: "+imgs[0].FigureType) {
				t.Errorf("mode %s dropped figure_type for %s", mode, src.ID)
			}
		}
		if !strings.Contains(p, "Visual context:") {
			t.Errorf("mode %s missing visual-context instruction", mode)
		}
	}

	// Fallback end of the chain: every figure type yields a hint.
	fb := BuildOriginalQuiz(srcs)
	if len(fb.Questions) != len(srcs) {
		t.Fatalf("fallback = %d questions, want %d", len(fb.Questions), len(srcs))
	}
	for _, q := range fb.Questions {
		if !strings.Contains(q.Question, "[Figure:") {
			t.Errorf("fallback stem missing hint for %s: %q", q.SourceQuestionID, q.Question)
		}
	}
	if err := ValidateAgainstSources(fb, srcs); err != nil {
		t.Fatalf("end-to-end fallback must validate: %v", err)
	}

	// No-image sources stay hint-free (backwards compatibility).
	plain := questions.Question{ID: "e2e-plain", DocumentID: "e2e-d1", QuestionText: e2eStrPtr("What is paging?")}
	fbPlain := BuildOriginalQuiz([]questions.Question{plain})
	if strings.Contains(fbPlain.Questions[0].Question, "[Figure:") {
		t.Errorf("plain fallback should have no hint: %q", fbPlain.Questions[0].Question)
	}
	pPlain := BuildMCQPrompt(QuizRequest{Mode: ModeMCQ, NumQuestions: 1}, []questions.Question{plain})
	if strings.Contains(pPlain, "has_image:") {
		t.Errorf("plain prompt should have no image lines")
	}
}
