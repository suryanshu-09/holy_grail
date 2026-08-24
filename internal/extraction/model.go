package extraction

// ImageRef references an image embedded on a PDF page.
type ImageRef struct {
	Name string `json:"name"`
}

// Page is the extracted representation of a single PDF page. Error is
// non-empty when the page itself could not be extracted; a failing page never
// aborts extraction of the remaining pages. NeedsOCR marks pages whose
// extracted text is missing or too short to be useful, which usually means
// the page is blank or scanned and requires OCR.
type Page struct {
	Number   int        `json:"number"` // 1-based page number
	Text     string     `json:"text"`
	Images   []ImageRef `json:"images"`
	NeedsOCR bool       `json:"needs_ocr,omitempty"`
	Error    string     `json:"error,omitempty"`
}

// DocumentExtraction is the result of extracting every page of one document.
type DocumentExtraction struct {
	DocumentID string `json:"document_id"`
	PageCount  int    `json:"page_count"`
	Pages      []Page `json:"pages"`
}

// ExtractionSummary is the API-facing overview of one extraction run: how
// many pages were processed, how many produced usable text, how many still
// need OCR and how many failed outright.
type ExtractionSummary struct {
	DocumentID      string `json:"document_id"`
	PageCount       int    `json:"page_count"`
	ExtractedPages  int    `json:"extracted_pages"`
	PagesNeedingOCR int    `json:"pages_needing_ocr"`
	ErrorPages      int    `json:"error_pages"`
}

// Summary condenses the extraction into an ExtractionSummary.
func (d DocumentExtraction) Summary() ExtractionSummary {
	s := ExtractionSummary{
		DocumentID:     d.DocumentID,
		PageCount:      d.PageCount,
		ExtractedPages: len(d.Pages),
	}
	for _, p := range d.Pages {
		if p.Error != "" {
			s.ErrorPages++
			s.ExtractedPages--
		}
		if p.NeedsOCR {
			s.PagesNeedingOCR++
		}
	}
	return s
}
