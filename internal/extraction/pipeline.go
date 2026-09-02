package extraction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"github.com/suryanshu-09/holy_grail/internal/documents"
	"github.com/suryanshu-09/holy_grail/internal/questions"
	"github.com/suryanshu-09/holy_grail/internal/topics"
)

const (
	// extractionDirName is the directory, next to original.pdf, holding the
	// intermediate results of a run: pages.json plus debug page dumps.
	extractionDirName = "extraction"

	debugDirName  = "debug"
	pagesFileName = "pages.json"
)

// ExtractionService wires the page extraction pipeline into the application:
// it runs extract -> normalize -> detect -> optional OCR for one uploaded
// document, saves the intermediate results under
// <root>/documents/<id>/extraction/ and keeps documents.status up to date via
// the documents repository.
type ExtractionService struct {
	root         string
	extractor    *Service
	repo         documents.Repository
	questionRepo questions.Repository
	llmFallback  *LLMFallback
	debugWriter  *DebugWriter
	classifier   topics.ClassifierInterface
	topicRepo    topics.Repository
}

// WithLLMFallback attaches an LLM fallback to the extraction service.
func (s *ExtractionService) WithLLMFallback(f *LLMFallback) *ExtractionService {
	s.llmFallback = f
	return s
}

// WithDebugWriter attaches a debug writer that also receives question extraction
// artifacts for replay and QA. This is separate from the page-level debug writer
// on the Service itself.
func (s *ExtractionService) WithDebugWriter(w *DebugWriter) *ExtractionService {
	s.debugWriter = w
	return s
}

// WithClassifier attaches a topic classifier used after question persistence.
// It is non-fatal: LLM failures are logged and continued.
func (s *ExtractionService) WithClassifier(c topics.ClassifierInterface) *ExtractionService {
	s.classifier = c
	return s
}

// WithTopicRepo attaches the topics repository used to persist classification results.
func (s *ExtractionService) WithTopicRepo(r topics.Repository) *ExtractionService {
	s.topicRepo = r
	return s
}

// WithTopicClassifier is a convenience that attaches both classifier and topic repo.
func (s *ExtractionService) WithTopicClassifier(c topics.ClassifierInterface, r topics.Repository) *ExtractionService {
	s.classifier = c
	s.topicRepo = r
	return s
}

// NewExtractionService resolves root (e.g. ./data) to an absolute path and
// returns an extraction service that persists intermediates there.
func NewExtractionService(root string, extractor *Service, repo documents.Repository, qrepo questions.Repository) (*ExtractionService, error) {
	if extractor == nil {
		return nil, fmt.Errorf("extraction: nil extractor service")
	}
	if repo == nil {
		return nil, fmt.Errorf("extraction: nil documents repository")
	}
	if qrepo == nil {
		return nil, fmt.Errorf("extraction: nil questions repository")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("extraction: resolve root: %w", err)
	}
	return &ExtractionService{root: abs, extractor: extractor, repo: repo, questionRepo: qrepo}, nil
}

// Extract runs the full pipeline for one document. documentID identifies the
// row in the documents table and storagePath is its slash-separated location
// relative to the data root as recorded in documents.storage_path; an empty
// storagePath falls back to the canonical layout written by
// storage.LocalStore (documents/<id>/original.pdf).
//
// On success the document status becomes "extracted"; when the pipeline fails
// it becomes "failed" and the error is returned. Intermediate results are
// saved before the status flips to extracted so a crash never advertises
// extracted without artifacts on disk.
func (s *ExtractionService) Extract(ctx context.Context, documentID, storagePath string) (DocumentExtraction, error) {
	pdfPath, err := s.resolvePDF(documentID, storagePath)
	if err != nil {
		return DocumentExtraction{}, err
	}

	result, err := s.extractor.ExtractFile(ctx, documentID, pdfPath)
	if err != nil {
		return DocumentExtraction{}, s.markFailed(ctx, documentID, err)
	}

	if err := s.saveResults(documentID, result); err != nil {
		return DocumentExtraction{}, s.markFailed(ctx, documentID, err)
	}

	// Run question extraction and persist detected questions. Failures in
	// question persistence mark the document failed so they are visible to
	// operators and can be retried.
	if err := s.ExtractQuestions(ctx, result); err != nil {
		return DocumentExtraction{}, s.markFailed(ctx, documentID, err)
	}

	// Topic classification is non-fatal: log and continue on LLM failure.
	if s.classifier != nil && s.topicRepo != nil {
		if err := s.ClassifyDocument(ctx, documentID); err != nil {
			slog.Warn("extraction: classification failed (non-fatal)", "document", documentID, "error", err)
		}
	} else {
		slog.Debug("extraction: classification skipped (no classifier or topic repo)", "document", documentID)
		// Still write empty classification artifact for observability
		_ = s.writeClassificationArtifact(documentID, nil)
	}

	if err := s.repo.UpdateStatus(ctx, documentID, documents.StatusExtracted); err != nil {
		return DocumentExtraction{}, fmt.Errorf("extraction: mark %q extracted: %w", documentID, err)
	}
	return result, nil
}

// markFailed records the failure in documents.status and returns the original
// pipeline error wrapped with any status-update failure joined onto it.
func (s *ExtractionService) markFailed(ctx context.Context, documentID string, cause error) error {
	err := cause
	if updateErr := s.repo.UpdateStatus(ctx, documentID, documents.StatusFailed); updateErr != nil {
		err = errors.Join(cause, fmt.Errorf("extraction: mark %q failed: %w", documentID, updateErr))
	}
	return err
}

// resolvePDF turns the stored relative path into an absolute PDF path inside
// the data root, guarding against traversal via crafted IDs or paths. An
// empty storagePath falls back to the canonical layout of
// storage.LocalStore: <root>/documents/<id>/original.pdf.
func (s *ExtractionService) resolvePDF(documentID, storagePath string) (string, error) {
	if !safeSegment(documentID) {
		return "", fmt.Errorf("extraction: unsafe document id %q", documentID)
	}
	rel := filepath.Join(documentLayout, documentID, originalName)
	if storagePath != "" {
		rel = filepath.FromSlash(storagePath)
	}
	dest := filepath.Join(s.root, rel)
	if !withinRoot(s.root, dest) {
		return "", fmt.Errorf("extraction: storage path escapes root: %q", storagePath)
	}
	return dest, nil
}

// saveResults writes pages.json plus one debug/page-NNN.txt dump per page
// under <root>/documents/<id>/extraction/. Files are written atomically so a
// concurrent or crashed run never leaves truncated JSON behind. It also writes
// an images.json manifest that enumerates extracted images with their page
// positions and storage paths.
func (s *ExtractionService) saveResults(documentID string, result DocumentExtraction) error {
	dir := filepath.Join(s.root, documentLayout, documentID, extractionDirName)
	debugDir := filepath.Join(dir, debugDirName)
	if err := os.MkdirAll(debugDir, 0o755); err != nil {
		return fmt.Errorf("extraction: create extraction dir: %w", err)
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("extraction: encode pages: %w", err)
	}
	if err := writeFileAtomic(filepath.Join(dir, pagesFileName), append(data, '\n')); err != nil {
		return fmt.Errorf("extraction: write pages.json: %w", err)
	}

	for _, page := range result.Pages {
		name := "page-" + pad3(page.Number) + ".txt"
		if err := writeFileAtomic(filepath.Join(debugDir, name), []byte(page.Text)); err != nil {
			return fmt.Errorf("extraction: write %s: %w", name, err)
		}
	}

	// Write images manifest
	var allImages []ImageRef
	for _, pg := range result.Pages {
		allImages = append(allImages, pg.Images...)
	}
	if allImages == nil {
		allImages = []ImageRef{}
	}
	if imgData, err := json.MarshalIndent(allImages, "", "  "); err == nil {
		_ = writeFileAtomic(filepath.Join(dir, "images.json"), append(imgData, '\n'))
		_ = writeFileAtomic(filepath.Join(debugDir, "images.json"), append(imgData, '\n'))
	}
	return nil
}

// ClassificationArtifact records the classification result for a single question
// for debug artifact persistence.
type ClassificationArtifact struct {
	QuestionID   string              `json:"question_id"`
	QuestionText string              `json:"question_text"`
	Subject      *string             `json:"subject,omitempty"`
	Labels       []topics.TopicLabel `json:"labels"`
	Error        string              `json:"error,omitempty"`
	PromptHash   string              `json:"prompt_hash,omitempty"`
}

// ClassifyDocument classifies all questions for a document on-demand.
// It fetches questions via the questions repository, calls the classifier for each
// question, and persists topics via FindOrCreate + AddQuestionTopic with confidence.
// It is non-fatal per-question: LLM failures are logged and continued; the method
// returns nil unless the classifier or topic repo is not configured or a fatal DB
// error occurs. It also writes debug artifacts (classification.json) under the
// extraction dir for observability.
func (s *ExtractionService) ClassifyDocument(ctx context.Context, documentID string) error {
	if !safeSegment(documentID) {
		return fmt.Errorf("extraction: unsafe document id %q", documentID)
	}
	if s.classifier == nil {
		return fmt.Errorf("extraction: no classifier configured")
	}
	if s.topicRepo == nil {
		return fmt.Errorf("extraction: no topic repo configured")
	}
	if s.questionRepo == nil {
		return fmt.Errorf("extraction: no questions repository configured")
	}

	// Fetch questions for document. Paginate because List clamps limit to 100.
	var allQuestions []questions.Question
	offset := 0
	limit := 100
	for {
		batch, err := s.questionRepo.List(ctx, questions.Filter{
			DocumentID: documentID,
			Limit:      limit,
			Offset:     offset,
		})
		if err != nil {
			return fmt.Errorf("extraction: list questions for classification: %w", err)
		}
		if len(batch) == 0 {
			break
		}
		allQuestions = append(allQuestions, batch...)
		if len(batch) < limit {
			break
		}
		offset += len(batch)
	}

	if len(allQuestions) == 0 {
		slog.Info("extraction: no questions to classify", "document", documentID)
		return s.writeClassificationArtifact(documentID, nil)
	}

	var artifacts []ClassificationArtifact
	for _, q := range allQuestions {
		if q.QuestionText == nil || len(*q.QuestionText) == 0 {
			slog.Warn("extraction: skip classification for question with empty text", "question_id", q.ID)
			artifacts = append(artifacts, ClassificationArtifact{
				QuestionID:   q.ID,
				QuestionText: "",
				Labels:       nil,
				Error:        "empty question text",
			})
			continue
		}
		subject := ""
		if q.Subject != nil {
			subject = *q.Subject
		}
		// Also consider document subject fallback? For now use question subject.
		labels, err := s.classifier.ClassifyQuestion(ctx, *q.QuestionText, subject)
		promptHash := ""
		if s.classifier != nil {
			promptHash = s.classifier.LastPromptHash()
		}
		if err != nil {
			slog.Warn("extraction: classification failed for question (non-fatal)", "question_id", q.ID, "error", err)
			artifacts = append(artifacts, ClassificationArtifact{
				QuestionID:   q.ID,
				QuestionText: *q.QuestionText,
				Subject:      q.Subject,
				Labels:       nil,
				Error:        err.Error(),
				PromptHash:   promptHash,
			})
			continue
		}

		// Persist each label via FindOrCreate + AddQuestionTopic
		for _, lbl := range labels {
			var subjPtr *string
			if lbl.Subject != "" {
				subj := lbl.Subject
				subjPtr = &subj
			} else if subject != "" {
				subj := subject
				subjPtr = &subj
			}
			// Normalize already done inside classifier; but ensure FindOrCreate uses normalized name
			topic, err := s.topicRepo.FindOrCreate(ctx, lbl.Topic, subjPtr)
			if err != nil {
				slog.Warn("extraction: FindOrCreate topic failed (non-fatal)", "question_id", q.ID, "topic", lbl.Topic, "error", err)
				continue
			}
			conf := lbl.Confidence
			// Clamp confidence already done but double-check
			if conf < 0 {
				conf = 0
			}
			if conf > 1 {
				conf = 1
			}
			if err := s.topicRepo.AddQuestionTopic(ctx, q.ID, topic.ID, &conf); err != nil {
				slog.Warn("extraction: AddQuestionTopic failed (non-fatal)", "question_id", q.ID, "topic_id", topic.ID, "error", err)
				continue
			}
		}

		artifacts = append(artifacts, ClassificationArtifact{
			QuestionID:   q.ID,
			QuestionText: *q.QuestionText,
			Subject:      q.Subject,
			Labels:       labels,
			PromptHash:   promptHash,
		})
	}

	// Write debug artifact (non-fatal if fails)
	if err := s.writeClassificationArtifact(documentID, artifacts); err != nil {
		slog.Warn("extraction: write classification artifact failed", "document", documentID, "error", err)
	}

	return nil
}

// writeClassificationArtifact writes classification.json under the extraction dir
// and per-question debug files, plus mirrors to debugWriter if configured.
func (s *ExtractionService) writeClassificationArtifact(documentID string, artifacts []ClassificationArtifact) error {
	if !safeSegment(documentID) {
		return fmt.Errorf("extraction: unsafe document id %q", documentID)
	}
	if artifacts == nil {
		artifacts = []ClassificationArtifact{}
	}
	dir := filepath.Join(s.root, documentLayout, documentID, extractionDirName)
	debugDir := filepath.Join(dir, debugDirName)
	if err := os.MkdirAll(debugDir, 0o755); err != nil {
		return fmt.Errorf("extraction: create classification artifact dir: %w", err)
	}

	data, err := json.MarshalIndent(artifacts, "", "  ")
	if err != nil {
		return fmt.Errorf("extraction: encode classification artifact: %w", err)
	}
	// Primary artifact
	if err := writeFileAtomic(filepath.Join(dir, "classification.json"), append(data, '\n')); err != nil {
		return fmt.Errorf("extraction: write classification.json: %w", err)
	}
	// Debug copy
	_ = writeFileAtomic(filepath.Join(debugDir, "classification.json"), append(data, '\n'))

	// Per-question debug files mirroring question-XXX pattern
	for i, art := range artifacts {
		b, _ := json.MarshalIndent(art, "", "  ")
		name := "classification-" + pad3(i+1) + ".json"
		_ = writeFileAtomic(filepath.Join(debugDir, name), append(b, '\n'))
	}

	// Also mirror to top-level DebugWriter if configured (separate root)
	if s.debugWriter != nil {
		// Write to debug root under documentID
		debugRootDir := filepath.Join(s.debugWriter.Root(), documentID)
		_ = os.MkdirAll(debugRootDir, 0o755)
		_ = writeFileAtomic(filepath.Join(debugRootDir, "classification.json"), append(data, '\n'))
		for i, art := range artifacts {
			b, _ := json.MarshalIndent(art, "", "  ")
			name := "classification-" + pad3(i+1) + ".json"
			_ = os.WriteFile(filepath.Join(debugRootDir, name), append(b, '\n'), 0o644)
		}
	}

	return nil
}

// writeFileAtomic writes data to path through a temp file plus rename so
// partial writes are never visible at the final path, mirroring
// internal/storage/local.go.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("sync file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("finalize file: %w", err)
	}
	return nil
}

func pad3(n int) string {
	s := strconv.Itoa(n)
	for len(s) < 3 {
		s = "0" + s
	}
	return s
}
