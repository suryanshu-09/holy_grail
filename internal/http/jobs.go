package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/suryanshu-09/holy_grail/internal/apperr"
	"github.com/suryanshu-09/holy_grail/internal/documents"
	"github.com/suryanshu-09/holy_grail/internal/httpx"
	"github.com/suryanshu-09/holy_grail/internal/jobs"
)

// processingStepsOrdered is the canonical Phase 19 pipeline order shown in
// the PLAN UI progress display.
var processingStepsOrdered = []jobs.ProgressStep{
	jobs.StepUploaded,
	jobs.StepExtracting,
	jobs.StepQuestions,
	jobs.StepImages,
	jobs.StepClassifying,
	jobs.StepEmbeddings,
}

// processingStepLabels maps each pipeline step to its UI label from PLAN
// Phase 19 ("Uploaded", "Extracting pages", ...).
var processingStepLabels = map[jobs.ProgressStep]string{
	jobs.StepUploaded:    "Uploaded",
	jobs.StepExtracting:  "Extracting pages",
	jobs.StepQuestions:   "Extracting questions",
	jobs.StepImages:      "Detecting images",
	jobs.StepClassifying: "Classifying topics",
	jobs.StepEmbeddings:  "Generating embeddings",
}

// processingStepView is one row of the PLAN-style steps checklist.
// State is one of "done" (✓), "active" (→), "pending" (○) or "failed".
type processingStepView struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	State   string `json:"state"`
	Percent int    `json:"percent"`
}

// processingStatusResponse mirrors the PLAN Phase 19 UI:
//
//	Processing...
//	✓ Uploaded → … ○ …
//	73%
type processingStatusResponse struct {
	DocumentID  string               `json:"document_id"`
	JobID       *string              `json:"job_id,omitempty"`
	JobType     string               `json:"job_type,omitempty"`
	Status      string               `json:"status"`
	Progress    int                  `json:"progress"`
	CurrentStep string               `json:"current_step"`
	Steps       []processingStepView `json:"steps"`
	Attempts    int                  `json:"attempts,omitempty"`
	LastError   string               `json:"last_error,omitempty"`
}

// handleProcessDocument enqueues a process_document job for one document.
//
//	POST /api/v1/documents/{id}/process
//
// Responses:
//   - 202 with the created jobs.Job on success.
//   - 409 with the existing active jobs.Job when a non-terminal
//     process_document job already exists for the document (dedup).
//   - 404 when the document does not exist.
//   - 503 when the job queue is not configured (nil store/enqueuer).
//   - 405 for non-POST methods, 400 when the id is missing.
func handleProcessDocument(docs *documents.Service, store jobs.Store, enqueuer jobs.Enqueuer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		id := r.PathValue("id")
		if id == "" {
			httpx.Error(w, http.StatusBadRequest, "missing document id")
			return
		}
		if store == nil && enqueuer == nil {
			httpx.Error(w, http.StatusServiceUnavailable, "job queue not configured")
			return
		}
		if docs == nil {
			httpx.Error(w, http.StatusInternalServerError, "documents service not configured")
			return
		}
		if _, err := docs.Get(r.Context(), id); err != nil {
			if errors.Is(err, apperr.ErrNotFound) {
				httpx.Error(w, http.StatusNotFound, "document not found")
				return
			}
			httpx.LogError("document lookup failed", err)
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}
		// Dedup: a non-terminal process_document job already owns this
		// document, so report a conflict instead of double-processing.
		if store != nil {
			if existing, found := activeProcessJob(r.Context(), store, id); found {
				httpx.WriteJSON(w, http.StatusConflict, existing)
				return
			}
		}

		uniqueKey := processJobUniqueKey(id)
		payload, _ := json.Marshal(map[string]string{"document_id": id})
		params := jobs.EnqueueParams{
			Type:       jobs.TypeProcessDocument,
			DocumentID: &id,
			Payload:    payload,
			UniqueKey:  &uniqueKey,
		}
		var (
			job jobs.Job
			err error
		)
		if enqueuer != nil {
			job, err = enqueuer.Enqueue(r.Context(), params)
		} else {
			job, err = store.Enqueue(r.Context(), params)
		}
		if err != nil {
			httpx.LogError("job enqueue failed", err)
			httpx.Error(w, http.StatusInternalServerError, "failed to enqueue processing job")
			return
		}
		httpx.WriteJSON(w, http.StatusAccepted, job)
	})
}

// activeProcessJob returns the newest non-terminal process_document job for
// a document, if any. ListByDocument returns newest-first, so the first
// non-terminal process_document match wins.
func activeProcessJob(ctx context.Context, store jobs.Store, documentID string) (jobs.Job, bool) {
	jls, err := store.ListByDocument(ctx, documentID)
	if err != nil {
		return jobs.Job{}, false
	}
	for _, j := range jls {
		if j.Type != jobs.TypeProcessDocument {
			continue
		}
		if !jobs.IsTerminal(j.Status.Normalized()) {
			return j, true
		}
	}
	return jobs.Job{}, false
}

// handleGetJob returns a single background job.
//
//	GET /api/v1/jobs/{id}
//
// Responses:
//   - 200 with the jobs.Job.
//   - 404 when the job does not exist.
//   - 503 when the job store is not configured.
func handleGetJob(store jobs.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		id := r.PathValue("id")
		if id == "" {
			httpx.Error(w, http.StatusBadRequest, "missing job id")
			return
		}
		if store == nil {
			httpx.Error(w, http.StatusServiceUnavailable, "job queue not configured")
			return
		}
		job, err := store.Get(r.Context(), id)
		if err != nil {
			if errors.Is(err, apperr.ErrNotFound) {
				httpx.Error(w, http.StatusNotFound, "job not found")
				return
			}
			httpx.LogError("job lookup failed", err)
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, job)
	})
}

// handleProcessingStatus returns the PLAN-style progress checklist for the
// latest process_document job of a document.
//
//	GET /api/v1/documents/{id}/processing-status
//
// When no job exists yet the endpoint returns 200 with status "idle" and an
// all-pending checklist so the UI can render before processing starts.
// A nil job store yields 503; an unknown document yields 404 only when the
// documents service is wired (otherwise the store is the source of truth).
func handleProcessingStatus(docs *documents.Service, store jobs.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		id := r.PathValue("id")
		if id == "" {
			httpx.Error(w, http.StatusBadRequest, "missing document id")
			return
		}
		if store == nil {
			httpx.Error(w, http.StatusServiceUnavailable, "job queue not configured")
			return
		}
		if docs != nil {
			if _, err := docs.Get(r.Context(), id); err != nil {
				if errors.Is(err, apperr.ErrNotFound) {
					httpx.Error(w, http.StatusNotFound, "document not found")
					return
				}
				httpx.LogError("document lookup failed", err)
				httpx.Error(w, http.StatusInternalServerError, "internal server error")
				return
			}
		}
		jls, err := store.ListByDocument(r.Context(), id)
		if err != nil {
			httpx.LogError("job list failed", err)
			httpx.Error(w, http.StatusInternalServerError, "internal server error")
			return
		}
		job := latestProcessJob(jls)
		httpx.WriteJSON(w, http.StatusOK, buildProcessingStatus(id, job))
	})
}

// latestProcessJob picks the newest process_document job, falling back to
// the newest job of any type. ListByDocument returns newest-first, so the
// first match wins. Nil when no jobs exist.
func latestProcessJob(jls []jobs.Job) *jobs.Job {
	if len(jls) == 0 {
		return nil
	}
	for i := range jls {
		if jls[i].Type == jobs.TypeProcessDocument {
			j := jls[i]
			return &j
		}
	}
	j := jls[0]
	return &j
}

// buildProcessingStatus renders a job (or idle when nil) into the PLAN UI
// shape: ordered steps with done/active/pending states plus a percent.
func buildProcessingStatus(documentID string, job *jobs.Job) processingStatusResponse {
	if job == nil {
		return processingStatusResponse{
			DocumentID: documentID,
			Status:     "idle",
			Progress:   0,
			Steps:      idleSteps(),
		}
	}
	status := string(job.Status.Normalized())
	progress := job.Progress
	if job.Status.Normalized() == jobs.StatusCompleted {
		progress = 100
	} else if progress == 0 && job.CurrentStep != "" {
		if p := jobs.ProgressForStep(jobs.ProgressStep(job.CurrentStep)); p >= 0 {
			progress = p
		}
	}
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	resp := processingStatusResponse{
		DocumentID:  documentID,
		JobType:     string(job.Type),
		Status:      status,
		Progress:    progress,
		CurrentStep: job.CurrentStep,
		Steps:       buildSteps(job),
		Attempts:    job.Attempts,
		LastError:   job.LastError,
	}
	resp.JobID = &job.ID
	return resp
}

// buildSteps marks steps before the current step done, the current step
// active (failed when the job failed, done when completed) and the rest
// pending.
func buildSteps(job *jobs.Job) []processingStepView {
	completed := job.Status.Normalized() == jobs.StatusCompleted
	failed := job.Status.Normalized() == jobs.StatusFailed
	curIdx := -1
	for i, s := range processingStepsOrdered {
		if string(s) == job.CurrentStep {
			curIdx = i
			break
		}
	}
	steps := make([]processingStepView, 0, len(processingStepsOrdered))
	for i, s := range processingStepsOrdered {
		state := "pending"
		switch {
		case completed:
			state = "done"
		case failed && i == curIdx:
			state = "failed"
		case curIdx == -1:
			// No step reported yet: an existing non-terminal job has at
			// least been uploaded, so mark the first step active.
			if i == 0 && !failed {
				state = "active"
			}
		case i < curIdx:
			state = "done"
		case i == curIdx:
			state = "active"
		}
		steps = append(steps, processingStepView{
			Key:     string(s),
			Label:   processingStepLabels[s],
			State:   state,
			Percent: jobs.StepProgress[s],
		})
	}
	return steps
}

// idleSteps returns an all-pending checklist for documents with no job yet.
func idleSteps() []processingStepView {
	steps := make([]processingStepView, 0, len(processingStepsOrdered))
	for _, s := range processingStepsOrdered {
		steps = append(steps, processingStepView{
			Key:     string(s),
			Label:   processingStepLabels[s],
			State:   "pending",
			Percent: jobs.StepProgress[s],
		})
	}
	return steps
}

// processJobUniqueKey returns the dedup key for document processing.
func processJobUniqueKey(documentID string) string {
	return fmt.Sprintf("process_document:%s", documentID)
}
