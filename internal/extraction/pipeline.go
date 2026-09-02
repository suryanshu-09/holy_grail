package extraction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/suryanshu-09/holy_grail/internal/documents"
	"github.com/suryanshu-09/holy_grail/internal/questions"
)

const (
	// extractionDirName is the directory, next to original.pdf, holding the
	// intermediate results of a run: pages.json plus debug page dumps.
	extractionDirName = "extraction"

	debugDirName  = "debug"
	pagesFileName = "pages.json"
)

// ExtractionService wires the page extraction pipeline into the application:
// it runs extract -> normalize -> detect -> optional OCR for one uploaded
// document, saves the intermediate results under
// <root>/documents/<id>/extraction/ and keeps documents.status up to date via
// the documents repository.
type ExtractionService struct {
	root         string
	extractor    *Service
	repo         documents.Repository
	questionRepo questions.Repository
	llmFallback  *LLMFallback
	debugWriter  *DebugWriter
}

// WithLLMFallback attaches an LLM fallback to the extraction service.
func (s *ExtractionService) WithLLMFallback(f *LLMFallback) *ExtractionService {
	s.llmFallback = f
	return s
}

// WithDebugWriter attaches a debug writer that also receives question extraction
// artifacts for replay and QA. This is separate from the page-level debug writer
// on the Service itself.
func (s *ExtractionService) WithDebugWriter(w *DebugWriter) *ExtractionService {
	s.debugWriter = w
	return s
}

// NewExtractionService resolves root (e.g. ./data) to an absolute path and
// returns an extraction service that persists intermediates there.
func NewExtractionService(root string, extractor *Service, repo documents.Repository, qrepo questions.Repository) (*ExtractionService, error) {
	if extractor == nil {
		return nil, fmt.Errorf("extraction: nil extractor service")
	}
	if repo == nil {
		return nil, fmt.Errorf("extraction: nil documents repository")
	}
	if qrepo == nil {
		return nil, fmt.Errorf("extraction: nil questions repository")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("extraction: resolve root: %w", err)
	}
	return &ExtractionService{root: abs, extractor: extractor, repo: repo, questionRepo: qrepo}, nil
}

// Extract runs the full pipeline for one document. documentID identifies the
// row in the documents table and storagePath is its slash-separated location
// relative to the data root as recorded in documents.storage_path; an empty
// storagePath falls back to the canonical layout written by
// storage.LocalStore (documents/<id>/original.pdf).
//
// On success the document status becomes "extracted"; when the pipeline fails
// it becomes "failed" and the error is returned. Intermediate results are
// saved before the status flips to extracted so a crash never advertises
// extracted without artifacts on disk.
func (s *ExtractionService) Extract(ctx context.Context, documentID, storagePath string) (DocumentExtraction, error) {
	pdfPath, err := s.resolvePDF(documentID, storagePath)
	if err != nil {
		return DocumentExtraction{}, err
	}

	result, err := s.extractor.ExtractFile(ctx, documentID, pdfPath)
	if err != nil {
		return DocumentExtraction{}, s.markFailed(ctx, documentID, err)
	}

	if err := s.saveResults(documentID, result); err != nil {
		return DocumentExtraction{}, s.markFailed(ctx, documentID, err)
	}

	// Run question extraction and persist detected questions. Failures in
	// question persistence mark the document failed so they are visible to
	// operators and can be retried.
	if err := s.ExtractQuestions(ctx, result); err != nil {
		return DocumentExtraction{}, s.markFailed(ctx, documentID, err)
	}

	if err := s.repo.UpdateStatus(ctx, documentID, documents.StatusExtracted); err != nil {
		return DocumentExtraction{}, fmt.Errorf("extraction: mark %q extracted: %w", documentID, err)
	}
	return result, nil
}

// markFailed records the failure in documents.status and returns the original
// pipeline error wrapped with any status-update failure joined onto it.
func (s *ExtractionService) markFailed(ctx context.Context, documentID string, cause error) error {
	err := cause
	if updateErr := s.repo.UpdateStatus(ctx, documentID, documents.StatusFailed); updateErr != nil {
		err = errors.Join(cause, fmt.Errorf("extraction: mark %q failed: %w", documentID, updateErr))
	}
	return err
}

// resolvePDF turns the stored relative path into an absolute PDF path inside
// the data root, guarding against traversal via crafted IDs or paths. An
// empty storagePath falls back to the canonical layout of
// storage.LocalStore: <root>/documents/<id>/original.pdf.
func (s *ExtractionService) resolvePDF(documentID, storagePath string) (string, error) {
	if !safeSegment(documentID) {
		return "", fmt.Errorf("extraction: unsafe document id %q", documentID)
	}
	rel := filepath.Join(documentLayout, documentID, originalName)
	if storagePath != "" {
		rel = filepath.FromSlash(storagePath)
	}
	dest := filepath.Join(s.root, rel)
	if !withinRoot(s.root, dest) {
		return "", fmt.Errorf("extraction: storage path escapes root: %q", storagePath)
	}
	return dest, nil
}

// saveResults writes pages.json plus one debug/page-NNN.txt dump per page
// under <root>/documents/<id>/extraction/. Files are written atomically so a
// concurrent or crashed run never leaves truncated JSON behind. It also writes
// an images.json manifest that enumerates extracted images with their page
// positions and storage paths.
func (s *ExtractionService) saveResults(documentID string, result DocumentExtraction) error {
	dir := filepath.Join(s.root, documentLayout, documentID, extractionDirName)
	debugDir := filepath.Join(dir, debugDirName)
	if err := os.MkdirAll(debugDir, 0o755); err != nil {
		return fmt.Errorf("extraction: create extraction dir: %w", err)
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("extraction: encode pages: %w", err)
	}
	if err := writeFileAtomic(filepath.Join(dir, pagesFileName), append(data, '\n')); err != nil {
		return fmt.Errorf("extraction: write pages.json: %w", err)
	}

	for _, page := range result.Pages {
		name := "page-" + pad3(page.Number) + ".txt"
		if err := writeFileAtomic(filepath.Join(debugDir, name), []byte(page.Text)); err != nil {
			return fmt.Errorf("extraction: write %s: %w", name, err)
		}
	}

	// Write images manifest
	var allImages []ImageRef
	for _, pg := range result.Pages {
		allImages = append(allImages, pg.Images...)
	}
	if allImages == nil {
		allImages = []ImageRef{}
	}
	if imgData, err := json.MarshalIndent(allImages, "", "  "); err == nil {
		_ = writeFileAtomic(filepath.Join(dir, "images.json"), append(imgData, '\n'))
		_ = writeFileAtomic(filepath.Join(debugDir, "images.json"), append(imgData, '\n'))
	}
	return nil
}

// writeFileAtomic writes data to path through a temp file plus rename so
// partial writes are never visible at the final path, mirroring
// internal/storage/local.go.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("sync file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("finalize file: %w", err)
	}
	return nil
}

func pad3(n int) string {
	s := strconv.Itoa(n)
	for len(s) < 3 {
		s = "0" + s
	}
	return s
}
