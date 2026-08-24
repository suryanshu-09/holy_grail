package extraction

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// buildTestPDF assembles a minimal but structurally valid PDF containing one
// content stream per page, with a correct xref table.
func buildTestPDF(t *testing.T, pageStreams []string) []byte {
	t.Helper()

	var buf strings.Builder
	offsets := make(map[int]int)

	writeObj := func(num int, body string) {
		offsets[num] = buf.Len()
		buf.WriteString(itoa(num) + " 0 obj\n" + body + "\nendobj\n")
	}

	buf.WriteString("%PDF-1.4\n")

	fontNum := 3 + 2*len(pageStreams)
	kids := make([]string, len(pageStreams))
	for i, stream := range pageStreams {
		pageNum := 3 + 2*i
		contentNum := pageNum + 1
		kids[i] = itoa(pageNum) + " 0 R"
		writeObj(pageNum, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /Font << /F1 "+itoa(fontNum)+" 0 R >> >> "+
			"/Contents "+itoa(contentNum)+" 0 R >>")
		writeObj(contentNum, "<< /Length "+itoa(len(stream))+" >>\nstream\n"+stream+"\nendstream")
	}
	writeObj(fontNum, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, "<< /Type /Pages /Kids ["+strings.Join(kids, " ")+"] /Count "+itoa(len(pageStreams))+" >>")

	xrefStart := buf.Len()
	lastNum := fontNum
	buf.WriteString("xref\n0 " + itoa(lastNum+1) + "\n")
	buf.WriteString("0000000000 65535 f \n")
	for i := 1; i <= lastNum; i++ {
		buf.WriteString(pad10(offsets[i]) + " 00000 n \n")
	}
	buf.WriteString("trailer\n<< /Size " + itoa(lastNum+1) + " /Root 1 0 R >>\nstartxref\n" +
		itoa(xrefStart) + "\n%%EOF\n")

	return []byte(buf.String())
}

func textStream(lines ...string) string {
	var b strings.Builder
	for _, line := range lines {
		b.WriteString("BT /F1 24 Tf 72 700 Td (" + line + ") Tj ET\n")
	}
	return b.String()
}

func storeTestPDF(t *testing.T, root, documentID string, content []byte) {
	t.Helper()
	dir := filepath.Join(root, "documents", documentID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create document dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "original.pdf"), content, 0o644); err != nil {
		t.Fatalf("write original.pdf: %v", err)
	}
}

func TestExtractMultiPage(t *testing.T) {
	root := t.TempDir()
	storeTestPDF(t, root, "doc-abc123", buildTestPDF(t, []string{
		textStream("Operating Systems PYQs"),
		textStream("Q1. What is a process?", "Q2. Define deadlock."),
	}))

	svc, err := NewService(root)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	got, err := svc.Extract(context.Background(), "doc-abc123")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if got.DocumentID != "doc-abc123" {
		t.Errorf("DocumentID = %q, want %q", got.DocumentID, "doc-abc123")
	}
	if got.PageCount != 2 {
		t.Fatalf("PageCount = %d, want 2", got.PageCount)
	}
	if len(got.Pages) != 2 {
		t.Fatalf("len(Pages) = %d, want 2", len(got.Pages))
	}

	want := []struct {
		number int
		text   string
	}{
		{1, "Operating Systems PYQs"},
		{2, "Q1. What is a process?\nQ2. Define deadlock."},
	}
	for i, w := range want {
		page := got.Pages[i]
		if page.Number != w.number {
			t.Errorf("Pages[%d].Number = %d, want %d", i, page.Number, w.number)
		}
		if !strings.Contains(page.Text, w.text) {
			t.Errorf("Pages[%d].Text = %q, want to contain %q", i, page.Text, w.text)
		}
		if page.Error != "" {
			t.Errorf("Pages[%d].Error = %q, want empty", i, page.Error)
		}
		if page.Images == nil || len(page.Images) != 0 {
			t.Errorf("Pages[%d].Images = %#v, want empty non-nil slice", i, page.Images)
		}
	}
}

func TestExtractRecordsErrorPerPage(t *testing.T) {
	root := t.TempDir()
	storeTestPDF(t, root, "doc-broken", buildTestPDF(t, []string{
		textStream("First page is fine"),
		"BT Tj ET", // Tj without a text operand: fails only this page
		textStream("Third page is fine"),
	}))

	svc, err := NewService(root)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	got, err := svc.Extract(context.Background(), "doc-broken")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if got.PageCount != 3 {
		t.Fatalf("PageCount = %d, want 3", got.PageCount)
	}
	for _, i := range []int{0, 2} {
		if got.Pages[i].Error != "" {
			t.Errorf("Pages[%d].Error = %q, want empty", i, got.Pages[i].Error)
		}
	}
	if !strings.Contains(got.Pages[0].Text, "First page is fine") {
		t.Errorf("Pages[0].Text = %q, want to contain first page text", got.Pages[0].Text)
	}
	if !strings.Contains(got.Pages[2].Text, "Third page is fine") {
		t.Errorf("Pages[2].Text = %q, want to contain third page text", got.Pages[2].Text)
	}
	if got.Pages[1].Error == "" {
		t.Error("Pages[1].Error is empty, want a recorded per-page error")
	}
}

func TestExtractEmptyPage(t *testing.T) {
	root := t.TempDir()
	storeTestPDF(t, root, "doc-empty", buildTestPDF(t, []string{
		textStream("Only this page has text"),
		"", // blank page
	}))

	svc, _ := NewService(root)
	got, err := svc.Extract(context.Background(), "doc-empty")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if got.Pages[1].Number != 2 {
		t.Errorf("Pages[1].Number = %d, want 2", got.Pages[1].Number)
	}
	if got.Pages[1].Text != "" {
		t.Errorf("Pages[1].Text = %q, want empty", got.Pages[1].Text)
	}
	if got.Pages[1].Error != "" {
		t.Errorf("Pages[1].Error = %q, want empty (blank pages are valid)", got.Pages[1].Error)
	}
}

func TestExtractMissingDocument(t *testing.T) {
	svc, err := NewService(t.TempDir())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if _, err := svc.Extract(context.Background(), "does-not-exist"); err == nil {
		t.Fatal("Extract for missing file succeeded, want error")
	}
}

func TestExtractRejectsUnsafeIDs(t *testing.T) {
	svc, _ := NewService(t.TempDir())
	for _, id := range []string{"", "../escape", "a/b", `a\b`, ".."} {
		if _, err := svc.Extract(context.Background(), id); err == nil {
			t.Errorf("Extract(%q) succeeded, want unsafe id error", id)
		}
	}
}

func TestExtractInvalidPDF(t *testing.T) {
	root := t.TempDir()
	storeTestPDF(t, root, "doc-garbage", []byte("%PDF-1.4\ngarbage without xref\n"))

	svc, _ := NewService(root)
	if _, err := svc.Extract(context.Background(), "doc-garbage"); err == nil {
		t.Fatal("Extract of invalid PDF succeeded, want error")
	}
}

// helpers

func itoa(n int) string {
	return strconv.Itoa(n)
}

func pad10(n int) string {
	s := strconv.Itoa(n)
	for len(s) < 10 {
		s = "0" + s
	}
	return s
}
