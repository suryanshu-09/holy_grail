package extraction

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/suryanshu-09/holy_grail/internal/topics"
)

// extractConcurrency returns the bounded worker-pool size for CPU-bound and
// LLM/vision fan-out. It defaults to runtime.NumCPU and honors the
// EXTRACT_CONCURRENCY environment override. The result is always >= 1 and
// is nil-safe (a zero Service still yields a sane default).
func extractConcurrency() int {
	if raw := strings.TrimSpace(os.Getenv("EXTRACT_CONCURRENCY")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			if n > 64 {
				return 64
			}
			return n
		}
	}
	n := runtime.NumCPU()
	if n < 1 {
		return 1
	}
	if n > 64 {
		return 64
	}
	return n
}

// ContentHashBytes returns the hex SHA-256 of b.
func ContentHashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ContentHashFile returns the hex SHA-256 of the file at path, or "" when
// the file cannot be read. It never returns an error so callers can treat
// a missing hash as "unknown, do not skip".
func ContentHashFile(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return ""
	}
	return ContentHashBytes(data)
}

// contentHashString hashes an arbitrary string (used for prompt dedup keys).
func contentHashString(s string) string {
	return ContentHashBytes([]byte(s))
}

// manifestFileName records the source PDF hash so unchanged documents can
// skip re-extraction. It lives next to pages.json.
const manifestFileName = "manifest.json"

// timingsFileName holds per-step processing durations for observability.
const timingsFileName = "timings.json"

// classifierCacheFileName holds prompt-hash -> labels entries so duplicate
// question texts never re-call the LLM, within and across runs.
const classifierCacheFileName = "classifier_cache.json"

// Manifest records which source content produced the cached extraction.
type Manifest struct {
	ContentHash string `json:"content_hash"`
	PageCount   int    `json:"page_count"`
}

// ProcessingDurations captures per-step wall-clock timings for one pipeline
// run. All fields are milliseconds (int64) plus skip flags so the artifact
// is JSON-stable and easy to graph.
type ProcessingDurations struct {
	DocumentID        string `json:"document_id"`
	ContentHash       string `json:"content_hash,omitempty"`
	PageExtractionMs  int64  `json:"page_extraction_ms"`
	VisionMs          int64  `json:"vision_ms"`
	SaveMs            int64  `json:"save_ms"`
	QuestionMs        int64  `json:"question_ms"`
	ClassificationMs  int64  `json:"classification_ms"`
	EmbeddingMs       int64  `json:"embedding_ms"`
	TotalMs           int64  `json:"total_ms"`
	Skipped           bool   `json:"skipped"`
	SkippedExtraction bool   `json:"skipped_extraction,omitempty"`
}

// toExtra maps durations to StepLogger extras (all values numeric).
func (d ProcessingDurations) toExtra() map[string]any {
	return map[string]any{
		"page_extraction_ms": d.PageExtractionMs,
		"vision_ms":          d.VisionMs,
		"save_ms":            d.SaveMs,
		"question_ms":        d.QuestionMs,
		"classification_ms":  d.ClassificationMs,
		"embedding_ms":       d.EmbeddingMs,
		"total_ms":           d.TotalMs,
		"skipped":            d.Skipped,
		"skipped_extraction": d.SkippedExtraction,
	}
}

// manifestPath returns the manifest path for a document ("" on unsafe id).
func (s *ExtractionService) manifestPath(documentID string) string {
	if s == nil || !safeSegment(documentID) {
		return ""
	}
	return filepath.Join(s.root, documentLayout, documentID, extractionDirName, manifestFileName)
}

// timingsPath returns the timings artifact path ("" on unsafe id).
func (s *ExtractionService) timingsPath(documentID string) string {
	if s == nil || !safeSegment(documentID) {
		return ""
	}
	return filepath.Join(s.root, documentLayout, documentID, extractionDirName, timingsFileName)
}

// readManifest loads the cached manifest, or nil when absent/corrupt.
func (s *ExtractionService) readManifest(documentID string) *Manifest {
	p := s.manifestPath(documentID)
	if p == "" {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	if m.ContentHash == "" {
		return nil
	}
	return &m
}

// writeManifest persists the source hash after a successful save. Failures
// are best-effort and never fail the pipeline.
func (s *ExtractionService) writeManifest(documentID, contentHash string, pageCount int) {
	p := s.manifestPath(documentID)
	if p == "" || contentHash == "" {
		return
	}
	data, err := json.MarshalIndent(Manifest{ContentHash: contentHash, PageCount: pageCount}, "", "  ")
	if err != nil {
		return
	}
	_ = writeFileAtomic(p, append(data, '\n'))
}

// loadCachedExtraction returns the previously saved pages.json when it
// exists and decodes cleanly, or nil otherwise. It never returns an error.
func (s *ExtractionService) loadCachedExtraction(documentID string) *DocumentExtraction {
	if s == nil || !safeSegment(documentID) {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(s.root, documentLayout, documentID, extractionDirName, pagesFileName))
	if err != nil {
		return nil
	}
	var cached DocumentExtraction
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil
	}
	if cached.DocumentID == "" {
		cached.DocumentID = documentID
	}
	return &cached
}

// shouldSkipExtraction reports whether the cached extraction can be reused:
// the PDF hash must be non-empty and match the stored manifest, and a
// decodable pages.json must exist. It never errors; false means "extract".
func (s *ExtractionService) shouldSkipExtraction(documentID, contentHash string) (*DocumentExtraction, bool) {
	if s == nil || contentHash == "" || !safeSegment(documentID) {
		return nil, false
	}
	m := s.readManifest(documentID)
	if m == nil || m.ContentHash != contentHash {
		return nil, false
	}
	cached := s.loadCachedExtraction(documentID)
	if cached == nil {
		return nil, false
	}
	return cached, true
}

// writeTimings persists the durations artifact. Failures are best-effort.
func (s *ExtractionService) writeTimings(d ProcessingDurations) {
	if s == nil {
		return
	}
	p := s.timingsPath(d.DocumentID)
	if p == "" {
		return
	}
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return
	}
	_ = writeFileAtomic(p, append(data, '\n'))
}

// msSince returns whole milliseconds elapsed since start (nil-safe on
// negative durations).
func msSince(start time.Time) int64 {
	ms := time.Since(start).Milliseconds()
	if ms < 0 {
		return 0
	}
	return ms
}

// classifyCacheKey builds the deterministic dedup key for one classification
// input: sha256 hex (first 32 chars) of subject + question text.
func classifyCacheKey(questionText, subject string) string {
	h := contentHashString(strings.TrimSpace(subject) + "\x00" + strings.TrimSpace(questionText))
	if len(h) > 32 {
		return h[:32]
	}
	return h
}

// loadClassifierCache reads the persisted prompt-hash -> labels map.
// Missing or corrupt files yield an empty map (never error).
func (s *ExtractionService) loadClassifierCache(documentID string) map[string][]topics.TopicLabel {
	cache := make(map[string][]topics.TopicLabel)
	if s == nil || !safeSegment(documentID) {
		return cache
	}
	data, err := os.ReadFile(filepath.Join(s.root, documentLayout, documentID, extractionDirName, classifierCacheFileName))
	if err != nil {
		return cache
	}
	_ = json.Unmarshal(data, &cache)
	if cache == nil {
		cache = make(map[string][]topics.TopicLabel)
	}
	return cache
}

// saveClassifierCache persists the prompt-hash -> labels map (best-effort).
func (s *ExtractionService) saveClassifierCache(documentID string, cache map[string][]topics.TopicLabel) {
	if s == nil || !safeSegment(documentID) || len(cache) == 0 {
		return
	}
	dir := filepath.Join(s.root, documentLayout, documentID, extractionDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return
	}
	_ = writeFileAtomic(filepath.Join(dir, classifierCacheFileName), append(data, '\n'))
}

// promptHashShort is a local alias for the classifier's prompt-hash scheme
// (sha256 hex, first 16 chars) used for artifact observability.
func promptHashShort(s string) string {
	h := contentHashString(s)
	if len(h) > 16 {
		return h[:16]
	}
	return fmt.Sprintf("%-16s", h)
}
