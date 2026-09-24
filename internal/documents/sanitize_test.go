package documents

import (
	"strings"
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple", "paper.pdf", "paper.pdf"},
		{"keeps spaces and dashes", "my paper - final.pdf", "my paper - final.pdf"},
		{"keeps underscores", "exam_2023_v2.pdf", "exam_2023_v2.pdf"},
		{"unix traversal", "../../etc/passwd", "passwd"},
		{"nested traversal", "a/b/../../c.pdf", "c.pdf"},
		{"absolute path", "/etc/passwd", "passwd"},
		{"windows traversal", `..\..\secret.pdf`, "secret.pdf"},
		{"windows absolute", `C:\docs\paper.pdf`, "paper.pdf"},
		{"special chars replaced", "my paper (final)!.pdf", "my paper _final__.pdf"},
		{"semicolon replaced", "a;b.pdf", "a_b.pdf"},
		{"unicode replaced", "café.pdf", "caf_.pdf"},
		{"empty falls back", "", "document.pdf"},
		{"dots only falls back", "...", "document.pdf"},
		{"leading dots stripped", "...paper.pdf", "paper.pdf"},
		{"surrounding spaces trimmed", "  paper.pdf  ", "paper.pdf"},
		{"separators only", "///", "_"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SanitizeFilename(tc.input); got != tc.want {
				t.Fatalf("SanitizeFilename(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestSanitizeFilenameNeverEscapes(t *testing.T) {
	inputs := []string{
		"../../etc/passwd",
		`..\..\windows\system32\drivers\etc\hosts`,
		"/absolute/path.pdf",
		"....//....//evil.pdf",
		"..",
		".",
		"",
		"normal.pdf\x00.pdf",
	}
	for _, in := range inputs {
		got := SanitizeFilename(in)
		if got == "" {
			t.Fatalf("SanitizeFilename(%q) returned empty string", in)
		}
		if strings.Contains(got, "/") || strings.Contains(got, "\\") {
			t.Fatalf("SanitizeFilename(%q) = %q contains a separator", in, got)
		}
		if strings.Contains(got, "..") {
			t.Fatalf("SanitizeFilename(%q) = %q contains dot-dot", in, got)
		}
	}
}

func TestSanitizeFilenameTruncates(t *testing.T) {
	long := strings.Repeat("a", 300) + ".pdf"
	got := SanitizeFilename(long)
	if len(got) != maxFilenameLen {
		t.Fatalf("truncated length = %d, want %d", len(got), maxFilenameLen)
	}
	if !strings.HasSuffix(got, ".pdf") {
		t.Fatalf("truncated name %q lost its extension", got)
	}

	// Extension longer than the cap still yields a bounded name.
	noExt := strings.Repeat("b", 300)
	got = SanitizeFilename(noExt)
	if len(got) != maxFilenameLen {
		t.Fatalf("truncated length = %d, want %d", len(got), maxFilenameLen)
	}
}
