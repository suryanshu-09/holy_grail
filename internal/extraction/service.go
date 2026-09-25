package extraction

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
//
// Page text extraction runs concurrently in a bounded worker pool sized by
// extractConcurrency (default NumCPU, override EXTRACT_CONCURRENCY). Page
// order is preserved (results are written by index) and ctx cancellation is
// honored per page. The underlying PDF reader is not safe for concurrent
// use, so raw page reads are serialized with a mutex while normalization
// and OCR fallback run concurrently.
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
		Pages:      make([]Page, pageCount),
	}
	if pageCount == 0 {
		result.Pages = []Page{}
	} else {
		concurrency := extractConcurrency()
		if concurrency > pageCount {
			concurrency = pageCount
		}
		sem := make(chan struct{}, concurrency)
		var wg sync.WaitGroup
		var pdfMu sync.Mutex
		ocrer := s.ocr
		for i := 1; i <= pageCount; i++ {
			if err := ctx.Err(); err != nil {
				// Mark remaining pages as cancelled but keep order.
				for j := i; j <= pageCount; j++ {
					result.Pages[j-1] = Page{Number: j, Images: []ImageRef{}, Error: err.Error()}
				}
				break
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(pageNum int) {
				defer wg.Done()
				defer func() { <-sem }()
				page := Page{Number: pageNum, Images: []ImageRef{}}
				if err := ctx.Err(); err != nil {
					page.Error = err.Error()
					result.Pages[pageNum-1] = page
					return
				}
				// Serialize raw PDF access: ledongthuc/pdf shares the
				// underlying file offset and is not goroutine-safe.
				pdfMu.Lock()
				p := reader.Page(pageNum)
				isNull := p.V.IsNull()
				var text string
				var textErr error
				if !isNull {
					text, textErr = p.GetPlainText(nil)
				}
				pdfMu.Unlock()

				if isNull {
					page.Error = fmt.Sprintf("page %d is missing from the document structure", pageNum)
					result.Pages[pageNum-1] = page
					return
				}
				if textErr != nil {
					page.Error = textErr.Error()
					result.Pages[pageNum-1] = page
					return
				}
				page.Text = normalizeText(text)
				page.NeedsOCR = needsOCR(page.Text)

				// OCR fallback: only run on detected pages and only when an OCRer is
				// configured. Failures skip gracefully — a missing or broken OCR
				// setup never aborts extraction; such pages simply keep NeedsOCR set
				// and whatever text was extracted.
				if page.NeedsOCR && ocrer != nil {
					if ocrText, err := ocrer.OCR(ctx, path, pageNum); err == nil {
						if cleaned := normalizeText(ocrText); cleaned != "" {
							page.Text = cleaned
							page.NeedsOCR = false
						}
					}
				}

				result.Pages[pageNum-1] = page
			}(i)
		}
		wg.Wait()
	}

	// Image extraction: preserve embedded images per page, record positions,
	// save files locally and generate thumbnails. Failures are non-fatal.
	// Clean previous images dir to avoid stale files on re-extraction.
	imagesRoot := filepath.Join(s.root, documentLayout, documentID, imagesDirName)
	_ = os.RemoveAll(imagesRoot)
	_ = os.MkdirAll(imagesRoot, 0o755)
	if imageMap := s.extractImages(ctx, documentID, path, reader); len(imageMap) > 0 {
		for idx, pg := range result.Pages {
			if refs, ok := imageMap[pg.Number]; ok && len(refs) > 0 {
				result.Pages[idx].Images = refs
			}
		}
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
