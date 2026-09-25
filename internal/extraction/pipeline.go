package extraction

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/suryanshu-09/holy_grail/internal/documents"
	"github.com/suryanshu-09/holy_grail/internal/embeddings"
	"github.com/suryanshu-09/holy_grail/internal/observability"
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
	embeddings   *embeddings.Service
	vision       VisionDescriber
	stepLog      *observability.StepLogger
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

// WithEmbeddingPipeline attaches the optional Phase 10 embedding pipeline.
func (s *ExtractionService) WithEmbeddingPipeline(pipeline *embeddings.Service) *ExtractionService {
	s.embeddings = pipeline
	return s
}

// WithVisionDescriber attaches the optional vision backend used to describe
// extracted images after extractImages. It is nil-safe: a nil describer
// disables vision (same as never calling this method) and never fails the
// pipeline. It returns the service so callers can chain.
func (s *ExtractionService) WithVisionDescriber(v VisionDescriber) *ExtractionService {
	if v == nil {
		return s
	}
	s.vision = v
	return s
}

// WithStepLogger attaches the structured step logger used for PLAN4 Phase 23
// observability. A nil logger disables step logging (slog diagnostics still
// apply). Job/document correlation is read from ctx via
// observability.JobIDFromContext.
func (s *ExtractionService) WithStepLogger(l *observability.StepLogger) *ExtractionService {
	s.stepLog = l
	return s
}

// steps returns the configured step logger, falling back to slog.Default().
func (s *ExtractionService) steps() *observability.StepLogger {
	if s != nil && s.stepLog != nil {
		return s.stepLog
	}
	return observability.NewStepLogger(slog.Default())
}

// stepBase builds the correlation base for one document from ctx (job_id)
// plus the explicit document id.
func (s *ExtractionService) stepBase(ctx context.Context, documentID string) observability.StepEntry {
	base := observability.BaseFromContext(ctx)
	base.DocumentID = documentID
	return base
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
	base := s.stepBase(ctx, documentID)
	finishOverall := s.steps().Start(ctx, "document_extraction", base)
	overallErr := error(nil)
	questionCount := 0
	defer func() {
		extra := map[string]any{"questions": questionCount}
		finishOverall(overallErr, extra)
	}()

	pdfPath, err := s.resolvePDF(documentID, storagePath)
	if err != nil {
		overallErr = err
		return DocumentExtraction{}, err
	}

	finishPages := s.steps().Start(ctx, "page_extraction", base)
	result, err := s.extractor.ExtractFile(ctx, documentID, pdfPath)
	finishPages(err, map[string]any{"pages": len(result.Pages)})
	if err != nil {
		overallErr = err
		return DocumentExtraction{}, s.markFailed(ctx, documentID, err)
	}

	// Vision descriptions are best-effort and non-fatal: describe extracted
	// images after extractImages so pages.json/images.json manifests carry
	// description + figure-type context. Failures are logged and skipped.
	finishVision := s.steps().Start(ctx, "vision_describe", base)
	s.describeImages(ctx, documentID, &result)
	finishVision(nil, map[string]any{"pages": len(result.Pages)})

	finishSave := s.steps().Start(ctx, "save_results", base)
	saveErr := s.saveResults(documentID, result)
	finishSave(saveErr, map[string]any{"pages": len(result.Pages)})
	if saveErr != nil {
		overallErr = saveErr
		return DocumentExtraction{}, s.markFailed(ctx, documentID, saveErr)
	}

	// Run question extraction and persist detected questions. Failures in
	// question persistence mark the document failed so they are visible to
	// operators and can be retried.
	if err := s.ExtractQuestions(ctx, result); err != nil {
		overallErr = err
		return DocumentExtraction{}, s.markFailed(ctx, documentID, err)
	}
	questionCount = s.countPersistedQuestions(ctx, documentID)

	// Topic classification is non-fatal: log and continue on LLM failure.
	if s.classifier != nil && s.topicRepo != nil {
		if err := s.ClassifyDocument(ctx, documentID); err != nil {
			slog.Warn("extraction: classification failed (non-fatal)", "document_id", documentID, "job_id", base.JobID, "error", err)
		}
	} else {
		slog.Debug("extraction: classification skipped (no classifier or topic repo)", "document_id", documentID, "job_id", base.JobID)
		// Still write empty classification artifact for observability
		_ = s.writeClassificationArtifact(documentID, nil)
		s.steps().Log(ctx, observability.StepEntry{
			DocumentID: documentID, JobID: base.JobID,
			Operation: "topic_classification", Status: observability.StatusSuccess,
			Extra: map[string]any{"skipped": true},
		})
	}

	if s.embeddings != nil {
		finishEmbed := s.steps().Start(ctx, "embedding", base)
		embeddingResult, err := s.embeddings.EmbedDocument(ctx, documentID)
		if err != nil {
			finishEmbed(err, nil)
			slog.Warn("extraction: embedding failed (non-fatal)", "document_id", documentID, "job_id", base.JobID, "error", err)
		} else {
			finishEmbed(nil, map[string]any{"failed": embeddingResult.Failed})
			if embeddingResult.Failed > 0 {
				slog.Warn("extraction: embedding partially completed", "document_id", documentID, "job_id", base.JobID, "failed", embeddingResult.Failed)
			}
		}
	} else {
		slog.Debug("extraction: embeddings skipped (no embedder configured)", "document_id", documentID, "job_id", base.JobID)
	}

	if err := s.repo.UpdateStatus(ctx, documentID, documents.StatusExtracted); err != nil {
		overallErr = fmt.Errorf("extraction: mark %q extracted: %w", documentID, err)
		return DocumentExtraction{}, overallErr
	}
	return result, nil
}

// countPersistedQuestions returns the number of stored questions for a
// document for step-log extras. Errors yield 0 (never fatal).
func (s *ExtractionService) countPersistedQuestions(ctx context.Context, documentID string) int {
	if s.questionRepo == nil {
		return 0
	}
	n := 0
	for offset := 0; ; {
		batch, err := s.questionRepo.List(ctx, questions.Filter{DocumentID: documentID, Limit: 100, Offset: offset})
		if err != nil || len(batch) == 0 {
			break
		}
		n += len(batch)
		if len(batch) < 100 {
			break
		}
		offset += len(batch)
	}
	return n
}

// visionCacheFileName holds content-hash -> DescribeOutput entries so
// re-extraction and duplicate images never re-call the vision backend.
const visionCacheFileName = "vision_cache.json"

// describeImages fills Description/FigureType/DescribedBy on every image in
// result using the configured VisionDescriber. It is best-effort and never
// returns an error: a nil describer is a no-op, per-image failures are logged
// and skipped, and cache I/O failures are ignored. Results are cached by
// SHA-256 of the image file bytes (persisted per document) so repeated runs
// are cheap and deterministic.
func (s *ExtractionService) describeImages(ctx context.Context, documentID string, result *DocumentExtraction) {
	if s.vision == nil || result == nil {
		return
	}
	hasImages := false
	for _, pg := range result.Pages {
		if len(pg.Images) > 0 {
			hasImages = true
			break
		}
	}
	if !hasImages {
		return
	}

	cache := s.loadVisionCache(documentID)
	dirty := false

	// Build page-text context for grounding (truncated per image).
	pageText := make(map[int]string, len(result.Pages))
	for _, pg := range result.Pages {
		pageText[pg.Number] = pg.Text
	}

	for pi := range result.Pages {
		for ii := range result.Pages[pi].Images {
			if err := ctx.Err(); err != nil {
				slog.Warn("extraction: vision describe stopped (context cancelled, non-fatal)", "document_id", documentID, "error", err)
				break
			}
			img := &result.Pages[pi].Images[ii]
			if strings.TrimSpace(img.Description) != "" {
				continue
			}
			absPath := s.imageAbsPath(documentID, *img)
			hash := hashFileSHA256(absPath)
			if hash == "" {
				// Fall back to a stable synthetic key so identical names
				// still share cache entries within/across runs.
				hash = "name:" + img.Name
			}
			if cached, ok := cache[hash]; ok {
				img.Description = cached.Description
				img.FigureType = NormalizeFigureType(cached.FigureType)
				img.DescribedBy = cached.DescribedBy
				continue
			}
			out, err := s.vision.Describe(ctx, DescribeInput{
				Name:   img.Name,
				Page:   img.Page,
				Path:   absPath,
				Format: img.Format,
				// Prefer the image's own page text; fall back to the page
				// currently being iterated when Page is unset (0).
				Context: truncateRunes(firstNonEmpty(pageText[img.Page], result.Pages[pi].Text), 500),
			})
			if err != nil {
				slog.Warn("extraction: vision describe failed (non-fatal)", "document_id", documentID, "image", img.Name, "error", err)
				continue
			}
			out.FigureType = NormalizeFigureType(out.FigureType)
			img.Description = strings.TrimSpace(out.Description)
			img.FigureType = out.FigureType
			img.DescribedBy = out.DescribedBy
			cache[hash] = out
			dirty = true
		}
	}

	if dirty {
		s.saveVisionCache(documentID, cache)
	}
}

// imageAbsPath resolves an ImageRef to its absolute file path on disk.
func (s *ExtractionService) imageAbsPath(documentID string, img ImageRef) string {
	if img.StoragePath != "" {
		p := filepath.Join(s.root, filepath.FromSlash(img.StoragePath))
		if withinRoot(s.root, p) {
			return p
		}
	}
	if img.Name != "" {
		return filepath.Join(s.root, documentLayout, documentID, imagesDirName, img.Name)
	}
	return ""
}

// loadVisionCache reads the persisted hash->DescribeOutput map for a
// document. Missing or corrupt cache files yield an empty map (never error).
func (s *ExtractionService) loadVisionCache(documentID string) map[string]DescribeOutput {
	cache := make(map[string]DescribeOutput)
	if !safeSegment(documentID) {
		return cache
	}
	data, err := os.ReadFile(filepath.Join(s.root, documentLayout, documentID, extractionDirName, visionCacheFileName))
	if err != nil {
		return cache
	}
	_ = json.Unmarshal(data, &cache)
	if cache == nil {
		cache = make(map[string]DescribeOutput)
	}
	return cache
}

// saveVisionCache persists the hash->DescribeOutput map. Failures are
// best-effort and only logged.
func (s *ExtractionService) saveVisionCache(documentID string, cache map[string]DescribeOutput) {
	if !safeSegment(documentID) {
		return
	}
	dir := filepath.Join(s.root, documentLayout, documentID, extractionDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Warn("extraction: create vision cache dir failed (non-fatal)", "document_id", documentID, "error", err)
		return
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return
	}
	if err := writeFileAtomic(filepath.Join(dir, visionCacheFileName), append(data, '\n')); err != nil {
		slog.Warn("extraction: write vision cache failed (non-fatal)", "document_id", documentID, "error", err)
	}
}

// hashFileSHA256 returns the hex SHA-256 of the file at path, or "" when the
// file cannot be read.
func hashFileSHA256(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// firstNonEmpty returns the first non-blank string, or "" when all are blank.
func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// truncateRunes clips s to at most n runes.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
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
// positions, storage paths, and optional vision fields (description,
// figure_type, described_by) carried on ImageRef.
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
	base := s.stepBase(ctx, documentID)
	finish := s.steps().Start(ctx, "topic_classification", base)
	classifyErr := error(nil)
	labeled := 0
	defer func() {
		finish(classifyErr, map[string]any{"labeled": labeled})
	}()
	if s.classifier == nil {
		classifyErr = fmt.Errorf("extraction: no classifier configured")
		return classifyErr
	}
	if s.topicRepo == nil {
		classifyErr = fmt.Errorf("extraction: no topic repo configured")
		return classifyErr
	}
	if s.questionRepo == nil {
		classifyErr = fmt.Errorf("extraction: no questions repository configured")
		return classifyErr
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
			classifyErr = fmt.Errorf("extraction: list questions for classification: %w", err)
			return classifyErr
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
		slog.Info("extraction: no questions to classify", "document_id", documentID, "job_id", base.JobID)
		if werr := s.writeClassificationArtifact(documentID, nil); werr != nil {
			classifyErr = werr
			return classifyErr
		}
		return nil
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
		slog.Warn("extraction: write classification artifact failed", "document_id", documentID, "job_id", base.JobID, "error", err)
	}
	for _, art := range artifacts {
		if art.Error == "" {
			labeled++
		}
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
