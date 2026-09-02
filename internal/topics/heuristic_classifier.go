package topics

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
)

// HeuristicClassifier is a fallback classifier that uses keyword matching when no LLM is configured.
// It implements ClassifierInterface and is deterministic, non-network, and fast.
type HeuristicClassifier struct {
	mu         sync.Mutex
	lastPrompt string
	lastHash   string
}

// Ensure HeuristicClassifier implements ClassifierInterface.
var _ ClassifierInterface = (*HeuristicClassifier)(nil)

// NewHeuristicClassifier creates a heuristic fallback classifier.
func NewHeuristicClassifier() *HeuristicClassifier {
	return &HeuristicClassifier{}
}

// LastPromptHash returns the hash of the last prompt built.
func (h *HeuristicClassifier) LastPromptHash() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastHash
}

// LastPrompt returns the last prompt string.
func (h *HeuristicClassifier) LastPrompt() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastPrompt
}

func (h *HeuristicClassifier) recordPrompt(prompt string) string {
	hash := promptHashFor(prompt)
	h.mu.Lock()
	h.lastPrompt = prompt
	h.lastHash = hash
	h.mu.Unlock()
	return hash
}

// ClassifyQuestion classifies a question using keyword heuristics, normalizes topic names,
// dedup via CanonicalTopicKey, clamps confidence, records prompt hash.
func (h *HeuristicClassifier) ClassifyQuestion(ctx context.Context, questionText, subject string) ([]TopicLabel, error) {
	if strings.TrimSpace(questionText) == "" {
		return nil, fmt.Errorf("topics: heuristic: questionText is required")
	}
	prompt := BuildClassificationPrompt(ClassificationRequest{
		QuestionText: questionText,
		SubjectHint:  subject,
	})
	_ = h.recordPrompt(prompt)

	// Respect context cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	labels := heuristicLabels(questionText, subject)
	// Normalize and dedup via NormalizeLabels (merges by canonical key, max confidence)
	deduped := NormalizeLabels(labels)
	// Ensure hash is deterministic: also compute prompt hash via sha256 first 16 chars (already done)
	_ = sha256.Sum256([]byte(prompt))
	return deduped, nil
}

// ClassifyBatch classifies a batch of questions sequentially (heuristic is cheap).
func (h *HeuristicClassifier) ClassifyBatch(ctx context.Context, items []BatchItem) ([][]TopicLabel, error) {
	if len(items) == 0 {
		return [][]TopicLabel{}, nil
	}
	results := make([][]TopicLabel, len(items))
	for i, it := range items {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		labels, err := h.ClassifyQuestion(ctx, it.QuestionText, it.Subject)
		if err != nil {
			return results, err
		}
		results[i] = labels
	}
	return results, nil
}

// keywordRules defines heuristic matching: lowercased keyword -> topic display name + confidence
var keywordRules = []struct {
	keywords []string
	topic    string
	conf     float64
	subject  string
}{
	{keywords: []string{"deadlock", "dead lock", "banker", "circular wait", "resource allocation"}, topic: "Deadlock", conf: 0.85, subject: "Operating Systems"},
	{keywords: []string{"paging", "page fault", "page replacement", "virtual memory"}, topic: "Paging", conf: 0.82, subject: "Operating Systems"},
	{keywords: []string{"cpu scheduling", "scheduling", "fcfs", "sjf", "round robin", "preemptive"}, topic: "CPU Scheduling", conf: 0.80, subject: "Operating Systems"},
	{keywords: []string{"synchronization", "semaphore", "mutex", "monitor", "critical section", "race condition"}, topic: "Synchronization", conf: 0.80, subject: "Operating Systems"},
	{keywords: []string{"memory management", "segmentation"}, topic: "Memory Management", conf: 0.78, subject: "Operating Systems"},
	{keywords: []string{"segmentation"}, topic: "Segmentation", conf: 0.78, subject: "Operating Systems"},
	{keywords: []string{"file system", "file systems"}, topic: "File Systems", conf: 0.78, subject: "Operating Systems"},
	{keywords: []string{"process", "pcb", "context switch"}, topic: "Processes", conf: 0.70, subject: "Operating Systems"},
	{keywords: []string{"thread"}, topic: "Threads", conf: 0.70, subject: "Operating Systems"},
	{keywords: []string{"i/o", "io ", "device", "disk"}, topic: "I/O", conf: 0.68, subject: "Operating Systems"},
}

func heuristicLabels(questionText, subjectHint string) []TopicLabel {
	lower := strings.ToLower(questionText)
	var out []TopicLabel
	seen := make(map[string]bool)
	for _, rule := range keywordRules {
		for _, kw := range rule.keywords {
			if strings.Contains(lower, strings.ToLower(kw)) {
				key := CanonicalTopicKey(rule.topic)
				if seen[key] {
					break
				}
				seen[key] = true
				subj := rule.subject
				if strings.TrimSpace(subjectHint) != "" {
					subj = strings.TrimSpace(subjectHint)
				}
				out = append(out, TopicLabel{
					Topic:      rule.topic,
					Confidence: rule.conf,
					Subject:    subj,
				})
				break
			}
		}
	}
	// If no keywords matched but subjectHint suggests OS, add a generic fallback
	if len(out) == 0 && strings.TrimSpace(subjectHint) != "" {
		// Return empty to allow empty topics; alternatively add a generic topic with low confidence
		// Keep empty so ListWithCounts remains accurate and doesn't invent topics.
	}
	// Clamp confidence and normalize (though rules already normalized)
	for i := range out {
		if out[i].Confidence < 0 {
			out[i].Confidence = 0
		}
		if out[i].Confidence > 1 {
			out[i].Confidence = 1
		}
		norm := NormalizeTopicName(out[i].Topic)
		if norm != "" {
			out[i].Topic = norm
		}
	}
	return out
}
