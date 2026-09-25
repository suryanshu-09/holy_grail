package extraction

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/suryanshu-09/holy_grail/internal/documents"
	"github.com/suryanshu-09/holy_grail/internal/questions"
)

// PreviewQuestion is the lightweight extraction representation returned by
// preview APIs and used internally before persisting into the questions table.
//
// Images holds the associated image names (legacy, always populated when a
// question has visuals). ImageDetails carries the full vision-enriched refs
// (name + description + figure_type + described_by when available) so quiz
// generation can preserve visual content. It is empty when the question has
// no images or when vision produced nothing (deterministic fallback).
type PreviewQuestion struct {
	Number          *string    `json:"number,omitempty"`
	Text            string     `json:"text"`
	StartPage       int        `json:"start_page"`
	EndPage         int        `json:"end_page"`
	StartOffset     *int       `json:"start_offset,omitempty"`
	EndOffset       *int       `json:"end_offset,omitempty"`
	Type            string     `json:"type,omitempty"`
	Options         []string   `json:"options,omitempty"`
	Confidence      float64    `json:"confidence,omitempty"`
	ExtractionNotes []string   `json:"extraction_notes,omitempty"`
	Images          []string   `json:"images,omitempty"`
	ImageDetails    []ImageRef `json:"image_details,omitempty"`
}

var (
	// questionStartRe matches numbered question starts in priority order:
	// explicit markers like "Q1.", "Q 1)", "Question 12:" and simple "1.", "1)".
	// It captures the question number and the remaining text on that line.
	questionStartRe = regexp.MustCompile(`(?i)^\s*(?:Q\s*\.?\s*|Question\s+)?(\d+)\s*[\.\)\:\-]?\s*(.*)`)
	// strictStartRe requires an explicit Q/Question prefix or a clear delimiter.
	// Used to rank confidence: strict matches score higher than bare-number matches.
	strictStartRe = regexp.MustCompile(`(?i)^\s*(?:Q\s*\.?\s*\d+[\.\)\:\-]?\s+|Question\s+\d+[\.\)\:\-]?\s+|\d+[\.\)]\s+).*`)
	// optionRe matches MCQ option lines like "A. ...", "B) ...", "(A) ...", "a. ...".
	optionRe = regexp.MustCompile(`(?i)^\s*(?:\(?[A-D]\)|\(?[A-D]\.|\b[A-D][\.\)])\s*(.*)`)
	// optionCollectRe captures the option body for storage.
	optionCollectRe = regexp.MustCompile(`(?i)^\s*(?:\(?([A-D])\)?[\.\)]?)\s*(.*)`)
	// subQuestionRe matches nested numbering that should stay inside a parent question.
	subQuestionRe = regexp.MustCompile(`(?i)^\s*(?:\(?[a-z]\)|\(?[ivxlcdm]+\)|\(?[a-z]\.|\(?[0-9]+\)|[0-9]+\([a-z]\))\s+.*`)
	// sectionHeaderRe matches non-question headers like "Section A", "Part I", "Instructions".
	sectionHeaderRe = regexp.MustCompile(`(?i)^\s*(?:Section|Part|Instructions|Note|Appendix|Unit)\b.*`)
	// numericalHintRe hints at numerical questions.
	numericalHintRe = regexp.MustCompile(`(?i)\b(calculate|compute|evaluate|find the value|numerical|solve|determine the|what is the value|average waiting time)\b`)
	// trueFalseHintRe hints at true/false questions.
	trueFalseHintRe = regexp.MustCompile(`(?i)\b(true\s*/\s*false|true or false)\b`)
	// descriptiveHintRe hints at descriptive questions via common verbs.
	descriptiveHintRe = regexp.MustCompile(`(?i)\b(explain|describe|discuss|elaborate|state|define|illustrate|compare|discuss the|explain the|with neat diagram)\b`)
)

// parseQuestionsFromExtraction runs deterministic heuristics over the document
// pages and returns preview question objects including start/end page ranges.
func parseQuestionsFromExtraction(de DocumentExtraction) []PreviewQuestion {
	var out []PreviewQuestion
	var buf strings.Builder
	var curNum *string
	var curNotes []string
	startPage := 0
	curEndPage := 0
	startOffset := 0
	endOffset := 0
	var startPageText string

	flush := func() {
		if buf.Len() == 0 {
			return
		}
		text := strings.TrimSpace(buf.String())
		if text == "" {
			buf.Reset()
			curNum = nil
			curNotes = nil
			startPage = 0
			curEndPage = 0
			startOffset = 0
			endOffset = 0
			startPageText = ""
			return
		}
		pq := PreviewQuestion{Text: text, StartPage: startPage, EndPage: curEndPage}
		if startPageText != "" {
			// compute offsets as byte offsets inside the page text
			if idx := strings.Index(startPageText, strings.Split(text, "\n")[0]); idx >= 0 {
				so := idx
				pq.StartOffset = &so
			}
			// end offset: length of last page's trailing fragment inside that page
			// For simplicity, store length of text fragment on the last page.
			// We approximate by searching for the last line of the question in the last page text.
			for _, pg := range de.Pages {
				if pg.Number == curEndPage {
					lastLine := text
					if lines := strings.Split(text, "\n"); len(lines) > 0 {
						lastLine = strings.TrimSpace(lines[len(lines)-1])
					}
					if lastLine != "" {
						if idx := strings.LastIndex(pg.Text, lastLine); idx >= 0 {
							eo := idx + len(lastLine)
							pq.EndOffset = &eo
						}
					}
					break
				}
			}
		}
		_ = startOffset
		_ = endOffset
		lines := strings.Split(text, "\n")
		// option detection
		optionCount := 0
		for _, l := range lines {
			trim := strings.TrimSpace(l)
			if optionRe.MatchString(trim) {
				optionCount++
			}
		}
		// collect options
		if optionCount >= 2 {
			for _, l := range lines {
				t := strings.TrimSpace(l)
				if m := optionCollectRe.FindStringSubmatch(t); m != nil {
					body := strings.TrimSpace(m[2])
					if body != "" {
						pq.Options = append(pq.Options, body)
					} else {
						pq.Options = append(pq.Options, m[1])
					}
				}
			}
		}
		// type detection
		switch {
		case optionCount >= 2:
			if trueFalseHintRe.MatchString(text) {
				pq.Type = "true_false"
				curNotes = append(curNotes, "deterministic:type-true_false")
			} else if len(pq.Options) > 4 {
				pq.Type = "MSQ"
				curNotes = append(curNotes, "deterministic:type-msq")
			} else {
				pq.Type = "MCQ"
				curNotes = append(curNotes, "deterministic:type-mcq")
			}
		case numericalHintRe.MatchString(text) || isMostlyNumeric(text):
			pq.Type = "numerical"
			curNotes = append(curNotes, "deterministic:type-numerical")
		case len(text) > 200:
			pq.Type = "descriptive"
			curNotes = append(curNotes, "deterministic:type-descriptive-long")
		case descriptiveHintRe.MatchString(text) && len(text) > 30:
			pq.Type = "descriptive"
			curNotes = append(curNotes, "deterministic:type-descriptive-keyword")
		case strings.Contains(text, "?") && len(text) > 30:
			pq.Type = "descriptive"
			curNotes = append(curNotes, "deterministic:type-descriptive-question-mark")
		default:
			// check for subquestion density: if many subquestion markers, treat as descriptive with subparts
			subCount := 0
			for _, l := range lines {
				if subQuestionRe.MatchString(strings.TrimSpace(l)) {
					subCount++
				}
			}
			if subCount > 0 {
				pq.Type = "descriptive"
				curNotes = append(curNotes, "deterministic:type-descriptive-subquestions")
			} else {
				pq.Type = "unknown"
				curNotes = append(curNotes, "deterministic:type-unknown")
			}
		}
		// confidence: base 0.5, plus rule strengths
		pq.Confidence = 0.45
		if curNum != nil {
			pq.Number = curNum
			// strict match boosts confidence more
			if strictStartRe.MatchString(text) || len(*curNum) > 0 {
				pq.Confidence += 0.3
				curNotes = append(curNotes, "deterministic:matched-numbered")
			} else {
				pq.Confidence += 0.2
				curNotes = append(curNotes, "deterministic:matched-bare-number")
			}
		} else {
			curNotes = append(curNotes, "deterministic:unnumbered")
		}
		if len(pq.Options) > 0 {
			pq.Confidence += 0.12
			curNotes = append(curNotes, fmt.Sprintf("deterministic:options-%d", len(pq.Options)))
		}
		if optionCount >= 2 && pq.Type == "MCQ" {
			pq.Confidence += 0.03
		}
		// long descriptive with subquestions slightly higher
		if pq.Type == "descriptive" && len(text) > 120 {
			pq.Confidence += 0.05
		}
		if pq.Confidence > 0.97 {
			pq.Confidence = 0.97
		}
		// images: associate images from pages in range
		for _, pg := range de.Pages {
			if pg.Number >= pq.StartPage && pg.Number <= pq.EndPage {
				for _, img := range pg.Images {
					pq.Images = append(pq.Images, img.Name)
				}
			}
		}
		pq.ExtractionNotes = append([]string{}, curNotes...)
		out = append(out, pq)
		buf.Reset()
		curNum = nil
		curNotes = nil
		startPage = 0
		curEndPage = 0
		startOffset = 0
		endOffset = 0
		startPageText = ""
	}

	// Track whether we have seen any numbered question to decide heuristic fallback.
	seenNumbered := false

	for _, p := range de.Pages {
		lines := strings.Split(p.Text, "\n")
		for _, line := range lines {
			trim := strings.TrimSpace(line)
			if trim == "" {
				if buf.Len() > 0 {
					buf.WriteString("\n")
				}
				continue
			}
			// section headers: treat as separator, flush current if any, then skip
			if sectionHeaderRe.MatchString(trim) {
				if buf.Len() > 0 {
					// keep header as part of current question only if question already started and header is short
					// otherwise flush
					if len(trim) < 40 {
						buf.WriteString(trim)
						buf.WriteString("\n")
						curEndPage = p.Number
					} else {
						flush()
					}
				}
				continue
			}
			if m := questionStartRe.FindStringSubmatch(trim); m != nil {
				num := m[1]
				rest := strings.TrimSpace(m[2])
				// Validate that this is a plausible question start:
				// - number should be reasonable (1..500)
				// - rest can be empty (multi-line question) or substantial
				// Heuristic: if rest is very short and next line looks like continuation, still treat as start.
				// Reject if number is huge (e.g., year 2024)
				isYearLike := len(num) == 4 && num[0] == '2' && num[1] == '0'
				if isYearLike && rest == "" {
					// likely a year header, not a question
					if buf.Len() > 0 {
						buf.WriteString(trim)
						buf.WriteString("\n")
						curEndPage = p.Number
					}
					continue
				}
				// Check for subquestion false positive: if buf non-empty and this match is actually a subquestion like "1. (a)"?
				// Our regex captures leading digits, so "(a)" won't match, safe.
				// Merge heuristic: if previous question's text ends without terminal punctuation
				// and this start looks weak (bare number without Q prefix and short rest), consider continuation.
				if buf.Len() > 0 && shouldMergeWithPrevious(buf.String(), trim) {
					buf.WriteString(trim)
					buf.WriteString("\n")
					curEndPage = p.Number
					continue
				}
				if buf.Len() > 0 {
					flush()
				}
				numCopy := num
				curNum = &numCopy
				startPage = p.Number
				curEndPage = p.Number
				startPageText = p.Text
				seenNumbered = true
				curNotes = []string{fmt.Sprintf("deterministic:matched-%s", num)}
				if strictStartRe.MatchString(trim) {
					curNotes[0] = "deterministic:matched-strict"
				}
				if rest != "" {
					buf.WriteString(rest)
					buf.WriteString("\n")
				} else {
					// number line with no rest: ensure we still have a buffer so next lines attach
					// we already set curNum and pages; buf will collect next lines
				}
				continue
			}
			// Not a new question start: if we have an active question, append.
			if buf.Len() > 0 || curNum != nil {
				buf.WriteString(trim)
				buf.WriteString("\n")
				if startPage == 0 {
					startPage = p.Number
					startPageText = p.Text
				}
				curEndPage = p.Number
			} else {
				// No active question yet. Heuristic: if this line looks like substantial content
				// and we haven't seen any numbered questions, buffer it as unnumbered start.
				// Skip headers/titles that are not question-like.
				if !seenNumbered && len(trim) > 15 && !isLikelyTitle(trim) && isQuestionLike(trim) {
					// start unnumbered question
					startPage = p.Number
					curEndPage = p.Number
					startPageText = p.Text
					buf.WriteString(trim)
					buf.WriteString("\n")
					curNotes = []string{"deterministic:heuristic-unnumbered-start"}
				} else if seenNumbered {
					// after we have seen numbered questions, continuation without marker after a gap
					// should still be appended to last question if buffer was flushed? But flushed would be empty.
					// In that case, treat as tail continuation only if previous output exists and last question could be extended.
					// For now, if we have previous output, extend last question rather than creating new unnumbered.
					if len(out) > 0 && shouldMergeWithPrevious(out[len(out)-1].Text, trim) {
						// extend last question
						last := &out[len(out)-1]
						last.Text = strings.TrimSpace(last.Text + "\n" + trim)
						if p.Number > last.EndPage {
							last.EndPage = p.Number
						}
						last.ExtractionNotes = append(last.ExtractionNotes, "deterministic:merged-trailing-continuation")
					} else {
						// otherwise start new unnumbered buffer
						startPage = p.Number
						curEndPage = p.Number
						startPageText = p.Text
						buf.WriteString(trim)
						buf.WriteString("\n")
						curNotes = []string{"deterministic:heuristic-unnumbered-after-numbered"}
					}
				}
			}
		}
		// page boundary: if buffer ends without terminal punctuation, keep it open for next page
		// (already handled by not flushing). No extra action needed.
	}
	flush()

	// Heuristic fallback for documents with no numbered questions but substantial text
	if len(out) == 0 {
		totalChars := 0
		for _, pg := range de.Pages {
			totalChars += len(strings.TrimSpace(pg.Text))
		}
		if totalChars > 40 {
			// create heuristic chunks: split concatenated text on double newlines or treat each page as a question
			for _, pg := range de.Pages {
				txt := strings.TrimSpace(pg.Text)
				if txt == "" || isLikelyTitle(txt) || sectionHeaderRe.MatchString(txt) {
					continue
				}
				// split page text into heuristic blocks on blank lines
				blocks := splitHeuristicBlocks(txt)
				for _, block := range blocks {
					block = strings.TrimSpace(block)
					if block == "" || len(block) < 20 {
						continue
					}
					sp := pg.Number
					ep := pg.Number
					pq := PreviewQuestion{
						Text:            block,
						StartPage:       sp,
						EndPage:         ep,
						Type:            heuristicType(block),
						Confidence:      0.32,
						ExtractionNotes: []string{"deterministic:heuristic-unnumbered-fallback", "deterministic:type-" + heuristicType(block)},
					}
					// detect options even in fallback
					if opts := extractOptions(block); len(opts) >= 2 {
						pq.Options = opts
						pq.Type = "MCQ"
						pq.Confidence = 0.38
						pq.ExtractionNotes = []string{"deterministic:heuristic-mcq-fallback"}
					}
					// Associate this page's images even in heuristic fallback
					// so visual context is not dropped for unnumbered docs.
					for _, img := range pg.Images {
						pq.Images = append(pq.Images, img.Name)
					}
					out = append(out, pq)
				}
			}
			// if still empty but totalChars indicates content, create one per page as last resort
			if len(out) == 0 {
				for _, pg := range de.Pages {
					txt := strings.TrimSpace(pg.Text)
					if txt == "" {
						continue
					}
					pq := PreviewQuestion{
						Text:            txt,
						StartPage:       pg.Number,
						EndPage:         pg.Number,
						Type:            "unknown",
						Confidence:      0.25,
						ExtractionNotes: []string{"deterministic:fallback-single-page"},
					}
					for _, img := range pg.Images {
						pq.Images = append(pq.Images, img.Name)
					}
					out = append(out, pq)
				}
			}
		}
	}

	// Post-process: ensure multi-page merge for questions that clearly continue without terminal punctuation
	out = mergeContinuedQuestions(out, de)

	// Refine image association using Y-coordinate heuristic for pages with
	// multiple questions. This handles the "nearest question region" rule.
	out = refineImageAssociation(de, out)

	// Propagate vision descriptions into each question. This fills
	// ImageDetails from the page image refs and appends deterministic
	// visual-context notes (has_image / figure types / vision status).
	// It is nil-safe and never fails: when vision produced nothing the
	// questions keep legacy Images names plus an explicit fallback note.
	out = enrichQuestionsWithImageDetails(de, out)

	return out
}

func isLikelyTitle(s string) bool {
	trim := strings.TrimSpace(s)
	if strings.Contains(trim, "PYQ") || strings.Contains(trim, "MCQ Test") || strings.Contains(trim, "DESCRIPTIVE PYQ") || strings.Contains(trim, "MIXED PYQ") {
		return true
	}
	if len(trim) < 25 && !strings.Contains(trim, "?") && !strings.Contains(trim, ".") {
		// short header without punctuation is likely a title
		if strings.Count(trim, " ") <= 3 {
			return true
		}
	}
	// headers that look like document titles: contain test/year markers but no question words
	if len(trim) < 80 && !strings.Contains(trim, "?") {
		lower := strings.ToLower(trim)
		if strings.Contains(lower, "operating system") && !strings.Contains(lower, "what") && !strings.Contains(lower, "define") && !strings.Contains(lower, "explain") {
			// likely a title page
			if strings.Contains(trim, "–") || strings.Contains(trim, "-") || strings.Contains(trim, "202") {
				return true
			}
		}
	}
	// all caps short line
	if len(trim) < 50 && trim == strings.ToUpper(trim) && len(trim) > 5 {
		return true
	}
	return false
}

func isQuestionLike(s string) bool {
	trim := strings.TrimSpace(s)
	if strings.Contains(trim, "?") {
		return true
	}
	lower := strings.ToLower(trim)
	keywords := []string{"what is", "define", "explain", "describe", "discuss", "calculate", "which", "how", "why", "determine", "find", "deadlock", "process", "paging", "scheduling", "preemptive"}
	for _, k := range keywords {
		if strings.Contains(lower, k) {
			return true
		}
	}
	// long enough and sentence-like
	if len(trim) > 60 && strings.Contains(trim, " ") {
		return true
	}
	return false
}

func isMostlyNumeric(s string) bool {
	digits := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	return digits > 8 && float64(digits)/float64(len(s)) > 0.15
}

func heuristicType(block string) string {
	if numericalHintRe.MatchString(block) {
		return "numerical"
	}
	if trueFalseHintRe.MatchString(block) {
		return "true_false"
	}
	if len(block) > 180 {
		return "descriptive"
	}
	if descriptiveHintRe.MatchString(block) && len(block) > 30 {
		return "descriptive"
	}
	if strings.Contains(block, "?") && len(block) > 30 {
		return "descriptive"
	}
	if len(block) > 80 {
		return "descriptive"
	}
	return "unknown"
}

func extractOptions(block string) []string {
	var opts []string
	for _, l := range strings.Split(block, "\n") {
		t := strings.TrimSpace(l)
		if m := optionCollectRe.FindStringSubmatch(t); m != nil {
			body := strings.TrimSpace(m[2])
			if body != "" {
				opts = append(opts, body)
			}
		}
	}
	return opts
}

func splitHeuristicBlocks(txt string) []string {
	// split on double newline or chapter markers
	parts := strings.Split(txt, "\n\n")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// further split very long blocks on sentence boundaries if >500 chars
		if len(p) > 600 {
			sents := strings.Split(p, ". ")
			var cur strings.Builder
			for _, s := range sents {
				if cur.Len()+len(s) > 400 && cur.Len() > 0 {
					out = append(out, strings.TrimSpace(cur.String()))
					cur.Reset()
				}
				if cur.Len() > 0 {
					cur.WriteString(". ")
				}
				cur.WriteString(s)
			}
			if cur.Len() > 0 {
				out = append(out, strings.TrimSpace(cur.String()))
			}
		} else {
			out = append(out, p)
		}
	}
	return out
}

func shouldMergeWithPrevious(prevText, nextLine string) bool {
	prevTrim := strings.TrimSpace(prevText)
	if prevTrim == "" {
		return false
	}
	// if previous ends without terminal punctuation and next line does not look like strong question start
	lastChar := prevTrim[len(prevTrim)-1]
	if lastChar == '.' || lastChar == '?' || lastChar == '!' || lastChar == ':' {
		return false
	}
	// if next line is an option or subquestion, it is definitely continuation
	if optionRe.MatchString(nextLine) || subQuestionRe.MatchString(nextLine) {
		return true
	}
	// if previous is long and next is short continuation (lowercase start), merge
	if len(prevTrim) > 50 && len(nextLine) > 0 && nextLine[0] >= 'a' && nextLine[0] <= 'z' {
		return true
	}
	// if next line is weak numbered match (bare digit without Q) and previous has no terminal punctuation, treat as continuation
	if questionStartRe.MatchString(nextLine) && !strictStartRe.MatchString(nextLine) {
		// weak match: only merge if previous ends abruptly
		if lastChar != '.' && len(prevTrim) > 80 {
			return true
		}
	}
	return false
}

func mergeContinuedQuestions(in []PreviewQuestion, de DocumentExtraction) []PreviewQuestion {
	if len(in) < 2 {
		return in
	}
	var out []PreviewQuestion
	for i := 0; i < len(in); i++ {
		cur := in[i]
		// look ahead: if next question is on immediate next page and current ends without punctuation
		// and next is low confidence unnumbered, merge them
		if i+1 < len(in) {
			next := in[i+1]
			if next.StartPage == cur.EndPage || next.StartPage == cur.EndPage+1 {
				if cur.Confidence < 0.6 && next.Confidence < 0.5 && shouldMergeWithPrevious(cur.Text, next.Text) {
					merged := PreviewQuestion{
						Text:            strings.TrimSpace(cur.Text + "\n" + next.Text),
						StartPage:       cur.StartPage,
						EndPage:         next.EndPage,
						Type:            cur.Type,
						Options:         append(cur.Options, next.Options...),
						Confidence:      (cur.Confidence + next.Confidence) / 2,
						ExtractionNotes: append(append([]string{}, cur.ExtractionNotes...), "deterministic:merged-continuation"),
					}
					if merged.Type == "unknown" && next.Type != "unknown" {
						merged.Type = next.Type
					}
					if cur.Number != nil {
						merged.Number = cur.Number
					} else if next.Number != nil {
						merged.Number = next.Number
					}
					// Merge images as well
					merged.Images = append([]string{}, cur.Images...)
					for _, im := range next.Images {
						found := false
						for _, ex := range merged.Images {
							if ex == im {
								found = true
								break
							}
						}
						if !found {
							merged.Images = append(merged.Images, im)
						}
					}
					// Merge image details (vision-enriched refs) by name.
					merged.ImageDetails = append([]ImageRef{}, cur.ImageDetails...)
					seenDetail := make(map[string]bool, len(merged.ImageDetails))
					for _, d := range merged.ImageDetails {
						seenDetail[d.Name] = true
					}
					for _, d := range next.ImageDetails {
						if !seenDetail[d.Name] {
							seenDetail[d.Name] = true
							merged.ImageDetails = append(merged.ImageDetails, d)
						}
					}
					// Merge extraction notes from both sides (keep visual notes).
					merged.ExtractionNotes = append(merged.ExtractionNotes, next.ExtractionNotes...)
					out = append(out, merged)
					i++ // skip next
					continue
				}
			}
		}
		out = append(out, cur)
	}
	return out
}

// refineImageAssociation implements the Y-coordinate heuristic for pages
// that contain multiple questions. Images that were initially associated
// via page-range are reassigned to the nearest question on that page.
func refineImageAssociation(de DocumentExtraction, qs []PreviewQuestion) []PreviewQuestion {
	// Build map page -> images with positions
	pageImages := make(map[int][]ImageRef)
	for _, pg := range de.Pages {
		if len(pg.Images) > 0 {
			pageImages[pg.Number] = pg.Images
		}
	}
	if len(pageImages) == 0 || len(qs) <= 1 {
		return qs
	}
	// For each page with images, check contention
	for pageNum, images := range pageImages {
		var indices []int
		for i, q := range qs {
			if q.StartPage <= pageNum && pageNum <= q.EndPage {
				indices = append(indices, i)
			}
		}
		if len(indices) <= 1 {
			continue
		}
		// Estimate Y for each question on this page
		qY := make(map[int]float64)
		// Gather questions starting on this page in order
		var startOnPage []int
		for _, idx := range indices {
			if qs[idx].StartPage == pageNum {
				startOnPage = append(startOnPage, idx)
			}
		}
		for order, idx := range startOnPage {
			// Use order-based Y to ensure vertical separation; line-based
			// estimate is unreliable when an image occupies vertical space.
			y := 700.0 - float64(order)*160.0
			// Optionally tighten with line estimate but keep monotonic order:
			// we average order Y with line estimate if available.
			if est := estimateQuestionY(de, pageNum, qs[idx]); est != nil {
				// Blend: prefer order Y for spacing, but nudge toward estimate
				// without breaking order.
				// Use weighted average favouring order
				y = 0.7*y + 0.3*(*est)
			}
			qY[idx] = y
		}
		for _, idx := range indices {
			if _, ok := qY[idx]; !ok {
				// Continuing question: place near middle
				qY[idx] = 400.0
			}
		}
		// Remove this page's images from all contending questions
		imageSet := make(map[string]bool)
		for _, im := range images {
			imageSet[im.Name] = true
		}
		for _, idx := range indices {
			filtered := make([]string, 0, len(qs[idx].Images))
			for _, name := range qs[idx].Images {
				if !imageSet[name] {
					filtered = append(filtered, name)
				}
			}
			qs[idx].Images = filtered
			// Keep any pre-existing ImageDetails in sync with the filtered names.
			if len(qs[idx].ImageDetails) > 0 {
				kept := make([]ImageRef, 0, len(qs[idx].ImageDetails))
				for _, d := range qs[idx].ImageDetails {
					if !imageSet[d.Name] {
						kept = append(kept, d)
					}
				}
				qs[idx].ImageDetails = kept
			}
		}
		// Reassign each image to the nearest question above it (Y heuristic).
		// For image Y, the owning question is the one whose start Y is just
		// above the image (smallest positive qY - imgY). If no question is
		// above (image at top), assign to the topmost question.
		for _, img := range images {
			if img.Y == 0 && img.X == 0 {
				qs[indices[0]].Images = append(qs[indices[0]].Images, img.Name)
				qs[indices[0]].ImageDetails = appendImageDetail(qs[indices[0]].ImageDetails, img)
				continue
			}
			nearest := -1
			bestAbove := 1e9
			for _, idx := range indices {
				if qY[idx] >= img.Y {
					dist := qY[idx] - img.Y
					if dist < bestAbove {
						bestAbove = dist
						nearest = idx
					}
				}
			}
			if nearest == -1 {
				// Image is above all questions or Y estimate off: pick
				// the question with highest Y (topmost) if image is at top,
				// otherwise nearest absolute distance.
				// Find topmost
				topIdx := indices[0]
				topY := qY[topIdx]
				for _, idx := range indices[1:] {
					if qY[idx] > topY {
						topY = qY[idx]
						topIdx = idx
					}
				}
				if img.Y > topY {
					nearest = topIdx
				} else {
					// fallback to absolute nearest
					nearest = indices[0]
					bestAbs := absFloat(img.Y - qY[nearest])
					for _, idx := range indices[1:] {
						if d := absFloat(img.Y - qY[idx]); d < bestAbs {
							bestAbs = d
							nearest = idx
						}
					}
				}
			}
			qs[nearest].Images = append(qs[nearest].Images, img.Name)
			qs[nearest].ImageDetails = appendImageDetail(qs[nearest].ImageDetails, img)
			qs[nearest].ExtractionNotes = append(qs[nearest].ExtractionNotes, fmt.Sprintf("image-associated:%s@page%d:y%.0f", img.Name, pageNum, img.Y))
		}
	}
	return qs
}

func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// appendImageDetail appends img to details unless an entry with the same name
// already exists (in which case the richer entry wins: a described ref
// replaces an undescribed one). It keeps association deterministic.
func appendImageDetail(details []ImageRef, img ImageRef) []ImageRef {
	for i, d := range details {
		if d.Name == img.Name {
			if strings.TrimSpace(d.Description) == "" && strings.TrimSpace(img.Description) != "" {
				details[i] = img
			}
			return details
		}
	}
	return append(details, img)
}

// enrichQuestionsWithImageDetails propagates vision descriptions from the
// document pages into each preview question. For every associated image name
// it fills ImageDetails with the full ImageRef (description + figure_type +
// described_by when the vision pipeline produced them) and appends
// deterministic visual-context notes:
//
//	visual:has_image            — question has at least one image
//	visual:figure-<type>        — one note per distinct figure type present
//	visual:described:<name>     — image has a non-empty vision description
//	visual:undescribed:<name>   — deterministic fallback when vision produced
//	                              nothing for this image (no key / failure)
//	vision:described            — at least one image described
//	vision:unavailable          — images present but none described (fallback)
//
// Questions without images are untouched. The function never fails: a nil or
// image-free extraction is returned unchanged.
func enrichQuestionsWithImageDetails(de DocumentExtraction, qs []PreviewQuestion) []PreviewQuestion {
	if len(qs) == 0 {
		return qs
	}
	byName := make(map[string]ImageRef)
	for _, pg := range de.Pages {
		for _, img := range pg.Images {
			if _, ok := byName[img.Name]; !ok {
				byName[img.Name] = img
			} else {
				// Prefer the described copy when duplicates share a name.
				existing := byName[img.Name]
				if strings.TrimSpace(existing.Description) == "" && strings.TrimSpace(img.Description) != "" {
					byName[img.Name] = img
				}
			}
		}
	}
	if len(byName) == 0 {
		return qs
	}
	for i := range qs {
		if len(qs[i].Images) == 0 {
			continue
		}
		// Rebuild details deterministically from the final Images order so
		// refine/merge reassignment never leaves stale entries behind.
		details := make([]ImageRef, 0, len(qs[i].Images))
		for _, name := range qs[i].Images {
			if ref, ok := byName[name]; ok {
				details = appendImageDetail(details, ref)
			} else {
				details = appendImageDetail(details, ImageRef{Name: name})
			}
		}
		qs[i].ImageDetails = details
		qs[i].ExtractionNotes = appendVisualNotes(qs[i].ExtractionNotes, details)
	}
	return qs
}

// appendVisualNotes returns notes with deterministic visual-context entries
// for details appended, skipping notes already present (idempotent).
func appendVisualNotes(notes []string, details []ImageRef) []string {
	if len(details) == 0 {
		return notes
	}
	have := make(map[string]bool, len(notes))
	for _, n := range notes {
		have[n] = true
	}
	add := func(n string) {
		if !have[n] {
			have[n] = true
			notes = append(notes, n)
		}
	}
	add("visual:has_image")
	// Distinct figure types in sorted order for determinism.
	types := make(map[string]bool)
	for _, d := range details {
		if strings.TrimSpace(d.FigureType) == "" {
			continue
		}
		if ft := NormalizeFigureType(d.FigureType); ft != "" && ft != FigureTypeUnknown {
			types[ft] = true
		}
		// Explicit "unknown" classifications emit no figure note (only the
		// vision:described/unavailable status notes below).
	}
	ordered := make([]string, 0, len(types))
	for t := range types {
		ordered = append(ordered, t)
	}
	// Simple deterministic sort without importing sort here is unnecessary;
	// reuse insertion via sorted compare using strings package order.
	for a := 0; a < len(ordered); a++ {
		for b := a + 1; b < len(ordered); b++ {
			if ordered[b] < ordered[a] {
				ordered[a], ordered[b] = ordered[b], ordered[a]
			}
		}
	}
	for _, t := range ordered {
		add("visual:figure-" + t)
	}
	described := 0
	for _, d := range details {
		if strings.TrimSpace(d.Description) != "" {
			described++
			add("visual:described:" + d.Name)
		} else {
			add("visual:undescribed:" + d.Name)
		}
	}
	if described > 0 {
		add("vision:described")
	} else {
		// Deterministic fallback: images exist but vision produced nothing.
		add("vision:unavailable")
	}
	return notes
}

// buildImagesJSONPayload renders the ImagesJSON blob for one preview question.
// When any associated image carries vision context (description, figure type,
// or described-by) it returns the vision-enriched object array so quiz
// generation can preserve visual content; otherwise it returns the legacy
// string array (deterministic fallback, backwards compatible). It never
// returns an error; empty input yields "" (caller skips persistence).
func buildImagesJSONPayload(pq PreviewQuestion) string {
	if len(pq.Images) == 0 && len(pq.ImageDetails) == 0 {
		return ""
	}
	enriched := false
	for _, d := range pq.ImageDetails {
		if strings.TrimSpace(d.Description) != "" || strings.TrimSpace(d.FigureType) != "" || strings.TrimSpace(d.DescribedBy) != "" {
			enriched = true
			break
		}
	}
	if enriched {
		type imageJSON struct {
			Name          string `json:"name"`
			StoragePath   string `json:"storage_path,omitempty"`
			ThumbnailPath string `json:"thumbnail_path,omitempty"`
			Description   string `json:"description,omitempty"`
			FigureType    string `json:"figure_type,omitempty"`
			DescribedBy   string `json:"described_by,omitempty"`
		}
		// Preserve the deterministic Images order; fall back to a name-only
		// entry when a name has no matching detail.
		byName := make(map[string]ImageRef, len(pq.ImageDetails))
		for _, d := range pq.ImageDetails {
			if _, ok := byName[d.Name]; !ok {
				byName[d.Name] = d
			}
		}
		names := append([]string{}, pq.Images...)
		if len(names) == 0 {
			for _, d := range pq.ImageDetails {
				names = append(names, d.Name)
			}
		}
		items := make([]imageJSON, 0, len(names))
		for _, n := range names {
			if d, ok := byName[n]; ok {
				ft := ""
				if strings.TrimSpace(d.FigureType) != "" {
					if norm := NormalizeFigureType(d.FigureType); norm != FigureTypeUnknown {
						ft = norm
					} else if strings.TrimSpace(d.FigureType) != "" {
						// Preserve explicit "unknown" only when the model
						// actually classified it as such with a description.
						if strings.TrimSpace(d.Description) != "" {
							ft = FigureTypeUnknown
						}
					}
				}
				items = append(items, imageJSON{
					Name:          d.Name,
					StoragePath:   d.StoragePath,
					ThumbnailPath: d.ThumbnailPath,
					Description:   strings.TrimSpace(d.Description),
					FigureType:    ft,
					DescribedBy:   d.DescribedBy,
				})
			} else {
				items = append(items, imageJSON{Name: n})
			}
		}
		if b, err := json.Marshal(items); err == nil {
			return string(b)
		}
	}
	// Legacy fallback: plain name array.
	if b, err := json.Marshal(pq.Images); err == nil {
		return string(b)
	}
	return ""
}

func estimateQuestionY(de DocumentExtraction, pageNum int, q PreviewQuestion) *float64 {
	var pageText string
	for _, pg := range de.Pages {
		if pg.Number == pageNum {
			pageText = pg.Text
			break
		}
	}
	if pageText == "" {
		return nil
	}
	lines := strings.Split(pageText, "\n")
	// Use first non-empty line of question text as anchor
	qLines := strings.Split(q.Text, "\n")
	var anchor string
	for _, l := range qLines {
		trim := strings.TrimSpace(l)
		if trim != "" {
			anchor = trim
			if len(anchor) > 40 {
				anchor = anchor[:40]
			}
			break
		}
	}
	if anchor == "" {
		return nil
	}
	for idx, line := range lines {
		if strings.Contains(line, anchor) || strings.Contains(anchor, strings.TrimSpace(line)) {
			y := 720.0 - float64(idx)*14.0
			return &y
		}
	}
	// Fallback: search for question number
	if q.Number != nil {
		num := *q.Number
		for idx, line := range lines {
			if strings.Contains(line, num) {
				y := 720.0 - float64(idx)*14.0
				return &y
			}
		}
	}
	return nil
}

// ValidateQuestion checks a preview question for required fields and sane page ranges.
func ValidateQuestion(q PreviewQuestion) error {
	if strings.TrimSpace(q.Text) == "" {
		return fmt.Errorf("question text is empty")
	}
	if q.StartPage < 1 {
		return fmt.Errorf("start_page must be >=1, got %d", q.StartPage)
	}
	if q.EndPage < q.StartPage {
		return fmt.Errorf("end_page %d < start_page %d", q.EndPage, q.StartPage)
	}
	if q.Confidence < 0 || q.Confidence > 1 {
		return fmt.Errorf("confidence %v out of range [0,1]", q.Confidence)
	}
	if q.Type != "" {
		switch q.Type {
		case "MCQ", "MSQ", "numerical", "descriptive", "true_false", "unknown":
		default:
			return fmt.Errorf("unknown question type %q", q.Type)
		}
	}
	return nil
}

// ExtractQuestions parses a DocumentExtraction into question records and
// persists them via the questions repository. It writes per-question debug
// JSON under the extraction debug dir for replay and QA.
func (s *ExtractionService) ExtractQuestions(ctx context.Context, de DocumentExtraction) error {
	if s.questionRepo == nil {
		return fmt.Errorf("extraction: no questions repository configured")
	}
	base := s.stepBase(ctx, de.DocumentID)
	finish := s.steps().Start(ctx, "question_extraction", base)
	qs := parseQuestionsFromExtraction(de)
	// Validate all before persisting to avoid partial writes
	for i, pq := range qs {
		if err := ValidateQuestion(pq); err != nil {
			verr := fmt.Errorf("extraction: validate question %d: %w", i+1, err)
			finish(verr, map[string]any{"questions": len(qs)})
			return verr
		}
	}
	extractDir := filepath.Join(s.root, documentLayout, de.DocumentID, extractionDirName)
	_ = os.MkdirAll(extractDir, 0o755)
	debugDir := filepath.Join(extractDir, debugDirName)
	_ = os.MkdirAll(debugDir, 0o755)

	for i, pq := range qs {
		qID, _ := documents.NewID()
		q := questions.Question{
			ID:           qID,
			DocumentID:   de.DocumentID,
			QuestionText: &pq.Text,
		}
		if pq.Number != nil {
			n := *pq.Number
			q.QuestionNumber = &n
		}
		sp := pq.StartPage
		ep := pq.EndPage
		q.StartPage = &sp
		q.EndPage = &ep
		q.PageNumber = &sp
		if pq.StartOffset != nil {
			so := *pq.StartOffset
			q.StartOffset = &so
		}
		if pq.EndOffset != nil {
			eo := *pq.EndOffset
			q.EndOffset = &eo
		}
		c := pq.Confidence
		q.Confidence = &c
		// new fields
		if pq.Type != "" {
			t := pq.Type
			q.QuestionType = &t
		}
		if len(pq.Options) > 0 {
			// store options as JSON array string
			if b, err := json.Marshal(pq.Options); err == nil {
				s := string(b)
				q.OptionsJSON = &s
			}
		}
		if len(pq.ExtractionNotes) > 0 {
			if b, err := json.Marshal(pq.ExtractionNotes); err == nil {
				s := string(b)
				q.ExtractionNotesJSON = &s
			}
		}
		// images JSON if any: vision-enriched object array when descriptions
		// exist (enough context for image-aware quiz generation), legacy
		// name array as deterministic fallback. ExtractionNotesJSON already
		// carries visual-context notes (visual:has_image / visual:figure-* /
		// vision:described|unavailable) via enrichQuestionsWithImageDetails.
		if len(pq.Images) > 0 || len(pq.ImageDetails) > 0 {
			if payload := buildImagesJSONPayload(pq); payload != "" {
				s := payload
				q.ImagesJSON = &s
			}
		}
		if err := s.questionRepo.Insert(ctx, q); err != nil {
			ierr := fmt.Errorf("extraction: insert question: %w", err)
			finish(ierr, map[string]any{"questions": len(qs)})
			return ierr
		}
		b, _ := json.MarshalIndent(pq, "", "  ")
		_ = os.WriteFile(filepath.Join(debugDir, fmt.Sprintf("question-%03d.json", i+1)), b, 0o644)
	}
	summaryPath := filepath.Join(extractDir, "questions.json")
	if data, err := json.MarshalIndent(qs, "", "  "); err == nil {
		_ = os.WriteFile(summaryPath, data, 0o644)
		// also write a metrics file for QA
		metrics := buildExtractionMetrics(de, qs)
		if mdata, err := json.MarshalIndent(metrics, "", "  "); err == nil {
			_ = os.WriteFile(filepath.Join(extractDir, "extraction_metrics.json"), mdata, 0o644)
		}
		// write to top-level debug dir as well if configured
		if s.debugWriter != nil {
			_ = s.debugWriter.WriteQuestions(de.DocumentID, qs)
		}
	}
	// store raw artifacts hash for reproducibility
	if s.llmFallback != nil && len(qs) > 0 {
		// log prompt hash if LLM was used (no-op if not)
	}
	finish(nil, map[string]any{"questions": len(qs)})
	return nil
}

// buildExtractionMetrics computes triage metrics for QA dashboards.
func buildExtractionMetrics(de DocumentExtraction, qs []PreviewQuestion) map[string]interface{} {
	totalPages := de.PageCount
	qPerPage := 0.0
	if totalPages > 0 {
		qPerPage = float64(len(qs)) / float64(totalPages)
	}
	avgConf := 0.0
	ambiguous := 0
	llmFallbackRate := 0.0 // caller can fill if LLM used
	for _, q := range qs {
		avgConf += q.Confidence
		if q.Confidence < 0.6 {
			ambiguous++
		}
		for _, n := range q.ExtractionNotes {
			if strings.Contains(n, "llm") {
				llmFallbackRate += 1
			}
		}
	}
	if len(qs) > 0 {
		avgConf /= float64(len(qs))
		llmFallbackRate /= float64(len(qs))
	}
	return map[string]interface{}{
		"document_id":                 de.DocumentID,
		"page_count":                  totalPages,
		"question_count":              len(qs),
		"questions_per_page":          qPerPage,
		"average_confidence":          avgConf,
		"pages_with_ambiguous":        ambiguous,
		"llm_fallback_rate":           llmFallbackRate,
		"pages":                       len(de.Pages),
	}
}

// ExtractPreview parses the PDF and returns preview questions without
// persisting them. pages can be nil/empty to parse the whole document.
func (s *ExtractionService) ExtractPreview(ctx context.Context, documentID, storagePath string, pages []int) ([]PreviewQuestion, error) {
	pdfPath, err := s.resolvePDF(documentID, storagePath)
	if err != nil {
		return nil, err
	}
	de, err := s.extractor.ExtractFile(ctx, documentID, pdfPath)
	if err != nil {
		return nil, err
	}
	// if pages requested, keep only those pages
	if len(pages) > 0 {
		keep := make([]Page, 0, len(pages))
		pageSet := map[int]struct{}{}
		for _, p := range pages {
			pageSet[p] = struct{}{}
		}
		for _, p := range de.Pages {
			if _, ok := pageSet[p.Number]; ok {
				keep = append(keep, p)
			}
		}
		de.Pages = keep
	}
	parsed := parseQuestionsFromExtraction(de)
	maxConf := 0.0
	for _, p := range parsed {
		if p.Confidence > maxConf {
			maxConf = p.Confidence
		}
	}
	if (len(parsed) == 0 || maxConf < 0.6) && s.llmFallback != nil {
		pg := de.Pages
		if len(pg) > s.llmFallback.MaxPages && s.llmFallback.MaxPages > 0 {
			pg = pg[:s.llmFallback.MaxPages]
		}
		// rate limiting check
		if err := s.llmFallback.Wait(ctx); err != nil {
			return parsed, nil
		}
		if llmOut, err := s.llmFallback.Call(ctx, documentID, pg); err == nil {
			// validate LLM output
			valid := make([]PreviewQuestion, 0, len(llmOut))
			for _, q := range llmOut {
				if err := ValidateQuestion(q); err == nil {
					valid = append(valid, q)
				}
			}
			if len(valid) > 0 {
				// record metrics: annotate with llm note and hash
				hash := s.llmFallback.LastPromptHash()
				for i := range valid {
					valid[i].ExtractionNotes = append(valid[i].ExtractionNotes, "llm:fallback", "prompt-hash:"+hash)
					if valid[i].Confidence == 0 {
						valid[i].Confidence = 0.72
					}
				}
				return valid, nil
			}
		}
	}
	return parsed, nil
}

// ExtractFromPages is a convenience wrapper that extracts questions from a subset of pages
// without persisting them. It mirrors ExtractPreview but uses the extractor directly.
func (s *ExtractionService) ExtractFromPages(ctx context.Context, documentID, storagePath string, pages []int) ([]PreviewQuestion, error) {
	return s.ExtractPreview(ctx, documentID, storagePath, pages)
}

// hash helper
func promptHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:8])
}

