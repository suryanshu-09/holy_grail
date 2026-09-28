// Package study implements the Study Mode flow (Phase 27: Optional
// Advanced Features):
//
//	Topic -> Explanation -> Example -> PYQs -> Quiz
//
// BuildStudyGuide deterministically turns a topic plus its retrieved PYQs
// into a study guide: a short explanation, a worked example drawn from the
// first PYQ, key points extracted from PYQ term frequency, PYQ references,
// multimodal-derived notes (math expressions, figures, tables), and a
// practice quiz suggestion built by reusing quiz.BuildOriginalQuiz.
//
// Everything here is pure and deterministic: no I/O, no LLM calls, no
// database access. Callers (HTTP handlers) supply the PYQs via the
// retrieval stack.
package study

import (
	"fmt"
	"sort"
	"strings"

	"github.com/suryanshu-09/holy_grail/internal/multimodal"
	"github.com/suryanshu-09/holy_grail/internal/questions"
	"github.com/suryanshu-09/holy_grail/internal/quiz"
)

// Caps keep guides and suggested quizzes short and deterministic.
const (
	// MaxPYQRefs is the maximum PYQ references carried in a guide.
	MaxPYQRefs = 10
	// MaxNextQuiz is the maximum questions in the suggested practice quiz.
	MaxNextQuiz = 5
	// MaxKeyPoints caps the derived key points.
	MaxKeyPoints = 5
	// MaxExampleChars caps the worked-example text.
	MaxExampleChars = 500
	// MaxRefTextChars caps each PYQ reference text.
	MaxRefTextChars = 300
)

// PYQRef is a single past-question reference inside a study guide.
type PYQRef struct {
	// QuestionID is the source PYQ ID.
	QuestionID string `json:"question_id"`
	// DocumentID is the PYQ's document (backlink for "View Original").
	DocumentID string `json:"document_id"`
	// Text is the (possibly truncated) question stem.
	Text string `json:"text"`
	// Year is the PYQ year when recorded, nil when unknown.
	Year *int `json:"year,omitempty"`
	// Subject is the PYQ subject when recorded.
	Subject string `json:"subject,omitempty"`
}

// StudyGuide is the Topic -> Explanation -> Example -> PYQs -> Quiz flow.
type StudyGuide struct {
	// Topic is the requested topic (trimmed, as given).
	Topic string `json:"topic"`
	// Subject scopes the guide when provided.
	Subject string `json:"subject,omitempty"`
	// WeakTopic marks guides for topics flagged as weak (extra focus cue).
	WeakTopic bool `json:"weak_topic"`
	// Explanation is the deterministic topic overview.
	Explanation string `json:"explanation"`
	// Example is the worked example drawn from the first usable PYQ.
	Example string `json:"example"`
	// KeyPoints are deterministic takeaways derived from PYQ term frequency.
	KeyPoints []string `json:"key_points"`
	// PYQRefs lists the source past questions (capped at MaxPYQRefs).
	PYQRefs []PYQRef `json:"pyq_refs"`
	// Summary carries multimodal-derived notes (math, figures, tables).
	Summary string `json:"summary,omitempty"`
	// NextQuiz is the suggested practice quiz reusing quiz.BuildOriginalQuiz.
	NextQuiz quiz.QuizResponse `json:"next_quiz"`
}

// stopWords drops low-signal tokens from key-point term frequency. Kept
// intentionally small so content-bearing terms are never lost.
var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "is": true, "are": true,
	"was": true, "were": true, "be": true, "been": true, "being": true,
	"what": true, "which": true, "who": true, "whom": true, "how": true,
	"why": true, "when": true, "where": true, "of": true, "in": true,
	"on": true, "for": true, "to": true, "and": true, "or": true,
	"with": true, "from": true, "by": true, "as": true, "at": true,
	"it": true, "its": true, "this": true, "that": true, "these": true,
	"those": true, "explain": true, "describe": true, "discuss": true,
	"following": true, "using": true, "used": true, "question": true,
}

// tokenize splits text into lowercase alphanumeric tokens of length >= 3.
func tokenize(text string) []string {
	lower := strings.ToLower(text)
	fields := strings.FieldsFunc(lower, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if len(f) >= 3 && !stopWords[f] {
			out = append(out, f)
		}
	}
	return out
}

// truncate shortens s to max chars (rune-safe), appending "..." when cut.
func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) <= max {
		return s
	}
	runes := []rune(s)
	return strings.TrimSpace(string(runes[:max])) + "..."
}

// pyqText returns the trimmed stem of a PYQ, or "" when absent.
func pyqText(q questions.Question) string {
	if q.QuestionText == nil {
		return ""
	}
	return strings.TrimSpace(*q.QuestionText)
}

// strVal dereferences an optional string pointer for display.
func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

// buildExplanation renders the deterministic topic overview.
func buildExplanation(topic, subject string, total int, weak bool) string {
	var b strings.Builder
	if subject != "" {
		fmt.Fprintf(&b, "Study guide for %q in %s, grounded in %d past question(s). ", topic, subject, total)
	} else {
		fmt.Fprintf(&b, "Study guide for %q, grounded in %d past question(s). ", topic, total)
	}
	b.WriteString("Read the explanation, work through the example, review the linked PYQs, then attempt the practice quiz. ")
	if weak {
		b.WriteString("This topic is flagged as a weak area — spend extra time on the example and re-attempt the quiz until the key points feel routine. ")
	}
	if total == 0 {
		b.WriteString("No past questions were found for this topic yet; the key points below are generic until PYQs are indexed.")
		return b.String()
	}
	fmt.Fprintf(&b, "The key points distill the recurring ideas across the %d referenced PYQ(s).", total)
	return b.String()
}

// buildExample frames the first usable PYQ stem as a worked example.
func buildExample(pyqs []questions.Question) string {
	for _, q := range pyqs {
		if text := pyqText(q); text != "" {
			ref := q.ID
			if n := strVal(q.QuestionNumber); n != "" {
				ref = fmt.Sprintf("%s (question %s)", q.ID, n)
			}
			return fmt.Sprintf("Worked example from PYQ %s: %s Walk through it step by step — identify the concept tested, recall the key points below, then check your answer against the quiz.", ref, truncate(text, MaxExampleChars))
		}
	}
	return "No worked example is available yet — attempt the practice quiz once PYQs are indexed for this topic."
}

// termCount is a deterministic term-frequency row.
type termCount struct {
	term  string
	count int
	cover int // number of distinct PYQs containing the term
}

// buildKeyPoints extracts up to MaxKeyPoints takeaways from PYQ term
// frequency: terms ranked by (coverage desc, count desc, term asc) so the
// output is stable for identical input.
func buildKeyPoints(pyqs []questions.Question) []string {
	counts := make(map[string]int)
	cover := make(map[string]int)
	usable := 0
	for _, q := range pyqs {
		text := pyqText(q)
		if text == "" {
			continue
		}
		usable++
		seen := make(map[string]struct{})
		for _, tok := range tokenize(text) {
			counts[tok]++
			if _, ok := seen[tok]; !ok {
				seen[tok] = struct{}{}
				cover[tok]++
			}
		}
	}
	rows := make([]termCount, 0, len(counts))
	for term, count := range counts {
		rows = append(rows, termCount{term: term, count: count, cover: cover[term]})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].cover != rows[j].cover {
			return rows[i].cover > rows[j].cover
		}
		if rows[i].count != rows[j].count {
			return rows[i].count > rows[j].count
		}
		return rows[i].term < rows[j].term
	})
	out := []string{}
	for _, row := range rows {
		if len(out) >= MaxKeyPoints {
			break
		}
		out = append(out, fmt.Sprintf("Focus on %q — it recurs in %d of %d past question(s).", row.term, row.cover, usable))
	}
	if len(out) == 0 {
		out = append(out, "No recurring terms yet — review the PYQs directly once they are indexed.")
	}
	return out
}

// buildPYQRefs converts PYQs into capped reference rows, skipping stems
// that are empty.
func buildPYQRefs(pyqs []questions.Question) []PYQRef {
	out := []PYQRef{}
	for _, q := range pyqs {
		if len(out) >= MaxPYQRefs {
			break
		}
		text := pyqText(q)
		if text == "" {
			continue
		}
		out = append(out, PYQRef{
			QuestionID: q.ID,
			DocumentID: q.DocumentID,
			Text:       truncate(text, MaxRefTextChars),
			Year:       q.Year,
			Subject:    strVal(q.Subject),
		})
	}
	return out
}

// buildSummary derives deterministic multimodal notes across the PYQs:
// math expressions found in stems, figure references from ImagesJSON, and a
// table summary when a stem parses as table text. Empty when nothing is found.
func buildSummary(pyqs []questions.Question) string {
	seenMath := make(map[string]struct{})
	mathShown := []string{}
	figures := 0
	var tableNote string
	for _, q := range pyqs {
		text := pyqText(q)
		if text != "" {
			for _, e := range multimodal.ExtractMathExpressions(text) {
				n := multimodal.NormalizeMath(e)
				if n == "" {
					continue
				}
				if _, dup := seenMath[n]; dup {
					continue
				}
				seenMath[n] = struct{}{}
				if len(n) > 80 {
					n = n[:77] + "..."
				}
				mathShown = append(mathShown, n)
				if len(mathShown) == 3 {
					break
				}
			}
			if tableNote == "" {
				if rows := multimodal.ParseTableText(text); len(rows) >= 2 {
					tableNote = multimodal.SummarizeTable(rows)
				}
			}
		}
		if q.ImagesJSON != nil && strings.TrimSpace(*q.ImagesJSON) != "" {
			figures++
		}
		if len(mathShown) == 3 && tableNote != "" && figures > 0 {
			break
		}
	}
	parts := []string{}
	if len(mathShown) > 0 {
		parts = append(parts, "Math to review: "+strings.Join(mathShown, "; ")+".")
	}
	if figures > 0 {
		parts = append(parts, fmt.Sprintf("%d referenced PYQ(s) include figures — inspect the diagrams before the quiz.", figures))
	}
	if tableNote != "" {
		parts = append(parts, tableNote)
	}
	return strings.Join(parts, " ")
}

// quizSources returns the first usable PYQs (capped) for the practice quiz.
func quizSources(pyqs []questions.Question) []questions.Question {
	out := make([]questions.Question, 0, MaxNextQuiz)
	for _, q := range pyqs {
		if len(out) >= MaxNextQuiz {
			break
		}
		if strings.TrimSpace(q.ID) == "" || pyqText(q) == "" {
			continue
		}
		out = append(out, q)
	}
	return out
}

// BuildStudyGuide builds the Topic -> Explanation -> Example -> PYQs ->
// Quiz flow for topic (required, trimmed) scoped to subject (optional).
// pyqs are the retrieved past questions for the topic (any order; output is
// deterministic); weak marks the guide for a weak topic. The NextQuiz
// suggestion reuses quiz.BuildOriginalQuiz over the first MaxNextQuiz
// usable PYQs, so it is always valid even when the LLM is unavailable.
func BuildStudyGuide(topic, subject string, pyqs []questions.Question, weak bool) StudyGuide {
	topic = strings.TrimSpace(topic)
	subject = strings.TrimSpace(subject)
	usable := 0
	for _, q := range pyqs {
		if pyqText(q) != "" {
			usable++
		}
	}
	return StudyGuide{
		Topic:       topic,
		Subject:     subject,
		WeakTopic:   weak,
		Explanation: buildExplanation(topic, subject, usable, weak),
		Example:     buildExample(pyqs),
		KeyPoints:   buildKeyPoints(pyqs),
		PYQRefs:     buildPYQRefs(pyqs),
		Summary:     buildSummary(pyqs),
		NextQuiz:    quiz.BuildOriginalQuiz(quizSources(pyqs)),
	}
}
