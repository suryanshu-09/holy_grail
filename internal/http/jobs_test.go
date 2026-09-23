package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
	"github.com/suryanshu-09/holy_grail/internal/documents"
	"github.com/suryanshu-09/holy_grail/internal/jobs"
	"github.com/suryanshu-09/holy_grail/internal/storage"
)

func testDocService(t *testing.T) *documents.Service {
	t.Helper()
	store, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeDocRepo{docs: map[string]documents.Document{
		"doc1": {ID: "doc1", Filename: "a.pdf", Status: "uploaded"},
	}}
	return documents.NewService(repo, store)
}

type fakeDocRepo struct {
	docs map[string]documents.Document
}

func (f *fakeDocRepo) List(ctx context.Context, fl documents.Filter) ([]documents.Document, error) {
	out := []documents.Document{}
	for _, d := range f.docs {
		out = append(out, d)
	}
	return out, nil
}
func (f *fakeDocRepo) GetByID(ctx context.Context, id string) (documents.Document, error) {
	d, ok := f.docs[id]
	if !ok {
		return documents.Document{}, apperr.ErrNotFound
	}
	return d, nil
}
func (f *fakeDocRepo) Create(ctx context.Context, d *documents.Document) error { return nil }
func (f *fakeDocRepo) UpdateStatus(ctx context.Context, id, status string) error {
	return nil
}

func TestSmokeJobsAPI(t *testing.T) {
	// Use apperr-mapped repo instead: swap to real not-found mapping check
	// by testing handlers with nil docs (skip doc validation) for status paths.
	store := jobs.NewMemoryStore()
	enq := jobs.NewMemoryEnqueuer(store, nil)

	// 1. GET /jobs/{id} 404
	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/nope", nil)
	req.SetPathValue("id", "nope")
	rec := httptest.NewRecorder()
	handleGetJob(store).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get missing job = %d, want 404", rec.Code)
	}

	// 2. processing-status idle (nil docs to skip doc check)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/documents/doc1/processing-status", nil)
	req.SetPathValue("id", "doc1")
	rec = httptest.NewRecorder()
	handleProcessingStatus(nil, store).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("idle status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var idle processingStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &idle); err != nil {
		t.Fatal(err)
	}
	if idle.Status != "idle" || len(idle.Steps) != 6 {
		t.Fatalf("idle = %+v", idle)
	}

	// 3. enqueue via store then GET job + status active checklist
	ctx := context.Background()
	uk := "process_document:doc1"
	docID := "doc1"
	j, err := enq.Enqueue(ctx, jobs.EnqueueParams{
		Type: jobs.TypeProcessDocument, DocumentID: &docID, UniqueKey: &uk,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateProgress(ctx, j.ID, 73, jobs.StepClassifying); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+j.ID, nil)
	req.SetPathValue("id", j.ID)
	rec = httptest.NewRecorder()
	handleGetJob(store).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get job = %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/documents/doc1/processing-status", nil)
	req.SetPathValue("id", "doc1")
	rec = httptest.NewRecorder()
	handleProcessingStatus(nil, store).ServeHTTP(rec, req)
	var st processingStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Progress != 73 {
		t.Fatalf("progress = %d, want 73", st.Progress)
	}
	// steps before classifying done, classifying active, embeddings pending
	want := map[string]string{
		"uploaded": "done", "extracting": "done", "questions": "done",
		"images": "done", "classifying": "active", "embeddings": "pending",
	}
	for _, s := range st.Steps {
		if want[s.Key] != s.State {
			t.Fatalf("step %s = %s, want %s", s.Key, s.State, want[s.Key])
		}
	}

	// 4. dedup helper finds active job
	if _, found := activeProcessJob(ctx, store, "doc1"); !found {
		t.Fatal("activeProcessJob should find the queued job")
	}

	// 5. nil store => 503
	req = httptest.NewRequest(http.MethodGet, "/api/v1/jobs/x", nil)
	req.SetPathValue("id", "x")
	rec = httptest.NewRecorder()
	handleGetJob(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil store = %d, want 503", rec.Code)
	}

	// 6. POST /documents/{id}/process: 202 then 409 on duplicate, 404 unknown
	store2 := jobs.NewMemoryStore()
	enq2 := jobs.NewMemoryEnqueuer(store2, nil)
	docsSvc := testDocService(t)
	post := func(docID string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/documents/"+docID+"/process", nil)
		r.SetPathValue("id", docID)
		rr := httptest.NewRecorder()
		handleProcessDocument(docsSvc, store2, enq2).ServeHTTP(rr, r)
		return rr
	}
	first := post("doc1")
	if first.Code != http.StatusAccepted {
		t.Fatalf("first process = %d body=%s, want 202", first.Code, first.Body.String())
	}
	var created jobs.Job
	if err := json.Unmarshal(first.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Type != jobs.TypeProcessDocument {
		t.Fatalf("job type = %s", created.Type)
	}
	second := post("doc1")
	if second.Code != http.StatusConflict {
		t.Fatalf("second process = %d, want 409", second.Code)
	}
	missing := post("nope")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing doc process = %d, want 404", missing.Code)
	}
	// completed job no longer conflicts: complete it, then re-process => 202
	if _, err := store2.UpdateStatus(ctx, created.ID, jobs.StatusCompleted, ""); err != nil {
		t.Fatal(err)
	}
	third := post("doc1")
	if third.Code != http.StatusAccepted {
		t.Fatalf("re-process after complete = %d, want 202", third.Code)
	}
}
