package extraction

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// DebugSummaryPage is the per-page entry of a debug summary.
type DebugSummaryPage struct {
	Number   int    `json:"number"`
	Chars    int    `json:"chars"`
	NeedsOCR bool   `json:"needs_ocr"`
	Error    string `json:"error,omitempty"`
}

// Summary describes one completed extraction: the page count plus per-page
// character counts and needs-OCR flags, so extraction quality can be judged
// without opening each page file.
type DebugSummary struct {
	DocumentID string             `json:"document_id"`
	PageCount  int                `json:"page_count"`
	Pages      []DebugSummaryPage `json:"pages"`
}

// DebugWriter persists extraction results under <root>/<document-id>/ as
// page-NNN.txt files (one per page, zero-padded to three digits) plus a
// summary.json, making extraction problems easy to diagnose offline.
type DebugWriter struct {
	root string
}

// NewDebugWriter resolves root (e.g. ./data/debug) to an absolute path and
// returns a debug writer rooted there.
func NewDebugWriter(root string) (*DebugWriter, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("extraction: resolve debug root: %w", err)
	}
	return &DebugWriter{root: abs}, nil
}

// Write stores page-NNN.txt for every page of the extraction plus a
// summary.json next to them. Any previous debug output for the document is
// replaced so stale page files cannot contradict the fresh summary.
func (w *DebugWriter) Write(e DocumentExtraction) error {
	if !safeSegment(e.DocumentID) {
		return fmt.Errorf("extraction: unsafe document id %q", e.DocumentID)
	}
	dir := filepath.Join(w.root, e.DocumentID)
	if !withinRoot(w.root, dir) {
		return fmt.Errorf("extraction: resolved path escapes root: %q", dir)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("extraction: clear debug dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("extraction: create debug dir: %w", err)
	}

	for _, page := range e.Pages {
		name := filepath.Join(dir, fmt.Sprintf("page-%03d.txt", page.Number))
		if err := os.WriteFile(name, []byte(page.Text), 0o644); err != nil {
			return fmt.Errorf("extraction: write debug page %d: %w", page.Number, err)
		}
	}

	data, err := json.MarshalIndent(buildSummary(e), "", "  ")
	if err != nil {
		return fmt.Errorf("extraction: encode debug summary: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), data, 0o644); err != nil {
		return fmt.Errorf("extraction: write debug summary: %w", err)
	}
	return nil
}

func buildSummary(e DocumentExtraction) DebugSummary {
	summary := DebugSummary{
		DocumentID: e.DocumentID,
		PageCount:  e.PageCount,
		Pages:      make([]DebugSummaryPage, 0, len(e.Pages)),
	}
	for _, page := range e.Pages {
		summary.Pages = append(summary.Pages, DebugSummaryPage{
			Number:   page.Number,
			Chars:    utf8.RuneCountInString(page.Text),
			NeedsOCR: page.NeedsOCR,
			Error:    page.Error,
		})
	}
	return summary
}

// WriteQuestions persists the extracted questions for a document as
// <root>/<documentID>/questions.json plus per-question question-NNN.json files.
// It is used to keep raw extraction artifacts for replay and QA. Unlike Write,
// it does not clear the directory so page dumps and question dumps coexist.
func (w *DebugWriter) WriteQuestions(documentID string, qs []PreviewQuestion) error {
	if !safeSegment(documentID) {
		return fmt.Errorf("extraction: unsafe document id %q", documentID)
	}
	dir := filepath.Join(w.root, documentID)
	if !withinRoot(w.root, dir) {
		return fmt.Errorf("extraction: resolved path escapes root: %q", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("extraction: create debug dir: %w", err)
	}
	data, err := json.MarshalIndent(qs, "", "  ")
	if err != nil {
		return fmt.Errorf("extraction: encode questions: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "questions.json"), append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("extraction: write questions.json: %w", err)
	}
	for i, q := range qs {
		b, _ := json.MarshalIndent(q, "", "  ")
		name := filepath.Join(dir, fmt.Sprintf("question-%03d.json", i+1))
		_ = os.WriteFile(name, b, 0o644)
	}
	// also store a metrics snapshot for triage
	metrics := map[string]interface{}{
		"document_id":    documentID,
		"question_count": len(qs),
	}
	if mdata, err := json.MarshalIndent(metrics, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "extraction_metrics.json"), append(mdata, '\n'), 0o644)
	}
	return nil
}
