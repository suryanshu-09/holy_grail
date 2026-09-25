package questions

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

// batchStubDriver records queries/execs so batch tests can assert single-
// statement behavior without a live Postgres.
type batchStubDriver struct{}

type batchStubConn struct{}

type batchStubRows struct {
	cols []string
	data [][]driver.Value
	pos  int
}

func (r *batchStubRows) Columns() []string { return r.cols }
func (r *batchStubRows) Close() error      { return nil }
func (r *batchStubRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.pos])
	r.pos++
	return nil
}

type batchStubResult struct{ n int64 }

func (r batchStubResult) LastInsertId() (int64, error) { return 0, nil }
func (r batchStubResult) RowsAffected() (int64, error) { return r.n, nil }

var (
	batchStubMu      sync.Mutex
	batchStubQuery   func(query string, args []driver.Value) (driver.Rows, error)
	batchStubExec    func(query string, args []driver.Value) (driver.Result, error)
	batchStubQueries []string
	batchStubExecs   []string
	batchStubReg     sync.Once
)

func (batchStubDriver) Open(string) (driver.Conn, error) { return batchStubConn{}, nil }
func (batchStubConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not supported")
}
func (batchStubConn) Close() error              { return nil }
func (batchStubConn) Begin() (driver.Tx, error) { return nil, errors.New("tx not supported") }

func (batchStubConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	vals := make([]driver.Value, len(args))
	for i, a := range args {
		vals[i] = a.Value
	}
	batchStubMu.Lock()
	batchStubQueries = append(batchStubQueries, query)
	fn := batchStubQuery
	batchStubMu.Unlock()
	if fn == nil {
		return nil, errors.New("questions test: no stub query handler")
	}
	return fn(query, vals)
}

func (batchStubConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	vals := make([]driver.Value, len(args))
	for i, a := range args {
		vals[i] = a.Value
	}
	batchStubMu.Lock()
	batchStubExecs = append(batchStubExecs, query)
	fn := batchStubExec
	batchStubMu.Unlock()
	if fn == nil {
		return nil, errors.New("questions test: no stub exec handler")
	}
	return fn(query, vals)
}

func openBatchStubDB(t *testing.T) *sql.DB {
	t.Helper()
	batchStubReg.Do(func() { sql.Register("questionsbatchstub", batchStubDriver{}) })
	db, err := sql.Open("questionsbatchstub", "test")
	if err != nil {
		t.Fatalf("sql.Open stub: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		batchStubMu.Lock()
		batchStubQuery = nil
		batchStubExec = nil
		batchStubQueries = nil
		batchStubExecs = nil
		batchStubMu.Unlock()
	})
	return db
}

func strP(s string) *string { return &s }

func TestBuildBatchInsertQueryPlaceholders(t *testing.T) {
	qs := []Question{
		{ID: "q1", DocumentID: "d1", QuestionText: strP("one")},
		{DocumentID: "d1", QuestionText: strP("two")}, // no ID -> gen_random_uuid()
	}
	query, args := buildBatchInsertQuery(qs)
	if !strings.Contains(query, "INSERT INTO questions") {
		t.Fatalf("query = %q, want INSERT INTO questions", query)
	}
	if got := strings.Count(query, "gen_random_uuid()"); got != 1 {
		t.Fatalf("gen_random_uuid count = %d, want 1", got)
	}
	// 17 columns per row, minus the one generated UUID.
	if len(args) != 2*17-1 {
		t.Fatalf("args = %d, want %d", len(args), 2*17-1)
	}
	// Placeholders must be dense $1..$N with no gaps.
	for i := 1; i <= len(args); i++ {
		if !strings.Contains(query, "$"+itoa(i)) {
			t.Fatalf("query missing placeholder $%d: %q", i, query)
		}
	}
	if args[0] != "q1" {
		t.Fatalf("args[0] = %v, want q1", args[0])
	}
}

func TestBatchInsertEmptyNoDB(t *testing.T) {
	repo := NewRepository(nil)
	if err := repo.(*repository).BatchInsert(context.Background(), nil); err != nil {
		t.Fatalf("BatchInsert(nil) = %v, want nil", err)
	}
}

func TestBatchInsertSingleStatement(t *testing.T) {
	db := openBatchStubDB(t)
	batchStubMu.Lock()
	batchStubExec = func(string, []driver.Value) (driver.Result, error) {
		return batchStubResult{n: 3}, nil
	}
	batchStubMu.Unlock()

	repo := NewRepository(db)
	qs := []Question{
		{ID: "q1", DocumentID: "d1"},
		{ID: "q2", DocumentID: "d1"},
		{ID: "q3", DocumentID: "d1"},
	}
	if err := repo.(*repository).BatchInsert(context.Background(), qs); err != nil {
		t.Fatalf("BatchInsert: %v", err)
	}
	batchStubMu.Lock()
	n := len(batchStubExecs)
	q := ""
	if n > 0 {
		q = batchStubExecs[0]
	}
	batchStubMu.Unlock()
	if n != 1 {
		t.Fatalf("exec calls = %d, want exactly 1 (no per-question N+1)", n)
	}
	if !strings.Contains(q, "INSERT INTO questions") {
		t.Fatalf("exec query = %q, want multi-row INSERT", q)
	}
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
	db := openBatchStubDB(t)
	ts := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	batchStubMu.Lock()
	batchStubQuery = func(string, []driver.Value) (driver.Rows, error) {
		return &batchStubRows{
			cols: []string{"question_id", "id", "name", "subject", "confidence", "created_at"},
			data: [][]driver.Value{
				{"q1", "t1", "Algebra", "Math", float64(0.9), ts},
				{"q1", "t2", "Geometry", "Math", nil, ts},
				{"q2", "t2", "Geometry", "Math", nil, ts},
			},
		}, nil
	}
	batchStubMu.Unlock()

	repo := NewRepository(db)
	got, err := repo.(*repository).ListTopicsForQuestions(context.Background(), []string{"q1", "q2", "q3"})
	if err != nil {
		t.Fatalf("ListTopicsForQuestions: %v", err)
	}
	batchStubMu.Lock()
	n := len(batchStubQueries)
	q := ""
	if n > 0 {
		q = batchStubQueries[0]
	}
	batchStubMu.Unlock()
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

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}
