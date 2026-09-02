package topics

import (
	"strings"
)

// aliasMap maps normalized lowercase keys to their canonical display names.
// Keys are already lowercased, whitespace-collapsed, hyphen-normalized.
var aliasMap = map[string]string{
	"deadlocks":         "Deadlock",
	"deadlock problem":  "Deadlock",
	"deadlock problems": "Deadlock",
	"dead lock":         "Deadlock",
	"dead locks":        "Deadlock",
	"dead-lock":         "Deadlock",
	"dead-locks":        "Deadlock",

	// Preserve taxonomy display names with correct capitalisation.
	"paging":            "Paging",
	"cpu scheduling":    "CPU Scheduling",
	"synchronization":   "Synchronization",
	"memory management": "Memory Management",
	"virtual memory":    "Virtual Memory",
	"segmentation":      "Segmentation",
	"file systems":      "File Systems",
	"file system":       "File Systems",
	"processes":         "Processes",
	"process":           "Processes",
	"threads":           "Threads",
	"thread":            "Threads",
	"io":                "I/O",
	"i/o":               "I/O",
}

// specialDisplay overrides title-casing for known acronyms or styled names.
var specialDisplay = map[string]string{
	"cpu": "CPU",
	"i/o": "I/O",
	"io":  "I/O",
}

// NormalizeTopicName returns the canonical display form for a raw topic string.
// It trims whitespace, lowercases, collapses internal whitespace, handles aliases
// (e.g. "Deadlocks" -> "Deadlock", "deadlock problem" -> "Deadlock"),
// applies singularization heuristics, and title-cases the result.
func NormalizeTopicName(name string) string {
	// Trim and collapse whitespace using Fields (handles spaces, tabs, newlines).
	collapsed := strings.Join(strings.Fields(name), " ")
	if collapsed == "" {
		return ""
	}
	// Normalise hyphens to spaces for alias matching (e.g. dead-lock -> dead lock).
	// We do this after Fields so hyphenated tokens are handled.
	hyphenNorm := strings.ReplaceAll(collapsed, "-", " ")
	hyphenNorm = strings.Join(strings.Fields(hyphenNorm), " ")
	lower := strings.ToLower(hyphenNorm)

	// Direct alias lookup.
	if canonical, ok := aliasMap[lower]; ok {
		return canonical
	}

	// Singularization heuristic: singularize each word in the phrase.
	singular := singularizePhrase(lower)
	if singular != lower {
		if canonical, ok := aliasMap[singular]; ok {
			return canonical
		}
		lower = singular
	}

	// If alias found after singularization, already returned; otherwise build display name.
	// Check alias once more for the singularized form that may itself be an alias target
	// that we already checked — but if the singularized form maps to a canonical
	// via title-casing equivalence, aliasMap handles it.
	if canonical, ok := aliasMap[lower]; ok {
		return canonical
	}

	return toDisplayName(lower)
}

// CanonicalTopicKey returns a lowercased deduplication/merging key for a topic name.
// Two names that normalize to the same topic have the same key, e.g.:
// CanonicalTopicKey("Deadlocks") == CanonicalTopicKey("deadlock") == "deadlock"
func CanonicalTopicKey(name string) string {
	normalized := NormalizeTopicName(name)
	if normalized == "" {
		return ""
	}
	return strings.ToLower(normalized)
}

// NormalizeLabels deduplicates a slice of TopicLabel by canonical topic key,
// merging confidence with max. Empty or whitespace-only names are dropped.
// The returned slice contains normalized display names (in Topic field).
func NormalizeLabels(labels []TopicLabel) []TopicLabel {
	m := make(map[string]*TopicLabel)
	order := make([]string, 0)

	for _, l := range labels {
		normName := NormalizeTopicName(l.Topic)
		if normName == "" {
			continue
		}
		key := CanonicalTopicKey(normName)
		if key == "" {
			key = strings.ToLower(normName)
		}
		if existing, ok := m[key]; ok {
			// merge confidence with max; keep subject from highest confidence if present
			if l.Confidence > existing.Confidence {
				existing.Confidence = l.Confidence
				if l.Subject != "" {
					existing.Subject = l.Subject
				}
			} else if existing.Subject == "" && l.Subject != "" {
				existing.Subject = l.Subject
			}
			continue
		}
		// new entry — preserve display name from NormalizeTopicName
		cp := TopicLabel{Topic: normName, Confidence: l.Confidence, Subject: l.Subject}
		m[key] = &cp
		order = append(order, key)
	}

	out := make([]TopicLabel, 0, len(order))
	for _, k := range order {
		out = append(out, *m[k])
	}
	return out
}

// MergeCandidates groups topics by canonical key and returns only groups with
// more than one topic (candidates for merging). Topics with empty names are ignored.
func MergeCandidates(topics []Topic) map[string][]Topic {
	groups := make(map[string][]Topic)
	for _, t := range topics {
		key := CanonicalTopicKey(t.Name)
		if key == "" {
			continue
		}
		groups[key] = append(groups[key], t)
	}
	candidates := make(map[string][]Topic)
	for k, v := range groups {
		if len(v) > 1 {
			candidates[k] = v
		}
	}
	return candidates
}

// singularizePhrase singularizes each word in a lowercased phrase.
func singularizePhrase(phrase string) string {
	words := strings.Fields(phrase)
	for i, w := range words {
		words[i] = singularizeWord(w)
	}
	return strings.Join(words, " ")
}

// singularizeWord applies simple English singularization heuristics.
func singularizeWord(w string) string {
	if len(w) <= 3 {
		return w
	}
	low := w // already lowercased
	// Handle "ies" -> "y" (e.g. factories -> factory)
	if strings.HasSuffix(low, "ies") && len(low) > 4 {
		return low[:len(low)-3] + "y"
	}
	// Handle common "es" plurals: ses, xes, ches, shes
	if strings.HasSuffix(low, "ses") || strings.HasSuffix(low, "xes") || strings.HasSuffix(low, "ches") || strings.HasSuffix(low, "shes") {
		return low[:len(low)-2]
	}
	// General trailing "s" (avoid words ending in "ss", "us", "is")
	if strings.HasSuffix(low, "s") && !strings.HasSuffix(low, "ss") && !strings.HasSuffix(low, "us") && !strings.HasSuffix(low, "is") {
		return low[:len(low)-1]
	}
	return low
}

// toDisplayName converts a lowercased phrase to Title Case with special handling.
func toDisplayName(lower string) string {
	words := strings.Fields(lower)
	for i, w := range words {
		if special, ok := specialDisplay[w]; ok {
			words[i] = special
			continue
		}
		words[i] = titleWord(w)
	}
	return strings.Join(words, " ")
}

func titleWord(w string) string {
	if w == "" {
		return w
	}
	return strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
}
