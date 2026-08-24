package extraction

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeOCRer records every call and returns canned results, so tests never
// depend on external OCR binaries.
type fakeOCRer struct {
	calls []ocrCall
	text  string
	err   error
}

type ocrCall struct {
	pdfPath string
	pageNum int
}

func (f *fakeOCRer) OCR(ctx context.Context, pdfPath string, pageNum int) (string, error) {
	f.calls = append(f.calls, ocrCall{pdfPath: pdfPath, pageNum: pageNum})
	if f.err != nil {
		return "", f.err
	}
	return f.text, nil
}

// longText is comfortably above minTextChars so pages using it are not
// flagged NeedsOCR.
var longText = strings.Repeat("Question about operating systems. ", 4)

func TestExtractRunsOCRFallback(t *testing.T) {
	root := t.TempDir()
	storeTestPDF(t, root, "doc-scanned", buildTestPDF(t, []string{
		textStream(longText),
		"", // scanned page: no text layer
	}))

	ocr := &fakeOCRer{text: "Q1. What  is   a process?\n\n\nQ2. Define deadlock."}
	svc, err := NewService(root)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	svc.WithOCRer(ocr)

	got, err := svc.Extract(context.Background(), "doc-scanned")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if len(ocr.calls) != 1 {
		t.Fatalf("OCR calls = %d (%#v), want exactly 1", len(ocr.calls), ocr.calls)
	}
	wantPath := filepath.Join(root, "documents", "doc-scanned", "original.pdf")
	if ocr.calls[0].pdfPath != wantPath {
		t.Errorf("OCR pdfPath = %q, want %q", ocr.calls[0].pdfPath, wantPath)
	}
	if ocr.calls[0].pageNum != 2 {
		t.Errorf("OCR pageNum = %d, want 2", ocr.calls[0].pageNum)
	}

	page := got.Pages[1]
	if page.NeedsOCR {
		t.Error("Pages[1].NeedsOCR = true after successful OCR, want false")
	}
	if page.Text != "Q1. What is a process?\n\nQ2. Define deadlock." {
		t.Errorf("Pages[1].Text = %q, want normalized OCR text", page.Text)
	}
	if page.Error != "" {
		t.Errorf("Pages[1].Error = %q, want empty", page.Error)
	}
	if got.Pages[0].NeedsOCR {
		t.Error("Pages[0].NeedsOCR = true, want false for a text-bearing page")
	}
}

func TestExtractSkipsOCRWithoutOCRer(t *testing.T) {
	root := t.TempDir()
	storeTestPDF(t, root, "doc-norcr", buildTestPDF(t, []string{
		"", // blank/scanned page
	}))

	svc, err := NewService(root)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	got, err := svc.Extract(context.Background(), "doc-norcr")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !got.Pages[0].NeedsOCR {
		t.Error("Pages[0].NeedsOCR = false without OCRer, want true (flag still set)")
	}
	if got.Pages[0].Text != "" {
		t.Errorf("Pages[0].Text = %q, want empty", got.Pages[0].Text)
	}
}

func TestExtractDoesNotOCRReadablePages(t *testing.T) {
	root := t.TempDir()
	storeTestPDF(t, root, "doc-readable", buildTestPDF(t, []string{
		textStream(longText),
		textStream(longText),
	}))

	ocr := &fakeOCRer{text: "should not be used"}
	svc, _ := NewService(root)
	svc.WithOCRer(ocr)

	got, err := svc.Extract(context.Background(), "doc-readable")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(ocr.calls) != 0 {
		t.Errorf("OCR calls = %d, want 0 for readable pages", len(ocr.calls))
	}
	for i, page := range got.Pages {
		if page.NeedsOCR {
			t.Errorf("Pages[%d].NeedsOCR = true, want false", i)
		}
	}
}

func TestExtractContinuesWhenOCRFails(t *testing.T) {
	root := t.TempDir()
	storeTestPDF(t, root, "doc-ocrfail", buildTestPDF(t, []string{
		textStream(longText),
		"", // triggers the failing OCR call
		textStream(longText),
	}))

	ocr := &fakeOCRer{err: errors.New("tesseract exploded")}
	svc, _ := NewService(root)
	svc.WithOCRer(ocr)

	got, err := svc.Extract(context.Background(), "doc-ocrfail")
	if err != nil {
		t.Fatalf("Extract aborted on OCR failure: %v", err)
	}
	if len(ocr.calls) != 1 {
		t.Fatalf("OCR calls = %d, want 1", len(ocr.calls))
	}
	if !got.Pages[1].NeedsOCR {
		t.Error("Pages[1].NeedsOCR = false after failed OCR, want true (still needs OCR)")
	}
	if got.Pages[1].Error != "" {
		t.Errorf("Pages[1].Error = %q, want empty (OCR failure skips gracefully)", got.Pages[1].Error)
	}
	for _, i := range []int{0, 2} {
		if got.Pages[i].NeedsOCR || got.Pages[i].Error != "" {
			t.Errorf("Pages[%d] = NeedsOCR %v / Error %q, want clean readable page",
				i, got.Pages[i].NeedsOCR, got.Pages[i].Error)
		}
	}
}

func TestExtractKeepsNeedsOCRWhenOCRResultEmpty(t *testing.T) {
	root := t.TempDir()
	storeTestPDF(t, root, "doc-empty-ocr", buildTestPDF(t, []string{
		"", // scanned page that OCR cannot read either
	}))

	ocr := &fakeOCRer{text: "   \n\t  "}
	svc, _ := NewService(root)
	svc.WithOCRer(ocr)

	got, err := svc.Extract(context.Background(), "doc-empty-ocr")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(ocr.calls) != 1 {
		t.Fatalf("OCR calls = %d, want 1", len(ocr.calls))
	}
	if !got.Pages[0].NeedsOCR {
		t.Error("Pages[0].NeedsOCR = false after whitespace-only OCR result, want true")
	}
}

func TestNewTesseractOCRMissingBinaries(t *testing.T) {
	emptyBin := t.TempDir() // PATH without pdftoppm/tesseract
	t.Setenv("PATH", emptyBin)

	if _, err := NewTesseractOCR(); err == nil {
		t.Fatal("NewTesseractOCR succeeded without binaries, want error")
	} else if !strings.Contains(err.Error(), pdftoppmBinary) {
		t.Errorf("error = %v, want it to name %s", err, pdftoppmBinary)
	}
}

func TestNewTesseractOCRMissingTesseractOnly(t *testing.T) {
	bin := t.TempDir()
	writeFakeScript(t, filepath.Join(bin, pdftoppmBinary), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", bin)

	_, err := NewTesseractOCR()
	if err == nil {
		t.Fatal("NewTesseractOCR succeeded without tesseract, want error")
	}
	if !strings.Contains(err.Error(), tesseractBinary) {
		t.Errorf("error = %v, want it to name %s", err, tesseractBinary)
	}
}

func TestTesseractOCRWiring(t *testing.T) {
	// Fake binaries prove the render->recognize pipeline is wired correctly
	// without requiring poppler-utils or tesseract to be installed.
	bin := t.TempDir()

	// pdftoppm must receive "-singlefile PDF PREFIX" and create PREFIX.png.
	writeFakeScript(t, filepath.Join(bin, pdftoppmBinary), `#!/bin/sh
after=
for arg in "$@"; do
    if [ "$after" = "2" ]; then
        printf 'PNGDATA' > "$arg.png" || exit 2
        exit 0
    fi
    if [ "$after" = "1" ]; then
        after=2
        continue
    fi
    if [ "$arg" = "-singlefile" ]; then
        after=1
    fi
done
exit 5
`)
	// tesseract must receive an existing PNG path plus "stdout".
	writeFakeScript(t, filepath.Join(bin, tesseractBinary), `#!/bin/sh
[ -s "$1" ] || { echo "input image missing: $1" >&2; exit 3; }
[ "$2" = "stdout" ] || exit 4
echo "Recognized question paper text"
`)
	t.Setenv("PATH", bin)

	ocr, err := NewTesseractOCR()
	if err != nil {
		t.Fatalf("NewTesseractOCR: %v", err)
	}

	pdfPath := filepath.Join(t.TempDir(), "original.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-fake"), 0o644); err != nil {
		t.Fatalf("write fake pdf: %v", err)
	}

	got, err := ocr.OCR(context.Background(), pdfPath, 3)
	if err != nil {
		t.Fatalf("OCR: %v", err)
	}
	if got != "Recognized question paper text\n" {
		t.Errorf("OCR text = %q, want %q", got, "Recognized question paper text\n")
	}
}

func TestTesseractOCRPropagatesFailure(t *testing.T) {
	bin := t.TempDir()
	writeFakeScript(t, filepath.Join(bin, pdftoppmBinary), "#!/bin/sh\nexit 0\n")
	writeFakeScript(t, filepath.Join(bin, tesseractBinary), "#!/bin/sh\necho nope >&2\nexit 1\n")
	t.Setenv("PATH", bin)

	ocr, err := NewTesseractOCR()
	if err != nil {
		t.Fatalf("NewTesseractOCR: %v", err)
	}
	if _, err := ocr.OCR(context.Background(), "/does/not/matter.pdf", 1); err == nil {
		t.Fatal("OCR succeeded while fake tesseract failed, want error")
	}
}

func TestTesseractOCRRejectsInvalidPageNumber(t *testing.T) {
	ocr := &TesseractOCR{}
	if _, err := ocr.OCR(context.Background(), "/unused.pdf", 0); err == nil {
		t.Error("OCR with pageNum 0 succeeded, want error")
	}
}

func writeFakeScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
