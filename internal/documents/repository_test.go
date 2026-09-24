package documents

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
)

// stubSQLDriver exercises the repository's SQL paths (including the
// pre-migration-009 legacy fallback) without a live Postgres.
type stubSQLDriver struct{}

type stubConn struct{}

type stubRows struct {
	cols []string
	data [][]driver.Value
	pos  int
}

func (r *stubRows) Columns() []string { return r.cols }
func (r *stubRows) Close() error      { return nil }
func (r *stubRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.pos])
	r.pos++
	return nil
}

type stubResult struct{ n int64 }

func (r stubResult) LastInsertId() (int64, error) { return 0, nil }
func (r stubResult) RowsAffected() (int64, error) { return r.n, nil }

var (
	stubMu    sync.Mutex
	stubQuery func(query string, args []driver.Value) (driver.Rows, error)
	stubExec  func(query string, args []driver.Value) (driver.Result, error)
	stubReg   sync.Once
)

func (stubSQLDriver) Open(string) (driver.Conn, error) { return stubConn{}, nil }

func (stubConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("prepare not supported") }
func (stubConn) Close() error                        { return nil }
func (stubConn) Begin() (driver.Tx, error)           { return nil, errors.New("tx not supported") }

func (stubConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	vals := make([]driver.Value, len(args))
	for i, a := range args {
		vals[i] = a.Value
	}
	stubMu.Lock()
	fn := stubQuery
	stubMu.Unlock()
	if fn == nil {
		return nil, errors.New("documents test: no stub query handler")
	}
	return fn(query, vals)
}

func (stubConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	vals := make([]driver.Value, len(args))
	for i, a := range args {
		vals[i] = a.Value
	}
	stubMu.Lock()
	fn := stubExec
	stubMu.Unlock()
	if fn == nil {
		return nil, errors.New("documents test: no stub exec handler")
	}
	return fn(query, vals)
}

func openStubDB(t *testing.T) *sql.DB {
	t.Helper()
	stubReg.Do(func() { sql.Register("documentsstub", stubSQLDriver{}) })
	db, err := sql.Open("documentsstub", "test")
	if err != nil {
		t.Fatalf("sql.Open stub: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		stubMu.Lock()
		stubQuery = nil
		stubExec = nil
		stubMu.Unlock()
	})
	return db
}

func setStub(t *testing.T,
	query func(string, []driver.Value) (driver.Rows, error),
	exec func(string, []driver.Value) (driver.Result, error)) {
	t.Helper()
	stubMu.Lock()
	stubQuery = query
	stubExec = exec
	stubMu.Unlock()
}

var errMissingOwnerColumn = errors.New(`pq: column "user_id" does not exist`)

func ownerRow(id string, owner string) []driver.Value {
	ts := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	var o driver.Value
	if owner != "" {
		o = owner
	}
	return []driver.Value{id, "paper.pdf", "paper.pdf", "documents/" + id + "/original.pdf", nil, nil, StatusUploaded, o, ts, ts}
}

func legacyRow(id string) []driver.Value {
	ts := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	return []driver.Value{id, "paper.pdf", "paper.pdf", "documents/" + id + "/original.pdf", nil, nil, StatusUploaded, ts, ts}
}

func TestNewRepository(t *testing.T) {
	if NewRepository(nil) == nil {
		t.Fatalf("NewRepository(nil) returned nil")
	}
	if NewRepository(openStubDB(t)) == nil {
		t.Fatalf("NewRepository(db) returned nil")
	}
}

func TestIsMissingOwnerColumn(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"pq missing column", errMissingOwnerColumn, true},
		{"uppercase", errors.New(`PQ: COLUMN "USER_ID" DOES NOT EXIST`), true},
		{"wrapped", errors.New("documents: list: " + errMissingOwnerColumn.Error()), true},
		{"user id only", errors.New("user_id"), false},
		{"does not exist only", errors.New("column does not exist"), false},
		{"unrelated", errors.New("connection refused"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isMissingOwnerColumn(tc.err); got != tc.want {
				t.Fatalf("isMissingOwnerColumn(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestRepositoryListWithOwner(t *testing.T) {
	db := openStubDB(t)
	var seenQuery string
	setStub(t, func(query string, _ []driver.Value) (driver.Rows, error) {
		seenQuery = query
		return &stubRows{
			cols: []string{"id", "filename", "original_filename", "storage_path", "subject", "year", "status", "user_id", "created_at", "updated_at"},
			data: [][]driver.Value{ownerRow("d1", "u1")},
		}, nil
	}, nil)

	repo := NewRepository(db)
	docs, err := repo.List(context.Background(), Filter{Subject: "Math", UserID: "u1", Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("len(docs) = %d, want 1", len(docs))
	}
	if docs[0].ID != "d1" {
		t.Fatalf("ID = %q, want d1", docs[0].ID)
	}
	if docs[0].UserID == nil || *docs[0].UserID != "u1" {
		t.Fatalf("UserID = %v, want u1", docs[0].UserID)
	}
	for _, want := range []string{"subject = $", "(user_id = $", "ORDER BY created_at DESC"} {
		if !strings.Contains(seenQuery, want) {
			t.Fatalf("query %q missing %q", seenQuery, want)
		}
	}
}

func TestRepositoryListLegacyFallback(t *testing.T) {
	db := openStubDB(t)
	setStub(t, func(query string, _ []driver.Value) (driver.Rows, error) {
		if strings.Contains(query, "user_id") {
			return nil, errMissingOwnerColumn
		}
		return &stubRows{
			cols: []string{"id", "filename", "original_filename", "storage_path", "subject", "year", "status", "created_at", "updated_at"},
			data: [][]driver.Value{legacyRow("d9")},
		}, nil
	}, nil)

	repo := NewRepository(db)
	docs, err := repo.List(context.Background(), Filter{Limit: 10})
	if err != nil {
		t.Fatalf("List fallback: %v", err)
	}
	if len(docs) != 1 || docs[0].ID != "d9" {
		t.Fatalf("docs = %+v, want one row d9", docs)
	}
	if docs[0].UserID != nil {
		t.Fatalf("legacy row UserID = %v, want nil", docs[0].UserID)
	}
}

func TestRepositoryListQueryError(t *testing.T) {
	db := openStubDB(t)
	setStub(t, func(string, []driver.Value) (driver.Rows, error) {
		return nil, errors.New("connection refused")
	}, nil)

	if _, err := NewRepository(db).List(context.Background(), Filter{Limit: 5}); err == nil {
		t.Fatalf("expected query error")
	}
}

func TestRepositoryGetByID(t *testing.T) {
	t.Run("found with owner", func(t *testing.T) {
		db := openStubDB(t)
		setStub(t, func(string, []driver.Value) (driver.Rows, error) {
			return &stubRows{
				cols: []string{"id", "filename", "original_filename", "storage_path", "subject", "year", "status", "user_id", "created_at", "updated_at"},
				data: [][]driver.Value{ownerRow("d1", "u1")},
			}, nil
		}, nil)
		got, err := NewRepository(db).GetByID(context.Background(), "d1")
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if got.ID != "d1" || got.UserID == nil || *got.UserID != "u1" {
			t.Fatalf("got = %+v, want d1/u1", got)
		}
	})

	t.Run("not found", func(t *testing.T) {
		db := openStubDB(t)
		setStub(t, func(string, []driver.Value) (driver.Rows, error) {
			return &stubRows{cols: []string{"id"}, data: nil}, nil
		}, nil)
		if _, err := NewRepository(db).GetByID(context.Background(), "missing"); !errors.Is(err, apperr.ErrNotFound) {
			t.Fatalf("GetByID err = %v, want ErrNotFound", err)
		}
	})

	t.Run("legacy fallback", func(t *testing.T) {
		db := openStubDB(t)
		setStub(t, func(query string, _ []driver.Value) (driver.Rows, error) {
			if strings.Contains(query, "user_id") {
				return nil, errMissingOwnerColumn
			}
			return &stubRows{
				cols: []string{"id", "filename", "original_filename", "storage_path", "subject", "year", "status", "created_at", "updated_at"},
				data: [][]driver.Value{legacyRow("d2")},
			}, nil
		}, nil)
		got, err := NewRepository(db).GetByID(context.Background(), "d2")
		if err != nil {
			t.Fatalf("GetByID fallback: %v", err)
		}
		if got.ID != "d2" || got.UserID != nil {
			t.Fatalf("got = %+v, want d2 with nil owner", got)
		}
	})
}

func TestRepositoryCreate(t *testing.T) {
	ts := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)

	t.Run("with owner", func(t *testing.T) {
		db := openStubDB(t)
		setStub(t, func(string, []driver.Value) (driver.Rows, error) {
			return &stubRows{cols: []string{"created_at", "updated_at"}, data: [][]driver.Value{{ts, ts}}}, nil
		}, nil)
		uid := "u1"
		d := &Document{ID: "d1", Filename: "a.pdf", Status: StatusUploaded, UserID: &uid}
		if err := NewRepository(db).Create(context.Background(), d); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if !d.CreatedAt.Equal(ts) || !d.UpdatedAt.Equal(ts) {
			t.Fatalf("timestamps not populated: %+v", d)
		}
	})

	t.Run("legacy fallback", func(t *testing.T) {
		db := openStubDB(t)
		setStub(t, func(query string, _ []driver.Value) (driver.Rows, error) {
			if strings.Contains(query, "user_id") {
				return nil, errMissingOwnerColumn
			}
			return &stubRows{cols: []string{"created_at", "updated_at"}, data: [][]driver.Value{{ts, ts}}}, nil
		}, nil)
		d := &Document{ID: "d2", Filename: "b.pdf", Status: StatusUploaded}
		if err := NewRepository(db).Create(context.Background(), d); err != nil {
			t.Fatalf("Create fallback: %v", err)
		}
		if !d.CreatedAt.Equal(ts) {
			t.Fatalf("timestamps not populated: %+v", d)
		}
	})
}

func TestRepositoryUpdateStatus(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		db := openStubDB(t)
		setStub(t, nil, func(string, []driver.Value) (driver.Result, error) {
			return stubResult{n: 1}, nil
		})
		if err := NewRepository(db).UpdateStatus(context.Background(), "d1", StatusExtracted); err != nil {
			t.Fatalf("UpdateStatus: %v", err)
		}
	})

	t.Run("unknown id maps to not found", func(t *testing.T) {
		db := openStubDB(t)
		setStub(t, nil, func(string, []driver.Value) (driver.Result, error) {
			return stubResult{n: 0}, nil
		})
		if err := NewRepository(db).UpdateStatus(context.Background(), "missing", StatusFailed); !errors.Is(err, apperr.ErrNotFound) {
			t.Fatalf("UpdateStatus err = %v, want ErrNotFound", err)
		}
	})

	t.Run("exec error wraps", func(t *testing.T) {
		db := openStubDB(t)
		setStub(t, nil, func(string, []driver.Value) (driver.Result, error) {
			return nil, errors.New("connection refused")
		})
		if err := NewRepository(db).UpdateStatus(context.Background(), "d1", StatusFailed); err == nil {
			t.Fatalf("expected exec error")
		}
	})
}
