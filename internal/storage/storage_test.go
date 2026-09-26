package storage

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestLocalPutGetDelete(t *testing.T) {
	ctx := context.Background()
	s := testStore(t, t.TempDir())

	if err := s.Put(ctx, "a/b.txt", strings.NewReader("hello")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	rc, err := s.Get(ctx, "a/b.txt")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "hello" {
		t.Fatalf("Get = %q, want hello", got)
	}
	if err := s.Delete(ctx, "a/b.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := s.Delete(ctx, "a/b.txt"); err != nil {
		t.Fatalf("Delete missing should be noop: %v", err)
	}
	if _, err := s.Get(ctx, "a/b.txt"); err == nil {
		t.Fatalf("Get after delete succeeded, want error")
	}
}

func TestLocalPutRejectsUnsafeKeys(t *testing.T) {
	s := testStore(t, t.TempDir())
	for _, k := range []string{"", "/abs", "../x", "a/../b", `a\b`, "a//b", "a/./b"} {
		if err := s.Put(context.Background(), k, strings.NewReader("x")); err == nil {
			t.Fatalf("Put(%q) succeeded, want error", k)
		}
		if _, err := s.Get(context.Background(), k); err == nil {
			t.Fatalf("Get(%q) succeeded, want error", k)
		}
		if err := s.Delete(context.Background(), k); err == nil {
			t.Fatalf("Delete(%q) succeeded, want error", k)
		}
	}
}

func TestLocalStorageAlias(t *testing.T) {
	s, err := NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}
	var _ Storage = s
	var ls *LocalStorage = s
	if ls == nil {
		t.Fatalf("alias conversion returned nil")
	}
	key, err := s.SaveDocument(context.Background(), "alias-1", strings.NewReader("data"))
	if err != nil {
		t.Fatalf("SaveDocument: %v", err)
	}
	if key != "documents/alias-1/original.pdf" {
		t.Fatalf("key = %q", key)
	}
}

func TestS3RequiresConfig(t *testing.T) {
	if _, err := NewS3Storage(S3Config{}); err == nil {
		t.Fatalf("expected bucket error")
	}
	if _, err := NewS3Storage(S3Config{Bucket: "b"}); err == nil {
		t.Fatalf("expected credentials error")
	}
}

func TestS3DocumentKeyRejectsUnsafe(t *testing.T) {
	s, err := NewS3Storage(S3Config{Bucket: "b", AccessKey: "a", SecretKey: "s"})
	if err != nil {
		t.Fatalf("NewS3Storage: %v", err)
	}
	if _, err := s.SaveDocument(context.Background(), "../evil", strings.NewReader("x")); err == nil {
		t.Fatalf("SaveDocument with unsafe id succeeded")
	}
	if err := s.RemoveDocument(context.Background(), "a/b"); err == nil {
		t.Fatalf("RemoveDocument with unsafe id succeeded")
	}
}

// fakeS3 is a minimal in-memory S3-compatible server.
type fakeS3 struct {
	mu   sync.Mutex
	objs map[string][]byte
}

func (f *fakeS3) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "missing auth", http.StatusForbidden)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			http.Error(w, "bad auth", http.StatusForbidden)
			return
		}
		// Path is /<bucket>/<key...>
		p := strings.TrimPrefix(r.URL.Path, "/")
		parts := strings.SplitN(p, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		key := parts[1]
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.Method {
		case http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			f.objs[key] = b
		case http.MethodGet:
			b, ok := f.objs[key]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Write(b)
		case http.MethodDelete:
			delete(f.objs, key)
		default:
			http.Error(w, "method", http.StatusMethodNotAllowed)
		}
	}
}

func newTestS3(t *testing.T) (*S3Storage, *fakeS3) {
	t.Helper()
	f := &fakeS3{objs: map[string][]byte{}}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	s, err := NewS3Storage(S3Config{
		Endpoint:  srv.URL,
		Bucket:    "test-bucket",
		Region:    "us-east-1",
		AccessKey: "testkey",
		SecretKey: "testsecret",
	})
	if err != nil {
		t.Fatalf("NewS3Storage: %v", err)
	}
	return s, f
}

func TestS3PutGetDeleteRoundtrip(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestS3(t)
	if err := s.Put(ctx, "docs/a.txt", strings.NewReader("s3-data")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	rc, err := s.Get(ctx, "docs/a.txt")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "s3-data" {
		t.Fatalf("Get = %q", got)
	}
	if err := s.Delete(ctx, "docs/a.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, "docs/a.txt"); err == nil {
		t.Fatalf("Get after delete succeeded")
	}
	if err := s.Delete(ctx, "docs/a.txt"); err != nil {
		t.Fatalf("Delete missing should be noop: %v", err)
	}
}

func TestS3SaveRemoveDocumentCompat(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestS3(t)
	key, err := s.SaveDocument(ctx, "doc-1", strings.NewReader("pdf-bytes"))
	if err != nil {
		t.Fatalf("SaveDocument: %v", err)
	}
	if key != "documents/doc-1/original.pdf" {
		t.Fatalf("key = %q", key)
	}
	rc, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "pdf-bytes" {
		t.Fatalf("content = %q", got)
	}
	if err := s.RemoveDocument(ctx, "doc-1"); err != nil {
		t.Fatalf("RemoveDocument: %v", err)
	}
	if _, err := s.Get(ctx, key); err == nil {
		t.Fatalf("object still exists after remove")
	}
}

func TestS3RejectsUnsafeKeys(t *testing.T) {
	s, _ := newTestS3(t)
	ctx := context.Background()
	if err := s.Put(ctx, "../evil", strings.NewReader("x")); err == nil {
		t.Fatalf("Put unsafe succeeded")
	}
	if _, err := s.Get(ctx, "../evil"); err == nil {
		t.Fatalf("Get unsafe succeeded")
	}
}

func TestNewStorageFromEnv(t *testing.T) {
	t.Setenv("STORAGE_BACKEND", "")
	s, err := NewStorageFromEnv(t.TempDir())
	if err != nil {
		t.Fatalf("local factory: %v", err)
	}
	if _, ok := s.(*LocalStore); !ok {
		t.Fatalf("want *LocalStore, got %T", s)
	}

	t.Setenv("STORAGE_BACKEND", "s3")
	t.Setenv("S3_ENDPOINT", "http://localhost:9000")
	t.Setenv("S3_BUCKET", "b")
	t.Setenv("S3_REGION", "us-east-1")
	t.Setenv("AWS_ACCESS_KEY_ID", "k")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "s")
	s2, err := NewStorageFromEnv(t.TempDir())
	if err != nil {
		t.Fatalf("s3 factory: %v", err)
	}
	if _, ok := s2.(*S3Storage); !ok {
		t.Fatalf("want *S3Storage, got %T", s2)
	}
}
