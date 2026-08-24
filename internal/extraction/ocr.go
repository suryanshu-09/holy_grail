package extraction

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// OCRer is the fallback text recognition step for pages whose regular text
// extraction came up empty or too short (typically scanned pages). pdfPath is
// the absolute path of the source PDF and pageNum is its 1-based page number.
type OCRer interface {
	OCR(ctx context.Context, pdfPath string, pageNum int) (string, error)
}

const (
	pdftoppmBinary   = "pdftoppm"
	tesseractBinary  = "tesseract"
	renderResolution = "300" // DPI used when rasterizing the page
)

// TesseractOCR implements OCRer by rasterizing a single PDF page with
// pdftoppm (poppler-utils) and recognizing the resulting image with the
// tesseract CLI. Both binaries must be installed on the host.
type TesseractOCR struct{}

// NewTesseractOCR verifies that the external binaries are present in PATH and
// returns a ready-to-use OCR implementation. The returned error names the
// missing binary so operators know what to install.
func NewTesseractOCR() (*TesseractOCR, error) {
	if err := checkOCRBins(); err != nil {
		return nil, err
	}
	return &TesseractOCR{}, nil
}

// checkOCRBins reports a clear error when either external dependency is
// missing.
func checkOCRBins() error {
	if _, err := exec.LookPath(pdftoppmBinary); err != nil {
		return fmt.Errorf("extraction: %s not found in PATH (install poppler-utils): %w", pdftoppmBinary, err)
	}
	if _, err := exec.LookPath(tesseractBinary); err != nil {
		return fmt.Errorf("extraction: %s not found in PATH (install tesseract-ocr): %w", tesseractBinary, err)
	}
	return nil
}

// OCR renders the given page of pdfPath to a temporary PNG with pdftoppm and
// runs tesseract on it, returning the recognized plain text.
func (o *TesseractOCR) OCR(ctx context.Context, pdfPath string, pageNum int) (string, error) {
	if pageNum < 1 {
		return "", fmt.Errorf("extraction: ocr: page number must be >= 1, got %d", pageNum)
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("extraction: ocr: %w", err)
	}
	if err := checkOCRBins(); err != nil {
		return "", err
	}

	dir, err := os.MkdirTemp("", "holy_grail-ocr-")
	if err != nil {
		return "", fmt.Errorf("extraction: ocr: create temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	// -singlefile makes pdftoppm write <prefix>.png instead of <prefix>-N.png.
	prefix := filepath.Join(dir, "page")
	var renderErr bytes.Buffer
	render := exec.CommandContext(ctx,
		pdftoppmBinary,
		"-f", strconv.Itoa(pageNum),
		"-l", strconv.Itoa(pageNum),
		"-r", renderResolution,
		"-png",
		"-singlefile",
		pdfPath, prefix,
	)
	render.Stderr = &renderErr
	if err := render.Run(); err != nil {
		return "", fmt.Errorf("extraction: ocr: render page %d with %s: %w: %s",
			pageNum, pdftoppmBinary, err, strings.TrimSpace(renderErr.String()))
	}

	var recognizeOut, recognizeErr bytes.Buffer
	recognize := exec.CommandContext(ctx, tesseractBinary, prefix+".png", "stdout")
	recognize.Stdout = &recognizeOut
	recognize.Stderr = &recognizeErr
	if err := recognize.Run(); err != nil {
		return "", fmt.Errorf("extraction: ocr: run %s on page %d: %w: %s",
			tesseractBinary, pageNum, err, strings.TrimSpace(recognizeErr.String()))
	}
	return recognizeOut.String(), nil
}
