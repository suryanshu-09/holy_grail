package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testStore(t *testing.T, root string) *LocalStore {
	t.Helper()
	s, err := NewLocalStore(root)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	if s == nil {
		t.Fatalf("NewLocalStore returned nil")
	}
	return s
}

func TestNewLocalStoreResolvesAbsolute(t *testing.T) {
	s := testStore(t, t.TempDir())
	if !filepath.IsAbs(s.root) {
		t.Fatalf("root = %q, want absolute path", s.root)
	}
}

func TestSaveAndRemoveRoundtrip(t *testing.T) {
	ctx := context.Background()
	s := testStore(t, t.TempDir())

	content := "fake pdf bytes"
	rel, err := s.SaveDocument(ctx, "doc-1", strings.NewReader(content))
	if err != nil {
		t.Fatalf("SaveDocument: %v", err)
	}
	if want := "documents/doc-1/original.pdf"; rel != want {
		t.Fatalf("rel = %q, want %q", rel, want)
	}

	// Temp files must not leak beside the final object.
	dir := filepath.Join(s.root, "documents", "doc-1")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "original.pdf" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("dir contents = %v, want [original.pdf]", names)
	}

	got, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != content {
		t.Fatalf("stored content = %q, want %q", got, content)
	}

	if err := s.RemoveDocument(ctx, "doc-1"); err != nil {
		t.Fatalf("RemoveDocument: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("document dir still exists after remove: %v", err)
	}
}

func TestSaveDocumentRejectsUnsafeIDs(t *testing.T) {
	s := testStore(t, t.TempDir())
	tests := []struct {
		name string
		id   string
	}{
		{"empty", ""},
		{"dot dot", ".."},
		{"parent traversal", "../evil"},
		{"nested traversal", "a/../../evil"},
		{"slash", "a/b"},
		{"backslash", `a\b`},
		{"absolute", "/etc/passwd"},
		{"embedded dots", "a..b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.SaveDocument(context.Background(), tc.id, strings.NewReader("x")); err == nil {
				t.Fatalf("SaveDocument(%q) succeeded, want traversal rejection", tc.id)
			}
		})
	}
}

func TestRemoveDocumentRejectsUnsafeIDs(t *testing.T) {
	s := testStore(t, t.TempDir())
	for _, id := range []string{"", "..", "../evil", "a/b", `a\b`, "a..b"} {
		if err := s.RemoveDocument(context.Background(), id); err == nil {
			t.Fatalf("RemoveDocument(%q) succeeded, want traversal rejection", id)
		}
	}
}

func TestRemoveNonexistentIsNoop(t *testing.T) {
	s := testStore(t, t.TempDir())
	if err := s.RemoveDocument(context.Background(), "never-saved"); err != nil {
		t.Fatalf("RemoveDocument of missing id: %v", err)
	}
}

func TestSaveCreatesMissingDirectories(t *testing.T) {
	// A root whose parents do not exist yet must be created on demand.
	root := filepath.Join(t.TempDir(), "nope", "nested", "data")
	s := testStore(t, root)
	if _, err := s.SaveDocument(context.Background(), "doc-9", strings.NewReader("bytes")); err != nil {
		t.Fatalf("SaveDocument with missing parents: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "documents", "doc-9", "original.pdf")); err != nil {
		t.Fatalf("expected file after save: %v", err)
	}
}

func TestSaveOverwritesExisting(t *testing.T) {
	ctx := context.Background()
	s := testStore(t, t.TempDir())
	if _, err := s.SaveDocument(ctx, "doc-2", strings.NewReader("v1")); err != nil {
		t.Fatalf("SaveDocument: %v", err)
	}
	rel, err := s.SaveDocument(ctx, "doc-2", strings.NewReader("v2"))
	if err != nil {
		t.Fatalf("re-save: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "v2" {
		t.Fatalf("content = %q, want v2", got)
	}
}

func TestWithinRoot(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "data", "root")
	tests := []struct {
		name   string
		target string
		want   bool
	}{
		{"child", filepath.Join(root, "documents", "d1", "original.pdf"), true},
		{"root itself", root, true},
		{"parent", filepath.Dir(root), false},
		{"sibling traversal", filepath.Join(root, "..", "other"), false},
		{"deep traversal", filepath.Join(root, "documents", "..", "..", "evil"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := withinRoot(root, tc.target); got != tc.want {
				t.Fatalf("withinRoot(%q, %q) = %v, want %v", root, tc.target, got, tc.want)
			}
		})
	}
}
