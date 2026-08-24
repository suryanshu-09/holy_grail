package extraction

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	pdf "github.com/ledongthuc/pdf"
)

// documentLayout mirrors the storage layout of internal/storage/local.go:
// documents/<document-id>/original.pdf under the data root.
const (
	documentLayout = "documents"
	originalName   = "original.pdf"

	// minTextChars is the minimum number of extracted characters for a page
	// to count as text-bearing. Pages with fewer characters (including no
	// text at all) are likely scanned or blank and are marked NeedsOCR.
	minTextChars = 20
)

// Service extracts page-level text from documents stored on the local
// filesystem.
type Service struct {
	root  string
	ocr   OCRer
	debug *DebugWriter
}

// NewService resolves root (e.g. ./data) to an absolute path and returns an
// extraction service for it.
func NewService(root string) (*Service, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("extraction: resolve root: %w", err)
	}
	return &Service{root: abs}, nil
}

// WithOCRer attaches an OCR fallback used for pages whose extracted text is
// missing or too short (scanned pages). It returns the service so callers can
// chain: svc, _ := NewService(root).WithOCRer(NewTesseractOCR()).
func (s *Service) WithOCRer(o OCRer) *Service {
	s.ocr = o
	return s
}

// WithDebugWriter attaches a writer that persists every extraction result as
// debug/<document-id>/page-NNN.txt files plus summary.json. It returns the
// service so callers can chain.
func (s *Service) WithDebugWriter(w *DebugWriter) *Service {
	s.debug = w
	return s
}

// Extract reads the stored original.pdf of a document and returns one Page
// entry per PDF page with its 1-based page number. Pages that fail to parse
// carry their error in Page.Error instead of failing the whole extraction.
// When an OCRer is configured (see WithOCRer), pages flagged NeedsOCR are
// recognized as a fallback; OCR failures are skipped gracefully and leave the
// NeedsOCR flag set.
func (s *Service) Extract(ctx context.Context, documentID string) (DocumentExtraction, error) {
	path, err := s.originalPath(documentID)
	if err != nil {
		return DocumentExtraction{}, err
	}
	return s.ExtractFile(ctx, documentID, path)
}

// ExtractFile is the storage-agnostic form of Extract: it parses the PDF at
// path and attributes the pages to documentID.
func (s *Service) ExtractFile(ctx context.Context, documentID, path string) (DocumentExtraction, error) {
	if err := ctx.Err(); err != nil {
		return DocumentExtraction{}, fmt.Errorf("extraction: %w", err)
	}

	file, reader, err := pdf.Open(path)
	if err != nil {
		return DocumentExtraction{}, fmt.Errorf("extraction: open pdf %q: %w", documentID, err)
	}
	defer file.Close()

	pageCount := reader.NumPage()
	result := DocumentExtraction{
		DocumentID: documentID,
		PageCount:  pageCount,
		Pages:      make([]Page, 0, pageCount),
	}
	for i := 1; i <= pageCount; i++ {
		page := Page{Number: i, Images: []ImageRef{}}

		p := reader.Page(i)
		if p.V.IsNull() {
			page.Error = fmt.Sprintf("page %d is missing from the document structure", i)
			result.Pages = append(result.Pages, page)
			continue
		}

		text, err := p.GetPlainText(nil)
		if err != nil {
			page.Error = err.Error()
			result.Pages = append(result.Pages, page)
			continue
		}
		page.Text = normalizeText(text)
		page.NeedsOCR = needsOCR(page.Text)

		// OCR fallback: only run on detected pages and only when an OCRer is
		// configured. Failures skip gracefully — a missing or broken OCR
		// setup never aborts extraction; such pages simply keep NeedsOCR set
		// and whatever text was extracted.
		if page.NeedsOCR && s.ocr != nil {
			if ocrText, err := s.ocr.OCR(ctx, path, i); err == nil {
				if cleaned := normalizeText(ocrText); cleaned != "" {
					page.Text = cleaned
					page.NeedsOCR = false
				}
			}
		}

		result.Pages = append(result.Pages, page)
	}

	// Debug output is purely diagnostic: like the OCR fallback above, write
	// failures never abort an otherwise successful extraction.
	if s.debug != nil {
		_ = s.debug.Write(result)
	}
	return result, nil
}

// needsOCR reports whether a page's extracted text is missing or below the
// minTextChars threshold, meaning the page probably has no extractable text
// layer (blank or scanned) and requires OCR.
func needsOCR(text string) bool {
	return utf8.RuneCountInString(strings.TrimSpace(text)) < minTextChars
}

// originalPath resolves <root>/documents/<id>/original.pdf using the same
// safety checks as internal/storage/local.go.
func (s *Service) originalPath(id string) (string, error) {
	if !safeSegment(id) {
		return "", fmt.Errorf("extraction: unsafe document id %q", id)
	}
	dest := filepath.Join(s.root, documentLayout, id, originalName)
	if !withinRoot(s.root, dest) {
		return "", fmt.Errorf("extraction: resolved path escapes root: %q", dest)
	}
	return dest, nil
}

func safeSegment(id string) bool {
	return id != "" && !strings.ContainsAny(id, `/\`) && !strings.Contains(id, "..")
}

func withinRoot(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
