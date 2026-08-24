package extraction

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDebugWriterWritesPagesAndSummary(t *testing.T) {
	root := t.TempDir()
	w, err := NewDebugWriter(root)
	if err != nil {
		t.Fatalf("NewDebugWriter: %v", err)
	}

	in := DocumentExtraction{
		DocumentID: "doc-debug",
		PageCount:  3,
		Pages: []Page{
			{Number: 1, Text: "Operating Systems PYQs"},
			{Number: 2, Text: "Q1. What is a process?"},
			{Number: 3, Text: "", NeedsOCR: true},
		},
	}
	if err := w.Write(in); err != nil {
		t.Fatalf("Write: %v", err)
	}

	dir := filepath.Join(root, "doc-debug")
	for _, tc := range []struct {
		name     string
		contains string
	}{
		{"page-001.txt", "Operating Systems PYQs"},
		{"page-002.txt", "Q1. What is a process?"},
		{"page-003.txt", ""},
	} {
		data, err := os.ReadFile(filepath.Join(dir, tc.name))
		if err != nil {
			t.Fatalf("read %s: %v", tc.name, err)
		}
		if !strings.Contains(string(data), tc.contains) {
			t.Errorf("%s = %q, want to contain %q", tc.name, string(data), tc.contains)
		}
	}

	data, err := os.ReadFile(filepath.Join(dir, "summary.json"))
	if err != nil {
		t.Fatalf("read summary.json: %v", err)
	}
	var got DebugSummary
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode summary.json: %v", err)
	}
	if got.DocumentID != "doc-debug" {
		t.Errorf("DocumentID = %q, want %q", got.DocumentID, "doc-debug")
	}
	if got.PageCount != 3 {
		t.Errorf("PageCount = %d, want 3", got.PageCount)
	}
	wantPages := []DebugSummaryPage{
		{Number: 1, Chars: 22},
		{Number: 2, Chars: 22},
		{Number: 3, Chars: 0, NeedsOCR: true},
	}
	if len(got.Pages) != len(wantPages) {
		t.Fatalf("len(Pages) = %d, want %d", len(got.Pages), len(wantPages))
	}
	for i, want := range wantPages {
		if got.Pages[i] != want {
			t.Errorf("Pages[%d] = %+v, want %+v", i, got.Pages[i], want)
		}
	}
}

func TestDebugWriterReplacesStaleOutput(t *testing.T) {
	root := t.TempDir()
	w, err := NewDebugWriter(root)
	if err != nil {
		t.Fatalf("NewDebugWriter: %v", err)
	}

	first := DocumentExtraction{
		DocumentID: "doc-shrink",
		PageCount:  2,
		Pages: []Page{
			{Number: 1, Text: "first"},
			{Number: 2, Text: "second"},
		},
	}
	if err := w.Write(first); err != nil {
		t.Fatalf("Write first: %v", err)
	}
	second := DocumentExtraction{
		DocumentID: "doc-shrink",
		PageCount:  1,
		Pages:      []Page{{Number: 1, Text: "only page"}},
	}
	if err := w.Write(second); err != nil {
		t.Fatalf("Write second: %v", err)
	}

	dir := filepath.Join(root, "doc-shrink")
	if _, err := os.Stat(filepath.Join(dir, "page-002.txt")); !os.IsNotExist(err) {
		t.Errorf("stale page-002.txt still exists (stat error: %v)", err)
	}
	var got DebugSummary
	data, err := os.ReadFile(filepath.Join(dir, "summary.json"))
	if err != nil {
		t.Fatalf("read summary.json: %v", err)
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode summary.json: %v", err)
	}
	if got.PageCount != 1 || len(got.Pages) != 1 {
		t.Errorf("summary = %+v, want a single page", got)
	}
}

func TestDebugWriterRejectsUnsafeIDs(t *testing.T) {
	w, err := NewDebugWriter(t.TempDir())
	if err != nil {
		t.Fatalf("NewDebugWriter: %v", err)
	}
	for _, id := range []string{"", "../escape", "a/b", `a\b`, ".."} {
		e := DocumentExtraction{DocumentID: id}
		if err := w.Write(e); err == nil {
			t.Errorf("Write(%q) succeeded, want unsafe id error", id)
		}
	}
}

func TestExtractWritesDebugOutput(t *testing.T) {
	root := t.TempDir()
	storeTestPDF(t, root, "doc-abc123", buildTestPDF(t, []string{
		textStream("First page has enough text"),
		"", // blank page: needs OCR
	}))

	debugRoot := filepath.Join(root, "debug")
	w, err := NewDebugWriter(debugRoot)
	if err != nil {
		t.Fatalf("NewDebugWriter: %v", err)
	}
	svc, err := NewService(root)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	svc.WithDebugWriter(w)

	got, err := svc.Extract(context.Background(), "doc-abc123")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	dir := filepath.Join(debugRoot, "doc-abc123")
	pageText, err := os.ReadFile(filepath.Join(dir, "page-001.txt"))
	if err != nil {
		t.Fatalf("read page-001.txt: %v", err)
	}
	if !strings.Contains(string(pageText), "First page has enough text") {
		t.Errorf("page-001.txt = %q, want extracted page text", string(pageText))
	}
	if _, err := os.Stat(filepath.Join(dir, "page-002.txt")); err != nil {
		t.Errorf("stat page-002.txt: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "summary.json"))
	if err != nil {
		t.Fatalf("read summary.json: %v", err)
	}
	var summary DebugSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatalf("decode summary.json: %v", err)
	}
	if summary.DocumentID != got.DocumentID || summary.PageCount != got.PageCount {
		t.Errorf("summary header = %+v, want document %q with %d pages",
			summary, got.DocumentID, got.PageCount)
	}
	if len(summary.Pages) != 2 {
		t.Fatalf("len(summary.Pages) = %d, want 2", len(summary.Pages))
	}
	if summary.Pages[0].NeedsOCR {
		t.Errorf("Pages[0].NeedsOCR = true, want false for text-bearing page")
	}
	if !summary.Pages[1].NeedsOCR {
		t.Errorf("Pages[1].NeedsOCR = false, want true for blank page")
	}
}

func TestExtractSurvivesDebugWriteFailure(t *testing.T) {
	root := t.TempDir()
	storeTestPDF(t, root, "doc-ok", buildTestPDF(t, []string{
		textStream("Extraction still works"),
	}))

	// A regular file where the debug root should be makes every write fail.
	blocked := filepath.Join(root, "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("create blocker file: %v", err)
	}
	w, err := NewDebugWriter(blocked)
	if err != nil {
		t.Fatalf("NewDebugWriter: %v", err)
	}

	svc, err := NewService(root)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	svc.WithDebugWriter(w)

	got, err := svc.Extract(context.Background(), "doc-ok")
	if err != nil {
		t.Fatalf("Extract with failing debug writer: %v", err)
	}
	if len(got.Pages) != 1 || got.Pages[0].Error != "" {
		t.Errorf("extraction result degraded by debug failure: %+v", got.Pages)
	}
}
