package questions

import (
	"context"
	"errors"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
)

// fakeQuestionRepo is an in-memory Repository for service tests.
type fakeQuestionRepo struct {
	items    map[string]Question
	last     Filter
	listErr  error
	getErr   error
	inserted []Question
}

func newFakeQuestionRepo() *fakeQuestionRepo {
	return &fakeQuestionRepo{items: make(map[string]Question)}
}

func (f *fakeQuestionRepo) List(_ context.Context, fl Filter) ([]Question, error) {
	f.last = fl
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]Question, 0, len(f.items))
	for _, q := range f.items {
		out = append(out, q)
	}
	return out, nil
}

func (f *fakeQuestionRepo) GetByID(_ context.Context, id string) (Question, error) {
	if f.getErr != nil {
		return Question{}, f.getErr
	}
	q, ok := f.items[id]
	if !ok {
		return Question{}, apperr.ErrNotFound
	}
	return q, nil
}

func (f *fakeQuestionRepo) Insert(_ context.Context, q Question) error {
	f.inserted = append(f.inserted, q)
	f.items[q.ID] = q
	return nil
}

func TestNewServiceNilSafe(t *testing.T) {
	if NewService(nil) == nil {
		t.Fatalf("NewService(nil) returned nil")
	}
	if NewService(newFakeQuestionRepo()) == nil {
		t.Fatalf("NewService(repo) returned nil")
	}
}

func TestParseYear(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    *int
		wantErr bool
	}{
		{"empty yields nil", "", nil, false},
		{"valid", "2020", intVal(2020), false},
		{"lower bound", "1900", intVal(1900), false},
		{"upper bound", "2100", intVal(2100), false},
		{"below range", "1899", nil, true},
		{"above range", "2101", nil, true},
		{"non numeric", "twenty", nil, true},
		{"float string", "2020.5", nil, true},
		{"whitespace", " 2020", nil, true},
		{"negative", "-5", nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseYear(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseYear(%q) succeeded, want error", tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseYear(%q): %v", tc.raw, err)
			}
			if tc.want == nil {
				if got != nil {
					t.Fatalf("ParseYear(%q) = %d, want nil", tc.raw, *got)
				}
				return
			}
			if got == nil || *got != *tc.want {
				t.Fatalf("ParseYear(%q) = %v, want %d", tc.raw, got, *tc.want)
			}
		})
	}
}

func intVal(n int) *int { return &n }

func TestServiceListClamp(t *testing.T) {
	tests := []struct {
		name       string
		in         Filter
		wantLimit  int
		wantOffset int
	}{
		{"zero defaults", Filter{}, defaultLimit, 0},
		{"negative limit defaults", Filter{Limit: -1}, defaultLimit, 0},
		{"over max clamps", Filter{Limit: 1000}, maxLimit, 0},
		{"exact max passes", Filter{Limit: maxLimit}, maxLimit, 0},
		{"negative offset clamps", Filter{Offset: -4}, defaultLimit, 0},
		{"filters preserved", Filter{DocumentID: "d1", TopicID: "t1", Subject: "Math", Limit: 5, Offset: 2}, 5, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeQuestionRepo()
			svc := NewService(repo)
			if _, err := svc.List(context.Background(), tc.in); err != nil {
				t.Fatalf("List: %v", err)
			}
			if repo.last.Limit != tc.wantLimit {
				t.Fatalf("Limit = %d, want %d", repo.last.Limit, tc.wantLimit)
			}
			if repo.last.Offset != tc.wantOffset {
				t.Fatalf("Offset = %d, want %d", repo.last.Offset, tc.wantOffset)
			}
			if tc.in.DocumentID != "" && repo.last.DocumentID != tc.in.DocumentID {
				t.Fatalf("DocumentID = %q, want %q", repo.last.DocumentID, tc.in.DocumentID)
			}
			if tc.in.TopicID != "" && repo.last.TopicID != tc.in.TopicID {
				t.Fatalf("TopicID = %q, want %q", repo.last.TopicID, tc.in.TopicID)
			}
		})
	}
}

func TestServiceListErrorPassthrough(t *testing.T) {
	repo := newFakeQuestionRepo()
	repo.listErr = errors.New("db down")
	if _, err := NewService(repo).List(context.Background(), Filter{}); err == nil {
		t.Fatalf("expected list error")
	}
}

func TestServiceGet(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo := newFakeQuestionRepo()
		repo.items["q1"] = Question{ID: "q1", DocumentID: "d1"}
		got, err := NewService(repo).Get(context.Background(), "q1")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.ID != "q1" || got.DocumentID != "d1" {
			t.Fatalf("got = %+v, want q1/d1", got)
		}
	})

	t.Run("missing maps to not found", func(t *testing.T) {
		svc := NewService(newFakeQuestionRepo())
		if _, err := svc.Get(context.Background(), "nope"); !errors.Is(err, apperr.ErrNotFound) {
			t.Fatalf("Get err = %v, want ErrNotFound", err)
		}
	})

	t.Run("repository error passes through unwrapped", func(t *testing.T) {
		repo := newFakeQuestionRepo()
		repo.getErr = errors.New("db down")
		if _, err := NewService(repo).Get(context.Background(), "q1"); err == nil || errors.Is(err, apperr.ErrNotFound) {
			t.Fatalf("Get err = %v, want non-NotFound error", err)
		}
	})
}
