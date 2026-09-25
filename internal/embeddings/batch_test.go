package embeddings

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

// embBatchStubDriver records queries/execs so batch tests can assert
// single-statement behavior without a live Postgres.
type embBatchStubDriver struct{}

type embBatchStubConn struct{}

type embBatchStubRows struct {
	cols []string
	data [][]driver.Value
	pos  int
}

func (r *embBatchStubRows) Columns() []string { return r.cols }
func (r *embBatchStubRows) Close() error      { return nil }
func (r *embBatchStubRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.pos])
	r.pos++
	return nil
}

type embBatchStubResult struct{ n int64 }

func (r embBatchStubResult) LastInsertId() (int64, error) { return 0, nil }
func (r embBatchStubResult) RowsAffected() (int64, error) { return r.n, nil }

var (
	embBatchMu      sync.Mutex
	embBatchQuery   func(query string, args []driver.Value) (driver.Rows, error)
	embBatchExec    func(query string, args []driver.Value) (driver.Result, error)
	embBatchQueries []string
	embBatchExecs   []string
	embBatchReg     sync.Once
)

func (embBatchStubDriver) Open(string) (driver.Conn, error) { return embBatchStubConn{}, nil }
func (embBatchStubConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not supported")
}
func (embBatchStubConn) Close() error              { return nil }
func (embBatchStubConn) Begin() (driver.Tx, error) { return nil, errors.New("tx not supported") }

func (embBatchStubConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	vals := make([]driver.Value, len(args))
	for i, a := range args {
		vals[i] = a.Value
	}
	embBatchMu.Lock()
	embBatchQueries = append(embBatchQueries, query)
	fn := embBatchQuery
	embBatchMu.Unlock()
	if fn == nil {
		return nil, errors.New("embeddings test: no stub query handler")
	}
	return fn(query, vals)
}

func (embBatchStubConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	vals := make([]driver.Value, len(args))
	for i, a := range args {
		vals[i] = a.Value
	}
	embBatchMu.Lock()
	embBatchExecs = append(embBatchExecs, query)
	fn := embBatchExec
	embBatchMu.Unlock()
	if fn == nil {
		return nil, errors.New("embeddings test: no stub exec handler")
	}
	return fn(query, vals)
}

func openEmbBatchStubDB(t *testing.T) *sql.DB {
	t.Helper()
	embBatchReg.Do(func() { sql.Register("embbatchstub", embBatchStubDriver{}) })
	db, err := sql.Open("embbatchstub", "test")
	if err != nil {
		t.Fatalf("sql.Open stub: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		embBatchMu.Lock()
		embBatchQuery = nil
		embBatchExec = nil
		embBatchQueries = nil
		embBatchExecs = nil
		embBatchMu.Unlock()
	})
	return db
}

func testVector(fill float32) []float32 {
	v := make([]float32, DefaultDimensions)
	for i := range v {
		v[i] = fill
	}
	return v
}

func TestBuildBatchUpsertQueryPlaceholders(t *testing.T) {
	items := []UpsertItem{
		{QuestionID: "q1", Vector: testVector(1), Model: DefaultModel, InputHash: "h1"},
		{QuestionID: "q2", Vector: testVector(2), Model: DefaultModel, InputHash: "h2"},
	}
	query, args, err := buildBatchUpsertQuery(items)
	if err != nil {
		t.Fatalf("buildBatchUpsertQuery: %v", err)
	}
	if !strings.Contains(query, "INSERT INTO embeddings") || !strings.Contains(query, "ON CONFLICT (question_id)") {
		t.Fatalf("query = %q, want multi-row upsert", query)
	}
	if len(args) != 8 {
		t.Fatalf("args = %d, want 8 (4 per row)", len(args))
	}
	for _, want := range []string{"$1", "$4", "$5", "$8", "$2::vector", "$6::vector"} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q: %q", want, query)
		}
	}
}

func TestBuildBatchUpsertQueryBadDimensions(t *testing.T) {
	items := []UpsertItem{{QuestionID: "q1", Vector: []float32{1, 2}, Model: DefaultModel, InputHash: "h1"}}
	if _, _, err := buildBatchUpsertQuery(items); err == nil {
		t.Fatalf("buildBatchUpsertQuery with bad dimensions succeeded, want error")
	}
}

func TestBatchUpsertEmptyNoDB(t *testing.T) {
	repo := NewRepository(nil)
	if err := repo.(*repository).BatchUpsert(context.Background(), nil); err != nil {
		t.Fatalf("BatchUpsert(nil) = %v, want nil", err)
	}
}

func TestBatchUpsertSingleStatement(t *testing.T) {
	db := openEmbBatchStubDB(t)
	embBatchMu.Lock()
	embBatchExec = func(string, []driver.Value) (driver.Result, error) {
		return embBatchStubResult{n: 3}, nil
	}
	embBatchMu.Unlock()

	repo := NewRepository(db)
	items := []UpsertItem{
		{QuestionID: "q1", Vector: testVector(1), Model: DefaultModel, InputHash: "h1"},
		{QuestionID: "q2", Vector: testVector(2), Model: DefaultModel, InputHash: "h2"},
		{QuestionID: "q3", Vector: testVector(3), Model: DefaultModel, InputHash: "h3"},
	}
	if err := repo.(*repository).BatchUpsert(context.Background(), items); err != nil {
		t.Fatalf("BatchUpsert: %v", err)
	}
	embBatchMu.Lock()
	n := len(embBatchExecs)
	embBatchMu.Unlock()
	if n != 1 {
		t.Fatalf("exec calls = %d, want exactly 1 (no per-question N+1)", n)
	}
}

func TestFindReusableBatchEmptyNoDB(t *testing.T) {
	repo := NewRepository(nil)
	got, err := repo.(*repository).FindReusableBatch(context.Background(), DefaultModel, nil)
	if err != nil {
		t.Fatalf("FindReusableBatch(nil) = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Fatalf("result len = %d, want 0", len(got))
	}
}

func TestFindReusableBatchSingleQuery(t *testing.T) {
	db := openEmbBatchStubDB(t)
	ts := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	embBatchMu.Lock()
	embBatchQuery = func(string, []driver.Value) (driver.Rows, error) {
		return &embBatchStubRows{
			cols: []string{"question_id", "model", "embedding_input_hash", "created_at", "updated_at"},
			data: [][]driver.Value{
				{"q9", DefaultModel, "h1", ts, ts},
				{"q7", DefaultModel, "h2", ts, ts},
			},
		}, nil
	}
	embBatchMu.Unlock()

	repo := NewRepository(db)
	got, err := repo.(*repository).FindReusableBatch(context.Background(), DefaultModel, []string{"h1", "h2", "h3"})
	if err != nil {
		t.Fatalf("FindReusableBatch: %v", err)
	}
	embBatchMu.Lock()
	n := len(embBatchQueries)
	q := ""
	if n > 0 {
		q = embBatchQueries[0]
	}
	embBatchMu.Unlock()
	if n != 1 {
		t.Fatalf("query calls = %d, want exactly 1", n)
	}
	if !strings.Contains(q, "ANY($2)") {
		t.Fatalf("query = %q, want ANY($2) single-query batch", q)
	}
	if len(got) != 2 {
		t.Fatalf("result len = %d, want 2 (h3 unmatched)", len(got))
	}
	if got["h1"].QuestionID != "q9" {
		t.Fatalf("h1.QuestionID = %q, want q9", got["h1"].QuestionID)
	}
}
