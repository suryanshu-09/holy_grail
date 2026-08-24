package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// documentLayout is the relative layout of an uploaded document inside the
// data root: documents/<document-id>/original.pdf.
const documentLayout = "documents"

// LocalStore persists uploads on the local filesystem under a single root
// directory (e.g. ./data).
type LocalStore struct {
	root string
}

// NewLocalStore resolves root to an absolute path and returns a store rooted
// there.
func NewLocalStore(root string) (*LocalStore, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve root: %w", err)
	}
	return &LocalStore{root: abs}, nil
}

func safeSegment(id string) bool {
	return id != "" && !strings.ContainsAny(id, `/\`) && !strings.Contains(id, "..")
}

// SaveDocument streams content into <root>/documents/<id>/original.pdf using a
// temp file plus rename so partial writes are never visible at the final path.
// It returns the slash-separated path relative to the store root.
func (s *LocalStore) SaveDocument(_ context.Context, id string, r io.Reader) (string, error) {
	if !safeSegment(id) {
		return "", fmt.Errorf("storage: unsafe document id %q", id)
	}
	rel := filepath.Join(documentLayout, id, "original.pdf")
	dest := filepath.Join(s.root, rel)
	if !withinRoot(s.root, dest) {
		return "", fmt.Errorf("storage: resolved path escapes root: %q", dest)
	}

	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("storage: create directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "upload-*")
	if err != nil {
		return "", fmt.Errorf("storage: create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", fmt.Errorf("storage: write file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", fmt.Errorf("storage: sync file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("storage: close file: %w", err)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("storage: finalize file: %w", err)
	}

	return filepath.ToSlash(rel), nil
}

// RemoveDocument deletes the storage directory of a document, e.g. to clean up
// after a failed metadata insert.
func (s *LocalStore) RemoveDocument(_ context.Context, id string) error {
	if !safeSegment(id) {
		return fmt.Errorf("storage: unsafe document id %q", id)
	}
	dir := filepath.Join(s.root, documentLayout, id)
	if !withinRoot(s.root, dir) {
		return fmt.Errorf("storage: resolved path escapes root: %q", dir)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("storage: remove document: %w", err)
	}
	return nil
}

// withinRoot reports whether target is contained in root, guarding against
// path traversal via crafted IDs or configuration values.
func withinRoot(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
