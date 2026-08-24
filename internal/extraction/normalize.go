package extraction

import "strings"

// normalizeText cleans up raw PDF text while keeping the structure that
// matters for question extraction: line breaks, spacing and question
// numbering.
//
//   - CRLF/CR are normalized to LF.
//   - Runs of spaces/tabs collapse to a single space and lines are trimmed.
//   - Blank lines collapse to a single blank line; leading and trailing blank
//     lines are removed.
func normalizeText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = collapseSpaces(line)
		out = append(out, line)
	}

	// Collapse consecutive blank lines into one and trim outer blanks.
	result := make([]string, 0, len(out))
	for _, line := range out {
		if line == "" && (len(result) == 0 || result[len(result)-1] == "") {
			continue
		}
		result = append(result, line)
	}
	if len(result) > 0 && result[len(result)-1] == "" {
		result = result[:len(result)-1]
	}
	return strings.Join(result, "\n")
}

// collapseSpaces trims the line and collapses internal runs of spaces and
// tabs to one space.
func collapseSpaces(line string) string {
	var b strings.Builder
	b.Grow(len(line))
	lastSpace := true // also drops leading spaces
	for _, r := range line {
		if r == ' ' || r == '\t' {
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
			continue
		}
		b.WriteRune(r)
		lastSpace = false
	}
	return strings.TrimRight(b.String(), " ")
}
