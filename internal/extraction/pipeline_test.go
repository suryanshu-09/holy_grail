package extraction

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
	"github.com/suryanshu-09/holy_grail/internal/documents"
	"github.com/suryanshu-09/holy_grail/internal/questions"
)

// fakeRepo records status transitions so tests can assert on them.
type fakeRepo struct {
	docs     map[string]documents.Document
	statuses map[string]string
}

func newFakeRepo(ids ...string) *fakeRepo {
	r := &fakeRepo{docs: make(map[string]documents.Document), statuses: make(map[string]string)}
	for _, id := range ids {
		path := "documents/" + id + "/original.pdf"
		r.docs[id] = documents.Document{ID: id, Status: documents.StatusUploaded, StoragePath: &path}
	}
	return r
}

func (r *fakeRepo) List(_ context.Context, _ documents.Filter) ([]documents.Document, error) {
	out := make([]documents.Document, 0, len(r.docs))
	for _, d := range r.docs {
		out = append(out, d)
	}
	return out, nil
}

func (r *fakeRepo) GetByID(_ context.Context, id string) (documents.Document, error) {
	if d, ok := r.docs[id]; ok {
		return d, nil
	}
	return documents.Document{}, apperr.ErrNotFound
}

func (r *fakeRepo) Create(_ context.Context, d *documents.Document) error {
	r.docs[d.ID] = *d
	return nil
}

func (r *fakeRepo) UpdateStatus(_ context.Context, id string, status string) error {
	if _, ok := r.docs[id]; !ok {
		return apperr.ErrNotFound
	}
	r.statuses[id] = status
	return nil
}

// fakeQuestionsRepo is a minimal in-memory implementation of questions.Repository
// used by extraction pipeline tests to assert that questions are persisted.
type fakeQuestionsRepo struct {
	inserted []questions.Question
}

func (f *fakeQuestionsRepo) List(_ context.Context, _ questions.Filter) ([]questions.Question, error) {
	return nil, nil
}
func (f *fakeQuestionsRepo) GetByID(_ context.Context, _ string) (questions.Question, error) {
	return questions.Question{}, apperr.ErrNotFound
}
func (f *fakeQuestionsRepo) Insert(_ context.Context, q questions.Question) error {
	f.inserted = append(f.inserted, q)
	return nil
}

func newTestPipeline(t *testing.T) (*ExtractionService, *fakeRepo) {
	t.Helper()
	root := t.TempDir()
	extractor, err := NewService(root)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	repo := newFakeRepo()
	qrepo := &fakeQuestionsRepo{}
	svc, err := NewExtractionService(root, extractor, repo, qrepo)
	if err != nil {
		t.Fatalf("NewExtractionService: %v", err)
	}
	return svc, repo
}

func TestPipelineExtractsSavesAndMarksExtracted(t *testing.T) {
	root := t.TempDir()
	storeTestPDF(t, root, "doc-pipe", buildTestPDF(t, []string{
		textStream("Q1. What is a process? Explain with a diagram."),
		textStream("Q2. Define deadlock and give an example scenario."),
	}))

	extractor, err := NewService(root)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	repo := newFakeRepo("doc-pipe")
	qrepo := &fakeQuestionsRepo{}
	svc, err := NewExtractionService(root, extractor, repo, qrepo)
	if err != nil {
		t.Fatalf("NewExtractionService: %v", err)
	}

	got, err := svc.Extract(context.Background(), "doc-pipe", "documents/doc-pipe/original.pdf")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if got.PageCount != 2 {
		t.Errorf("PageCount = %d, want 2", got.PageCount)
	}
	if repo.statuses["doc-pipe"] != documents.StatusExtracted {
		t.Errorf("status = %q, want %q", repo.statuses["doc-pipe"], documents.StatusExtracted)
	}

	summary := got.Summary()
	if summary.ErrorPages != 0 || summary.PagesNeedingOCR != 0 {
		t.Errorf("Summary = %+v, want no errors and no OCR pages", summary)
	}

	// pages.json must exist and round-trip to the same extraction.
	data, err := os.ReadFile(filepath.Join(root, "documents", "doc-pipe", "extraction", "pages.json"))
	if err != nil {
		t.Fatalf("read pages.json: %v", err)
	}
	var saved DocumentExtraction
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("unmarshal pages.json: %v", err)
	}
	if len(saved.Pages) != 2 {
		t.Fatalf("len(saved.Pages) = %d, want 2", len(saved.Pages))
	}
	if saved.Pages[0].Number != 1 || !strings.Contains(saved.Pages[0].Text, "What is a process?") {
		t.Errorf("saved page 1 = %+v, want page 1 with question text", saved.Pages[0])
	}

	// Debug dumps: one file per page containing the page text.
	debugDir := filepath.Join(root, "documents", "doc-pipe", "extraction", "debug")
	for _, want := range []struct {
		name string
		text string
	}{
		{"page-001.txt", "What is a process?"},
		{"page-002.txt", "Define deadlock"},
	} {
		raw, err := os.ReadFile(filepath.Join(debugDir, want.name))
		if err != nil {
			t.Fatalf("read debug/%s: %v", want.name, err)
		}
		if !strings.Contains(string(raw), want.text) {
			t.Errorf("debug/%s = %q, want to contain %q", want.name, raw, want.text)
		}
	}
}

func TestPipelineEmptyStoragePathUsesCanonicalLayout(t *testing.T) {
	root := t.TempDir()
	storeTestPDF(t, root, "doc-canonical", buildTestPDF(t, []string{
		textStream("Plenty of text lives here so OCR is unnecessary."),
	}))

	extractor, err := NewService(root)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	repo := newFakeRepo("doc-canonical")
	qrepo := &fakeQuestionsRepo{}
	svc, err := NewExtractionService(root, extractor, repo, qrepo)
	if err != nil {
		t.Fatalf("NewExtractionService: %v", err)
	}

	if _, err := svc.Extract(context.Background(), "doc-canonical", ""); err != nil {
		t.Fatalf("Extract with empty storage path: %v", err)
	}
	if repo.statuses["doc-canonical"] != documents.StatusExtracted {
		t.Errorf("status = %q, want %q", repo.statuses["doc-canonical"], documents.StatusExtracted)
	}
}

func TestPipelineMarksFailedOnBrokenPDF(t *testing.T) {
	root := t.TempDir()
	storeTestPDF(t, root, "doc-broken", []byte("%PDF-1.4\ngarbage without xref\n"))

	extractor, err := NewService(root)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	repo := newFakeRepo("doc-broken")
	qrepo := &fakeQuestionsRepo{}
	svc, err := NewExtractionService(root, extractor, repo, qrepo)
	if err != nil {
		t.Fatalf("NewExtractionService: %v", err)
	}

	if _, err := svc.Extract(context.Background(), "doc-broken", "documents/doc-broken/original.pdf"); err == nil {
		t.Fatal("Extract of broken PDF succeeded, want error")
	}
	if repo.statuses["doc-broken"] != documents.StatusFailed {
		t.Errorf("status = %q, want %q", repo.statuses["doc-broken"], documents.StatusFailed)
	}
	if _, err := os.Stat(filepath.Join(root, "documents", "doc-broken", "extraction")); !os.IsNotExist(err) {
		t.Errorf("extraction dir exists after failed run, want it absent (stat err = %v)", err)
	}
}

func TestPipelineRejectsUnsafePaths(t *testing.T) {
	svc, repo := newTestPipeline(t)
	cases := []struct {
		id, path string
	}{
		{"../escape", ""},
		{"a/b", ""},
		{"doc-ok", "../../etc/passwd"},
	}
	for _, c := range cases {
		if _, err := svc.Extract(context.Background(), c.id, c.path); err == nil {
			t.Errorf("Extract(%q, %q) succeeded, want rejection", c.id, c.path)
		}
	}
	if len(repo.statuses) != 0 {
		t.Errorf("statuses recorded for rejected inputs: %v", repo.statuses)
	}
}

func TestSummaryCountsPagesNeedingOCRAndErrors(t *testing.T) {
	root := t.TempDir()
	storeTestPDF(t, root, "doc-mixed", buildTestPDF(t, []string{
		textStream("This page has plenty of extractable text content."),
		"",         // blank -> NeedsOCR
		"BT Tj ET", // per-page parse error
	}))

	extractor, err := NewService(root)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	repo := newFakeRepo("doc-mixed")
	qrepo := &fakeQuestionsRepo{}
	svc, err := NewExtractionService(root, extractor, repo, qrepo)
	if err != nil {
		t.Fatalf("NewExtractionService: %v", err)
	}

	got, err := svc.Extract(context.Background(), "doc-mixed", "")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	summary := got.Summary()
	if summary.PageCount != 3 {
		t.Errorf("PageCount = %d, want 3", summary.PageCount)
	}
	if summary.ExtractedPages != 2 {
		t.Errorf("ExtractedPages = %d, want 2", summary.ExtractedPages)
	}
	if summary.PagesNeedingOCR != 1 {
		t.Errorf("PagesNeedingOCR = %d, want 1", summary.PagesNeedingOCR)
	}
	if summary.ErrorPages != 1 {
		t.Errorf("ErrorPages = %d, want 1", summary.ErrorPages)
	}
}

func TestNewExtractionServiceRejectsNilDeps(t *testing.T) {
	extractor, err := NewService(t.TempDir())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if _, err := NewExtractionService(t.TempDir(), nil, newFakeRepo(), &fakeQuestionsRepo{}); err == nil {
		t.Error("nil extractor accepted, want error")
	}
	if _, err := NewExtractionService(t.TempDir(), extractor, nil, &fakeQuestionsRepo{}); err == nil {
		t.Error("nil repository accepted, want error")
	}
}
