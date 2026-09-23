// Package jobs implements Phase 19 background-job state: job types,
// statuses, progress steps, persistent storage, and queue adapters.
package jobs

import (
	"encoding/json"
	"time"
)

// JobType identifies the kind of background work to perform.
// Initially process_document covers the whole pipeline; the finer-grained
// types allow splitting stages later without schema changes.
type JobType string

const (
	TypeProcessDocument    JobType = "process_document"
	TypeExtractQuestions   JobType = "extract_questions"
	TypeClassifyQuestions  JobType = "classify_questions"
	TypeGenerateEmbeddings JobType = "generate_embeddings"
)

// ValidJobType reports whether t is a known job type.
func ValidJobType(t JobType) bool {
	switch t {
	case TypeProcessDocument, TypeExtractQuestions, TypeClassifyQuestions, TypeGenerateEmbeddings:
		return true
	default:
		return false
	}
}

// JobStatus tracks where a job is in its lifecycle.
type JobStatus string

const (
	StatusQueued    JobStatus = "queued"
	StatusActive    JobStatus = "active"
	StatusCompleted JobStatus = "completed"
	StatusFailed    JobStatus = "failed"
	// StatusPending is the legacy default written by migration 008
	// (status TEXT NOT NULL DEFAULT 'pending'). Treat it as queued.
	StatusPending JobStatus = "pending"
)

// ValidStatus reports whether s is a known job status, including the
// legacy pending value for rows written before the queue used "queued".
func ValidStatus(s JobStatus) bool {
	switch s {
	case StatusQueued, StatusActive, StatusCompleted, StatusFailed, StatusPending:
		return true
	default:
		return false
	}
}

// IsTerminal reports whether s ends the job lifecycle.
func IsTerminal(s JobStatus) bool {
	return s == StatusCompleted || s == StatusFailed
}

// Normalized maps the legacy pending status onto queued so callers only
// need to handle the four canonical statuses.
func (s JobStatus) Normalized() JobStatus {
	if s == StatusPending {
		return StatusQueued
	}
	return s
}

// ProgressStep is a named pipeline stage reported to the UI.
// It mirrors the Phase 19 progress display:
//
//	Processing...
//	✓ Uploaded → Extracting pages → Extracting questions →
//	Detecting images → Classifying topics → Generating embeddings
type ProgressStep string

const (
	StepUploaded    ProgressStep = "uploaded"
	StepExtracting  ProgressStep = "extracting"
	StepQuestions   ProgressStep = "questions"
	StepImages      ProgressStep = "images"
	StepClassifying ProgressStep = "classifying"
	StepEmbeddings  ProgressStep = "embeddings"
)

// ValidStep reports whether s is a known progress step.
func ValidStep(s ProgressStep) bool {
	switch s {
	case StepUploaded, StepExtracting, StepQuestions, StepImages, StepClassifying, StepEmbeddings:
		return true
	default:
		return false
	}
}

// StepProgress maps each step to a representative completion percentage
// for the UI progress bar.
var StepProgress = map[ProgressStep]int{
	StepUploaded:    5,
	StepExtracting:  20,
	StepQuestions:   45,
	StepImages:      60,
	StepClassifying: 73,
	StepEmbeddings:  95,
}

// ProgressForStep returns the representative percentage for a step,
// or -1 when the step is unknown.
func ProgressForStep(s ProgressStep) int {
	if p, ok := StepProgress[s]; ok {
		return p
	}
	return -1
}

// Job mirrors the jobs table from migration 008.
type Job struct {
	ID             string          `json:"id"`
	Type           JobType         `json:"type"`
	DocumentID     *string         `json:"document_id,omitempty"`
	Status         JobStatus       `json:"status"`
	Progress       int             `json:"progress"`
	CurrentStep    string          `json:"current_step"`
	Payload        json.RawMessage `json:"payload"`
	Result         json.RawMessage `json:"result,omitempty"`
	LastError      string          `json:"last_error,omitempty"`
	Attempts       int             `json:"attempts"`
	MaxRetries     int             `json:"max_retries"`
	TimeoutSeconds int             `json:"timeout_seconds"`
	UniqueKey      *string         `json:"unique_key,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	StartedAt      *time.Time      `json:"started_at,omitempty"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty"`
}

// EnqueueParams carries the inputs for creating a job.
type EnqueueParams struct {
	Type           JobType
	DocumentID     *string
	Payload        json.RawMessage
	MaxRetries     int
	TimeoutSeconds int
	UniqueKey      *string
}

// WithDefaults fills zero-value retry/timeout/payload fields with the
// package defaults.
func (p EnqueueParams) WithDefaults() EnqueueParams {
	if p.MaxRetries <= 0 {
		p.MaxRetries = DefaultMaxRetries
	}
	if p.TimeoutSeconds <= 0 {
		p.TimeoutSeconds = DefaultTimeoutSeconds
	}
	if len(p.Payload) == 0 {
		p.Payload = json.RawMessage(`{}`)
	}
	return p
}

const (
	// DefaultMaxRetries matches the migration 008 column default.
	DefaultMaxRetries = 5
	// DefaultTimeoutSeconds matches the migration 008 column default (600s).
	DefaultTimeoutSeconds = 600
)
