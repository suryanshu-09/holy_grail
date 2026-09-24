package documents

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
	"github.com/suryanshu-09/holy_grail/internal/auth"
)

// fakeDocRepo is an in-memory Repository for service tests.
type fakeDocRepo struct {
	docs      map[string]Document
	listErr   error
	getErr    error
	createErr error
	last      Filter
	statuses  map[string]string
}

func newFakeDocRepo() *fakeDocRepo {
	return &fakeDocRepo{docs: make(map[string]Document), statuses: make(map[string]string)}
}

func (f *fakeDocRepo) List(_ context.Context, fl Filter) ([]Document, error) {
	f.last = fl
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]Document, 0, len(f.docs))
	for _, d := range f.docs {
		out = append(out, d)
	}
	return out, nil
}

func (f *fakeDocRepo) GetByID(_ context.Context, id string) (Document, error) {
	if f.getErr != nil {
		return Document{}, f.getErr
	}
	d, ok := f.docs[id]
	if !ok {
		return Document{}, apperr.ErrNotFound
	}
	return d, nil
}

func (f *fakeDocRepo) Create(_ context.Context, d *Document) error {
	if f.createErr != nil {
		return f.createErr
	}
	if d == nil {
		return errors.New("nil document")
	}
	f.docs[d.ID] = *d
	return nil
}

func (f *fakeDocRepo) UpdateStatus(_ context.Context, id string, status string) error {
	if _, ok := f.docs[id]; !ok {
		return apperr.ErrNotFound
	}
	f.statuses[id] = status
	return nil
}

// fakeDocStore is an in-memory Storage for service tests.
type fakeDocStore struct {
	saved     map[string][]byte
	saveErr   error
	removed   []string
	removeErr error
}

func newFakeDocStore() *fakeDocStore {
	return &fakeDocStore{saved: make(map[string][]byte)}
}

func (f *fakeDocStore) SaveDocument(_ context.Context, id string, r io.Reader) (string, error) {
	if f.saveErr != nil {
		return "", f.saveErr
	}
	body, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	f.saved[id] = body
	return "documents/" + id + "/original.pdf", nil
}

func (f *fakeDocStore) RemoveDocument(_ context.Context, id string) error {
	f.removed = append(f.removed, id)
	return f.removeErr
}

func ctxWithUserID(id string) context.Context {
	if id == "" {
		return context.Background()
	}
	return auth.WithUser(context.Background(), auth.User{ID: id})
}

func TestServiceListClampAndScope(t *testing.T) {
	tests := []struct {
		name       string
		in         Filter
		wantLimit  int
		wantOffset int
	}{
		{"zero defaults", Filter{}, defaultLimit, 0},
		{"negative limit defaults", Filter{Limit: -5}, defaultLimit, 0},
		{"over max clamps", Filter{Limit: 500}, maxLimit, 0},
		{"exact max passes", Filter{Limit: maxLimit}, maxLimit, 0},
		{"negative offset clamps", Filter{Offset: -3}, defaultLimit, 0},
		{"offset passes", Filter{Offset: 7, Limit: 10}, 10, 7},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeDocRepo()
			svc := NewService(repo, newFakeDocStore())
			if _, err := svc.List(context.Background(), tc.in); err != nil {
				t.Fatalf("List: %v", err)
			}
			if repo.last.Limit != tc.wantLimit {
				t.Fatalf("Limit = %d, want %d", repo.last.Limit, tc.wantLimit)
			}
			if repo.last.Offset != tc.wantOffset {
				t.Fatalf("Offset = %d, want %d", repo.last.Offset, tc.wantOffset)
			}
		})
	}
}

func TestServiceListUserScope(t *testing.T) {
	repo := newFakeDocRepo()
	svc := NewService(repo, newFakeDocStore())

	if _, err := svc.List(ctxWithUserID("u1"), Filter{}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if repo.last.UserID != "u1" {
		t.Fatalf("UserID = %q, want %q", repo.last.UserID, "u1")
	}

	if _, err := svc.List(context.Background(), Filter{}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if repo.last.UserID != "" {
		t.Fatalf("anonymous UserID = %q, want empty", repo.last.UserID)
	}
}

func TestServiceGetOwnership(t *testing.T) {
	other := "someone-else"
	owned := Document{ID: "d1", Filename: "a.pdf", Status: StatusUploaded, UserID: strPtrFor("u1")}
	foreign := Document{ID: "d2", Filename: "b.pdf", Status: StatusUploaded, UserID: &other}
	legacy := Document{ID: "d3", Filename: "c.pdf", Status: StatusUploaded}

	newSvc := func() (*Service, *fakeDocRepo) {
		repo := newFakeDocRepo()
		repo.docs["d1"] = owned
		repo.docs["d2"] = foreign
		repo.docs["d3"] = legacy
		return NewService(repo, newFakeDocStore()), repo
	}

	t.Run("owner reads own", func(t *testing.T) {
		svc, _ := newSvc()
		got, err := svc.Get(ctxWithUserID("u1"), "d1")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.ID != "d1" {
			t.Fatalf("ID = %q, want d1", got.ID)
		}
	})

	t.Run("other owner hidden as not found", func(t *testing.T) {
		svc, _ := newSvc()
		if _, err := svc.Get(ctxWithUserID("u1"), "d2"); !errors.Is(err, apperr.ErrNotFound) {
			t.Fatalf("Get = %v, want ErrNotFound", err)
		}
	})

	t.Run("foreign owner reads own", func(t *testing.T) {
		svc, _ := newSvc()
		if _, err := svc.Get(ctxWithUserID("someone-else"), "d2"); err != nil {
			t.Fatalf("Get: %v", err)
		}
	})

	t.Run("legacy unowned visible anonymously", func(t *testing.T) {
		svc, _ := newSvc()
		if _, err := svc.Get(context.Background(), "d3"); err != nil {
			t.Fatalf("Get: %v", err)
		}
	})

	t.Run("missing maps to not found", func(t *testing.T) {
		svc, _ := newSvc()
		if _, err := svc.Get(context.Background(), "nope"); !errors.Is(err, apperr.ErrNotFound) {
			t.Fatalf("Get = %v, want ErrNotFound", err)
		}
	})

	t.Run("repository error passes through", func(t *testing.T) {
		repo := newFakeDocRepo()
		repo.getErr = errors.New("boom")
		svc := NewService(repo, newFakeDocStore())
		if _, err := svc.Get(context.Background(), "d1"); err == nil || errors.Is(err, apperr.ErrNotFound) {
			t.Fatalf("Get = %v, want non-NotFound error", err)
		}
	})
}

func strPtrFor(s string) *string { return &s }

func TestServiceUploadValidation(t *testing.T) {
	pdf := []byte("%PDF-1.7 fake content")

	t.Run("nil reader rejected without touching repo", func(t *testing.T) {
		// Nil service dependencies must not panic: validation runs first.
		svc := NewService(nil, nil)
		if _, err := svc.Upload(context.Background(), "a.pdf", nil); !errors.Is(err, apperr.ErrInvalidUpload) {
			t.Fatalf("Upload = %v, want ErrInvalidUpload", err)
		}
	})

	tests := []struct {
		name     string
		filename string
		body     []byte
		wantErr  bool
	}{
		{"valid pdf", "paper.pdf", pdf, false},
		{"non pdf rejected", "paper.pdf", []byte("hello world"), true},
		{"empty rejected", "paper.pdf", []byte{}, true},
		{"truncated magic rejected", "paper.pdf", []byte("%PD"), true},
		{"wrong extension but pdf magic accepted", "paper.txt", pdf, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(newFakeDocRepo(), newFakeDocStore())
			var r io.Reader
			if tc.body != nil {
				r = bytes.NewReader(tc.body)
			}
			doc, err := svc.Upload(context.Background(), tc.filename, r)
			if tc.wantErr {
				if !errors.Is(err, apperr.ErrInvalidUpload) {
					t.Fatalf("Upload err = %v, want ErrInvalidUpload", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Upload: %v", err)
			}
			if doc.Status != StatusUploaded {
				t.Fatalf("Status = %q, want %q", doc.Status, StatusUploaded)
			}
			if !IsValidID(doc.ID) {
				t.Fatalf("ID %q is not a valid document ID", doc.ID)
			}
			if doc.StoragePath == nil || !strings.HasPrefix(*doc.StoragePath, "documents/") {
				t.Fatalf("StoragePath = %v, want documents/ layout", doc.StoragePath)
			}
		})
	}
}

func TestServiceUploadOwnershipAndSanitization(t *testing.T) {
	pdf := []byte("%PDF-1.7 fake")

	t.Run("authenticated upload attributed", func(t *testing.T) {
		repo := newFakeDocRepo()
		svc := NewService(repo, newFakeDocStore())
		doc, err := svc.Upload(ctxWithUserID("u9"), "paper.pdf", bytes.NewReader(pdf))
		if err != nil {
			t.Fatalf("Upload: %v", err)
		}
		if doc.UserID == nil || *doc.UserID != "u9" {
			t.Fatalf("UserID = %v, want u9", doc.UserID)
		}
		if stored := repo.docs[doc.ID]; stored.UserID == nil {
			t.Fatalf("persisted document lost ownership")
		}
	})

	t.Run("anonymous upload stays unowned", func(t *testing.T) {
		svc := NewService(newFakeDocRepo(), newFakeDocStore())
		doc, err := svc.Upload(context.Background(), "paper.pdf", bytes.NewReader(pdf))
		if err != nil {
			t.Fatalf("Upload: %v", err)
		}
		if doc.UserID != nil {
			t.Fatalf("UserID = %v, want nil", doc.UserID)
		}
	})

	t.Run("traversal filename sanitized", func(t *testing.T) {
		svc := NewService(newFakeDocRepo(), newFakeDocStore())
		doc, err := svc.Upload(context.Background(), "../../evil.pdf", bytes.NewReader(pdf))
		if err != nil {
			t.Fatalf("Upload: %v", err)
		}
		if doc.Filename != "evil.pdf" {
			t.Fatalf("Filename = %q, want evil.pdf", doc.Filename)
		}
		if doc.OriginalFilename != "../../evil.pdf" {
			t.Fatalf("OriginalFilename = %q, want raw input preserved", doc.OriginalFilename)
		}
	})
}

func TestServiceUploadFailures(t *testing.T) {
	pdf := []byte("%PDF-1.7 fake")

	t.Run("storage error aborts", func(t *testing.T) {
		store := newFakeDocStore()
		store.saveErr = errors.New("disk full")
		svc := NewService(newFakeDocRepo(), store)
		if _, err := svc.Upload(context.Background(), "a.pdf", bytes.NewReader(pdf)); err == nil {
			t.Fatalf("expected storage error")
		}
	})

	t.Run("repo error cleans up stored file", func(t *testing.T) {
		repo := newFakeDocRepo()
		repo.createErr = errors.New("db down")
		store := newFakeDocStore()
		svc := NewService(repo, store)
		if _, err := svc.Upload(context.Background(), "a.pdf", bytes.NewReader(pdf)); err == nil {
			t.Fatalf("expected repo error")
		}
		if len(store.removed) != 1 {
			t.Fatalf("RemoveDocument calls = %d, want 1 (no orphaned file)", len(store.removed))
		}
		if len(store.saved) != 1 {
			t.Fatalf("saved files = %d, want 1 before cleanup", len(store.saved))
		}
	})
}

func TestDocumentStatusConstants(t *testing.T) {
	for _, s := range []string{StatusUploaded, StatusExtracted, StatusFailed} {
		if s == "" {
			t.Fatalf("status constant is empty")
		}
	}
	if StatusUploaded == StatusExtracted || StatusUploaded == StatusFailed || StatusExtracted == StatusFailed {
		t.Fatalf("status constants must be distinct")
	}
}
