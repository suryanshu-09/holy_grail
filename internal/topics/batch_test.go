package topics

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
)

// topicsBatchStubDriver records queries so batch tests can assert
// single-statement behavior without a live Postgres.
type topicsBatchStubDriver struct{}

type topicsBatchStubConn struct{}

type topicsBatchStubRows struct {
	cols []string
	data [][]driver.Value
	pos  int
}

func (r *topicsBatchStubRows) Columns() []string { return r.cols }
func (r *topicsBatchStubRows) Close() error      { return nil }
func (r *topicsBatchStubRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.pos])
	r.pos++
	return nil
}

var (
	topicsBatchMu      sync.Mutex
	topicsBatchQuery   func(query string, args []driver.Value) (driver.Rows, error)
	topicsBatchQueries []string
	topicsBatchReg     sync.Once
)

func (topicsBatchStubDriver) Open(string) (driver.Conn, error) { return topicsBatchStubConn{}, nil }
func (topicsBatchStubConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not supported")
}
func (topicsBatchStubConn) Close() error              { return nil }
func (topicsBatchStubConn) Begin() (driver.Tx, error) { return nil, errors.New("tx not supported") }

func (topicsBatchStubConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	vals := make([]driver.Value, len(args))
	for i, a := range args {
		vals[i] = a.Value
	}
	topicsBatchMu.Lock()
	topicsBatchQueries = append(topicsBatchQueries, query)
	fn := topicsBatchQuery
	topicsBatchMu.Unlock()
	if fn == nil {
		return nil, errors.New("topics test: no stub query handler")
	}
	return fn(query, vals)
}

func openTopicsBatchStubDB(t *testing.T) *sql.DB {
	t.Helper()
	topicsBatchReg.Do(func() { sql.Register("topicsbatchstub", topicsBatchStubDriver{}) })
	db, err := sql.Open("topicsbatchstub", "test")
	if err != nil {
		t.Fatalf("sql.Open stub: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		topicsBatchMu.Lock()
		topicsBatchQuery = nil
		topicsBatchQueries = nil
		topicsBatchMu.Unlock()
	})
	return db
}

func TestListTopicsForQuestionsEmptyNoDB(t *testing.T) {
	repo := NewRepository(nil)
	got, err := repo.(*repository).ListTopicsForQuestions(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTopicsForQuestions(nil) = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Fatalf("result len = %d, want 0", len(got))
	}
}

func TestListTopicsForQuestionsSingleQuery(t *testing.T) {
	db := openTopicsBatchStubDB(t)
	ts := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	subj := "Math"
	topicsBatchMu.Lock()
	topicsBatchQuery = func(string, []driver.Value) (driver.Rows, error) {
		return &topicsBatchStubRows{
			cols: []string{"question_id", "id", "name", "subject", "created_at"},
			data: [][]driver.Value{
				{"q1", "t1", "Algebra", subj, ts},
				{"q1", "t2", "Geometry", subj, ts},
				{"q2", "t2", "Geometry", subj, ts},
			},
		}, nil
	}
	topicsBatchMu.Unlock()

	repo := NewRepository(db)
	got, err := repo.(*repository).ListTopicsForQuestions(context.Background(), []string{"q1", "q2", "q3"})
	if err != nil {
		t.Fatalf("ListTopicsForQuestions: %v", err)
	}
	topicsBatchMu.Lock()
	n := len(topicsBatchQueries)
	q := ""
	if n > 0 {
		q = topicsBatchQueries[0]
	}
	topicsBatchMu.Unlock()
	if n != 1 {
		t.Fatalf("query calls = %d, want exactly 1 (N+1 avoided)", n)
	}
	if !strings.Contains(q, "ANY($1)") {
		t.Fatalf("query = %q, want ANY($1) single-query batch", q)
	}
	if len(got) != 3 {
		t.Fatalf("result keys = %d, want 3 (every ID present)", len(got))
	}
	if len(got["q1"]) != 2 || len(got["q2"]) != 1 {
		t.Fatalf("counts = %d/%d, want 2/1", len(got["q1"]), len(got["q2"]))
	}
	if got["q3"] == nil || len(got["q3"]) != 0 {
		t.Fatalf("q3 = %v, want empty non-nil slice", got["q3"])
	}
	if got["q1"][0].Name != "Algebra" {
		t.Fatalf("q1[0].Name = %q, want Algebra", got["q1"][0].Name)
	}
}
