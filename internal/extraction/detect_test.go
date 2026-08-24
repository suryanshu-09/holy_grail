package extraction

import (
	"context"
	"testing"
)

func TestNeedsOCR(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "empty text needs OCR",
			text: "",
			want: true,
		},
		{
			name: "whitespace-only text needs OCR",
			text: "   \n\t  ",
			want: true,
		},
		{
			name: "short text below threshold needs OCR",
			text: "Q1. Deadlock?",
			want: true,
		},
		{
			name: "text exactly at threshold does not need OCR",
			text: "12345678901234567890",
			want: false,
		},
		{
			name: "longer text does not need OCR",
			text: "Q1. What is a process? Explain the process states with a neat diagram.",
			want: false,
		},
		{
			name: "multi-line text counts all characters",
			text: "Q1. What is a process?\n\nQ2. Define deadlock.",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := needsOCR(tt.text); got != tt.want {
				t.Errorf("needsOCR(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestExtractMarksScannedPages(t *testing.T) {
	root := t.TempDir()
	storeTestPDF(t, root, "doc-scanned", buildTestPDF(t, []string{
		textStream("This page has plenty of extractable text content."),
		"", // blank page, likely scanned
		textStream("Short page."),
	}))

	svc, err := NewService(root)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	got, err := svc.Extract(context.Background(), "doc-scanned")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if got.Pages[0].NeedsOCR {
		t.Errorf("Pages[0].NeedsOCR = true, want false for a text-bearing page")
	}
	if !got.Pages[1].NeedsOCR {
		t.Errorf("Pages[1].NeedsOCR = false, want true for a blank/scanned page")
	}
	if !got.Pages[2].NeedsOCR {
		t.Errorf("Pages[2].NeedsOCR = false, want true below minTextChars")
	}
}
