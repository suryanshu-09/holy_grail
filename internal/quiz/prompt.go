package quiz

import (
	"fmt"
	"sort"
	"strings"

	"github.com/suryanshu-09/holy_grail/internal/questions"
)

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
		out = append(out, b)
	}
	return out
}

// renderSources renders retrieved PYQs as numbered source blocks with IDs.
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
		fmt.Fprintf(&b, "text: %s\n\n", s.Text)
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
	b.WriteString("For each quiz item: copy the source text unchanged into \"question\", then write 4 options where one option is the correct answer entailed by the source, plus an explanation citing the source.\n\n")
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
	b.WriteString("The MCQ stem must test the same concept as the source; options must be plausible and mutually exclusive with exactly one correct answer.\n\n")
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
	b.WriteString("The new question must still be answerable from the knowledge tested by the source; the explanation must reference the source concept.\n\n")
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
	b.WriteString("Mode: MIXED QUIZ. Combine styles across items: for roughly the first third use the original wording (Mode 1), the second third MCQ conversions (Mode 2), and the final third similar-but-new questions (Mode 3). With fewer than 3 items, cycle through the styles in order.\n\n")
	b.WriteString("Constraints:\n" + renderConstraints(req) + "\n\n")
	b.WriteString("Retrieved PYQs:\n" + renderSources(blocks) + "\n\n")
	b.WriteString(schemaInstruction)
	return b.String()
}
