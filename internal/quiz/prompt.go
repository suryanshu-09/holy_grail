package quiz

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/suryanshu-09/holy_grail/internal/questions"
)

// SourceImage carries one visual reference attached to a retrieved PYQ.
//
// Name is the image identifier (e.g. storage name); Description is the
// optional vision-model caption (empty when undescribed); FigureType is the
// optional normalized figure classification (diagram, graph, math_figure,
// table, chart, photo, unknown; empty means unclassified).
type SourceImage struct {
	Name        string
	Description string
	FigureType  string
}

// SourceBlock is the rendered form of one retrieved PYQ passed to the LLM.
// It carries the IDs the model must echo back for traceability.
type SourceBlock struct {
	SourceQuestionID string
	DocumentID       string
	QuestionNumber   string
	Subject          string
	Topics           string
	Difficulty       string
	QuestionType     string
	Text             string
	// Images carries visual references parsed from ImagesJSON (descriptions
	// included when the vision pipeline produced them). Empty when the
	// source question has no associated images.
	Images []SourceImage
}

// HasVisual reports whether the block carries any image references.
func (b SourceBlock) HasVisual() bool { return len(b.Images) > 0 }

// normalizeFigureType lowercases/trims a figure-type label and maps common
// synonyms to the canonical set. Empty input stays empty (unclassified);
// unknown labels map to "unknown".
func normalizeFigureType(s string) string {
	t := strings.ToLower(strings.TrimSpace(s))
	t = strings.ReplaceAll(t, "-", "_")
	t = strings.ReplaceAll(t, " ", "_")
	switch t {
	case "":
		return ""
	case "diagram", "diagrams":
		return "diagram"
	case "graph", "graphs", "plot", "plots":
		return "graph"
	case "math_figure", "mathfigure", "math", "mathematical_figure", "formula", "equation":
		return "math_figure"
	case "table", "tables":
		return "table"
	case "chart", "charts", "bar_chart", "pie_chart", "line_chart", "histogram":
		return "chart"
	case "photo", "photos", "photograph", "image", "images", "figure", "figures":
		return "photo"
	case "unknown":
		return "unknown"
	default:
		return t
	}
}

// parseImagesJSON decodes the ImagesJSON blob stored on a question into
// SourceImage entries. It accepts both the legacy format (JSON array of
// image-name strings) and the vision-enriched format (JSON array of objects
// with name/storage_path + description + figure_type). Invalid or empty
// input yields nil; parsing never fails the caller.
func parseImagesJSON(raw string) []SourceImage {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil
	}
	out := make([]SourceImage, 0, len(items))
	for _, item := range items {
		// Legacy string entry: just an image name.
		var name string
		if err := json.Unmarshal(item, &name); err == nil {
			if strings.TrimSpace(name) != "" {
				out = append(out, SourceImage{Name: strings.TrimSpace(name)})
			}
			continue
		}
		// Vision-enriched object entry (ImageRef-compatible).
		var obj struct {
			Name          string `json:"name"`
			StoragePath   string `json:"storage_path"`
			ThumbnailPath string `json:"thumbnail_path"`
			Description   string `json:"description"`
			FigureType    string `json:"figure_type"`
			FigureKind    string `json:"figure_kind"`
			Kind          string `json:"kind"`
			Type          string `json:"type"`
		}
		if err := json.Unmarshal(item, &obj); err != nil {
			continue
		}
		n := strings.TrimSpace(obj.Name)
		if n == "" {
			n = strings.TrimSpace(obj.StoragePath)
		}
		if n == "" {
			continue
		}
		ft := normalizeFigureType(obj.FigureType)
		if ft == "" {
			ft = normalizeFigureType(obj.FigureKind)
		}
		if ft == "" {
			ft = normalizeFigureType(obj.Kind)
		}
		if ft == "" {
			ft = normalizeFigureType(obj.Type)
		}
		out = append(out, SourceImage{
			Name:        n,
			Description: strings.TrimSpace(obj.Description),
			FigureType:  ft,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ToSourceBlocks converts retrieved questions into deterministic prompt blocks.
func ToSourceBlocks(srcs []questions.Question) []SourceBlock {
	out := make([]SourceBlock, 0, len(srcs))
	for _, q := range srcs {
		b := SourceBlock{
			SourceQuestionID: q.ID,
			DocumentID:       q.DocumentID,
		}
		if q.QuestionNumber != nil {
			b.QuestionNumber = strings.TrimSpace(*q.QuestionNumber)
		}
		if q.Subject != nil {
			b.Subject = strings.TrimSpace(*q.Subject)
		}
		if q.Difficulty != nil {
			b.Difficulty = strings.TrimSpace(*q.Difficulty)
		}
		if q.QuestionType != nil {
			b.QuestionType = strings.TrimSpace(*q.QuestionType)
		}
		if q.QuestionText != nil {
			b.Text = strings.TrimSpace(*q.QuestionText)
		}
		if q.ImagesJSON != nil {
			b.Images = parseImagesJSON(*q.ImagesJSON)
		}
		out = append(out, b)
	}
	return out
}

// renderSources renders retrieved PYQs as numbered source blocks with IDs.
// Image references (with vision descriptions + figure types when available)
// are rendered inline so the LLM can preserve/explain visual content instead
// of dropping it.
func renderSources(blocks []SourceBlock) string {
	var b strings.Builder
	for i, s := range blocks {
		fmt.Fprintf(&b, "[Source %d]\nsource_question_id: %s\ndocument_id: %s\n", i+1, s.SourceQuestionID, s.DocumentID)
		if s.QuestionNumber != "" {
			fmt.Fprintf(&b, "question_number: %s\n", s.QuestionNumber)
		}
		if s.Subject != "" {
			fmt.Fprintf(&b, "subject: %s\n", s.Subject)
		}
		if s.Topics != "" {
			fmt.Fprintf(&b, "topics: %s\n", s.Topics)
		}
		if s.Difficulty != "" {
			fmt.Fprintf(&b, "difficulty: %s\n", s.Difficulty)
		}
		if s.QuestionType != "" {
			fmt.Fprintf(&b, "question_type: %s\n", s.QuestionType)
		}
		fmt.Fprintf(&b, "text: %s\n", s.Text)
		if len(s.Images) > 0 {
			fmt.Fprintf(&b, "has_image: true\n")
			for _, img := range s.Images {
				fmt.Fprintf(&b, "image: %s\n", img.Name)
				if img.FigureType != "" {
					fmt.Fprintf(&b, "figure_type: %s\n", img.FigureType)
				}
				if img.Description != "" {
					fmt.Fprintf(&b, "image_description: %s\n", img.Description)
				}
			}
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderConstraints renders length/difficulty/topic/subject filtering rules.
func renderConstraints(req QuizRequest) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("- Generate EXACTLY %d quiz question(s).\n", req.NumQuestions))
	if req.Difficulty != "" {
		fmt.Fprintf(&b, "- Target difficulty: %s. Prefer sources matching this difficulty; set question style accordingly.\n", req.NormalizedDifficulty())
	}
	if len(req.Topics) > 0 {
		topics := append([]string{}, req.Topics...)
		sort.Strings(topics)
		fmt.Fprintf(&b, "- Topic filter: %s. Only use sources relevant to these topics.\n", strings.Join(topics, ", "))
	}
	if req.Subject != "" {
		fmt.Fprintf(&b, "- Subject filter: %s. Only use sources from this subject.\n", req.Subject)
	}
	if req.Query != "" {
		fmt.Fprintf(&b, "- User request: %q. Stay relevant to it.\n", req.Query)
	}
	return strings.TrimRight(b.String(), "\n")
}

// schemaInstruction is the strict output contract shared by all modes.
const schemaInstruction = `Respond ONLY with a JSON object of the form:
{"questions": [{"id": "string (optional)", "source_question_id": "string (required)", "document_id": "string (required)", "question": "string (required)", "options": ["4 strings, required]", "correct_answer": 0-3 (required), "explanation": "string (required)"}]}
Rules:
- source_question_id MUST be one of the source_question_id values listed above (no invention).
- document_id MUST equal the document_id of that source question.
- question must be non-empty.
- options must contain EXACTLY 4 distinct non-empty strings.
- correct_answer must be the 0-based index of the correct option (0-3).
- explanation must be non-empty and grounded in the source PYQ.
- Every quiz question must have a distinct question text and a distinct source_question_id (no duplicates).
- Return ONLY the JSON object, no markdown fences, no extra text.`

// groundRule is the anti-hallucination header shared by all modes.
const groundRule = `You are a quiz generator. Retrieved Previous Year Questions (PYQs) below are the ONLY source of truth. Do NOT invent source material, IDs, or facts beyond what the sources support.`

// visualContextInstruction tells the model how to handle image references.
// Sources may carry image/image_description/figure_type lines produced by a
// vision pipeline (diagrams, graphs, math figures, tables, charts, photos).
// The instruction is mode-agnostic: preserve visual content instead of
// dropping it, and never invent visual details beyond the description.
const visualContextInstruction = `Visual context: a source may list image(s) with an image_description and figure_type (diagram/graph/math_figure/table/chart/photo). When present, treat the description as part of the source of truth: preserve or explain the visual content in the quiz question/options/explanation as needed (e.g. "as shown in the diagram ..."), do NOT drop figure-dependent information, and do NOT invent visual details beyond the given description. When a source has no image lines, it has no visual content — do not mention figures for it.`

// BuildQuizPrompt dispatches to the per-mode builder after validating the request.
func BuildQuizPrompt(req QuizRequest, sources []questions.Question) (string, error) {
	if err := req.Validate(); err != nil {
		return "", err
	}
	if len(sources) == 0 {
		return "", fmt.Errorf("quiz: no source questions provided")
	}
	switch req.Mode {
	case ModeOriginal:
		return BuildOriginalPrompt(req, sources), nil
	case ModeMCQ:
		return BuildMCQPrompt(req, sources), nil
	case ModeSimilar:
		return BuildSimilarPrompt(req, sources), nil
	case ModeMixed:
		return BuildMixedPrompt(req, sources), nil
	default:
		return "", fmt.Errorf("quiz: invalid mode %q", string(req.Mode))
	}
}

// BuildOriginalPrompt shows each original PYQ directly as the quiz question,
// adding 3 plausible distractors + explanation without altering the stem.
func BuildOriginalPrompt(req QuizRequest, sources []questions.Question) string {
	blocks := ToSourceBlocks(sources)
	var b strings.Builder
	b.WriteString(groundRule + "\n\n")
	b.WriteString("Mode: ORIGINAL PYQs. Use the original question wording verbatim as the quiz question stem.\n")
	b.WriteString("For each quiz item: copy the source text unchanged into \"question\", then write 4 options where one option is the correct answer entailed by the source, plus an explanation citing the source.\n")
	b.WriteString(visualContextInstruction + "\n")
	b.WriteString("For image-bearing sources, keep the stem verbatim but carry the visual reference into the quiz item (e.g. append the figure cue such as [Figure: <name>] and explain the visual in the explanation) so figure-dependent questions stay answerable.\n\n")
	b.WriteString("Constraints:\n" + renderConstraints(req) + "\n\n")
	b.WriteString("Retrieved PYQs:\n" + renderSources(blocks) + "\n\n")
	b.WriteString(schemaInstruction)
	return b.String()
}

// BuildMCQPrompt converts each descriptive PYQ into a 4-option MCQ.
func BuildMCQPrompt(req QuizRequest, sources []questions.Question) string {
	blocks := ToSourceBlocks(sources)
	var b strings.Builder
	b.WriteString(groundRule + "\n\n")
	b.WriteString("Mode: MCQ CONVERSION. Convert each descriptive PYQ into a self-contained multiple-choice question.\n")
	b.WriteString("The MCQ stem must test the same concept as the source; options must be plausible and mutually exclusive with exactly one correct answer.\n")
	b.WriteString(visualContextInstruction + "\n")
	b.WriteString("For image-bearing sources, make the MCQ stem self-contained by summarizing the visual content from the image_description (do not assume the test-taker sees the raw image) and keep the explanation grounded in both text and visual.\n\n")
	b.WriteString("Constraints:\n" + renderConstraints(req) + "\n\n")
	b.WriteString("Retrieved PYQs:\n" + renderSources(blocks) + "\n\n")
	b.WriteString(schemaInstruction)
	return b.String()
}

// BuildSimilarPrompt generates a new question based on (but distinct from) a PYQ.
func BuildSimilarPrompt(req QuizRequest, sources []questions.Question) string {
	blocks := ToSourceBlocks(sources)
	var b strings.Builder
	b.WriteString(groundRule + "\n\n")
	b.WriteString("Mode: SIMILAR QUESTION. Write a NEW question inspired by each source PYQ: same concept/topic and difficulty, but different wording, numbers, or scenario. Do NOT copy the source text verbatim.\n")
	b.WriteString("The new question must still be answerable from the knowledge tested by the source; the explanation must reference the source concept.\n")
	b.WriteString(visualContextInstruction + "\n")
	b.WriteString("For image-bearing sources, create an analogous visual scenario described in words (same figure_type, e.g. a new diagram/graph/table with different values) rather than copying the original figure; the new question must remain solvable from its own stem.\n\n")
	b.WriteString("Constraints:\n" + renderConstraints(req) + "\n\n")
	b.WriteString("Retrieved PYQs:\n" + renderSources(blocks) + "\n\n")
	b.WriteString(schemaInstruction)
	return b.String()
}

// BuildMixedPrompt combines question types across the quiz.
func BuildMixedPrompt(req QuizRequest, sources []questions.Question) string {
	blocks := ToSourceBlocks(sources)
	var b strings.Builder
	b.WriteString(groundRule + "\n\n")
	b.WriteString("Mode: MIXED QUIZ. Combine styles across items: for roughly the first third use the original wording (Mode 1), the second third MCQ conversions (Mode 2), and the final third similar-but-new questions (Mode 3). With fewer than 3 items, cycle through the styles in order.\n")
	b.WriteString(visualContextInstruction + "\n")
	b.WriteString("Apply the per-style visual rules above to each item according to the style it uses.\n\n")
	b.WriteString("Constraints:\n" + renderConstraints(req) + "\n\n")
	b.WriteString("Retrieved PYQs:\n" + renderSources(blocks) + "\n\n")
	b.WriteString(schemaInstruction)
	return b.String()
}
