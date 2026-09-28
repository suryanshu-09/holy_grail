// Package multimodal provides deterministic, dependency-free helpers for
// multimodal understanding: image embedding stubs, diagram classification,
// table parsing/summarization, and math expression extraction/normalization.
//
// Every function in this package is pure: no I/O, no network, no database,
// no global state. Callers (quiz fallback, extraction display) use these
// helpers to enrich deterministic output without changing any API shapes.
package multimodal

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
)

// Diagram-kind constants returned by ClassifyDiagram.
const (
	DiagramKindFlowchart = "flowchart"
	DiagramKindCircuit   = "circuit"
	DiagramKindGraph     = "graph"
	DiagramKindChart     = "chart"
	DiagramKindTable     = "table"
	DiagramKindGeometry  = "geometry"
	DiagramKindNetwork   = "network"
	DiagramKindTimeline  = "timeline"
	DiagramKindGeneral   = "general_diagram"
	DiagramKindUnknown   = "unknown"
)

// DescribeImage returns a deterministic stand-in description for an image.
//
// When visionDescription is non-empty (a vision model already described the
// figure), it is trimmed and passed through unchanged. Otherwise a stable
// stub is built from the image name plus a SHA-256 content hash of the raw
// bytes, so the same image always yields the same description and distinct
// images yield distinct descriptions. It never fails.
func DescribeImage(name string, data []byte, visionDescription string) string {
	if s := strings.TrimSpace(visionDescription); s != "" {
		return s
	}
	n := strings.TrimSpace(name)
	if n == "" {
		n = "unnamed"
	}
	sum := sha256.Sum256(append([]byte(n+":"), data...))
	return fmt.Sprintf("Undescribed image %s (content hash %x, %d bytes; vision unavailable)", n, sum[:4], len(data))
}

// EmbedImageText builds the deterministic text representation of a figure
// for embedding: "Figure <name> (<figure_type>): <description>", degrading
// gracefully when the type and/or description are missing. Empty names fall
// back to "unnamed figure". The output is stable for identical inputs.
func EmbedImageText(name, figureType, description string) string {
	n := strings.TrimSpace(name)
	if n == "" {
		n = "unnamed figure"
	}
	ft := strings.ToLower(strings.TrimSpace(figureType))
	d := strings.TrimSpace(description)
	switch {
	case ft != "" && d != "":
		return fmt.Sprintf("Figure %s (%s): %s", n, ft, d)
	case ft != "":
		return fmt.Sprintf("Figure %s (%s)", n, ft)
	case d != "":
		return fmt.Sprintf("Figure %s: %s", n, d)
	default:
		return fmt.Sprintf("Figure %s", n)
	}
}

// diagramRules maps a diagram sub-kind to its keyword triggers. Rules are
// evaluated in order; the first rule whose keyword appears in the combined
// "figure_type name description" text wins. Keywords are matched
// case-insensitively on the lowercased haystack.
var diagramRules = []struct {
	kind     string
	keywords []string
}{
	{DiagramKindFlowchart, []string{"flowchart", "flow chart", "flow-chart", "flow diagram"}},
	{DiagramKindCircuit, []string{"circuit", "resistor", "capacitor", "inductor", "transistor", "diode", "op-amp", "opamp", "breadboard", "kirchhoff", "logic gate"}},
	{DiagramKindChart, []string{"bar chart", "bar-chart", "barchart", "pie chart", "pie-chart", "piechart", "histogram", "line chart", "line-chart", "bar graph", "pie diagram"}},
	{DiagramKindGraph, []string{"graph", "plot", "axes", "axis", "scatter", "throughput", "saturation", "curve", "vs load", "vs. load", "x-y", "xy plot"}},
	{DiagramKindTable, []string{"table", "tabular", "burst time", "rows and columns", "rows", "columns", "spreadsheet", "gantt table"}},
	{DiagramKindGeometry, []string{"triangle", "pythagoras", "circle", "angle", "geometry", "geometric", "polygon", "theorem", "proof", "rectangle", "quadrilateral", "pentagon", "hexagon"}},
	{DiagramKindNetwork, []string{"network", "topology", "router", "switch", "uml", "class diagram", "sequence diagram", "er diagram", "entity-relationship", "entity relationship", "use case", "state machine", "data flow"}},
	{DiagramKindTimeline, []string{"timeline", "gantt", "roadmap", "chronology"}},
}

// ClassifyDiagram heuristically classifies a figure into a diagram sub-kind
// (flowchart, circuit, graph, chart, table, geometry, network, timeline,
// general_diagram, unknown) from the figure_type, image name, and vision
// description keywords. Matching is case-insensitive and deterministic: the
// first matching rule in priority order wins. Single-word keywords match on
// token boundaries (so "photograph" does not trigger the "graph" rule);
// multi-word phrases match as substrings.
//
// When no keyword matches, a non-empty figure_type is passed through
// lowercased (so photos stay "photo" and plain diagrams become
// "general_diagram"); fully empty input yields "unknown".
func ClassifyDiagram(figureType, name, description string) string {
	hay := normalizeHaystack(strings.Join([]string{figureType, name, description}, " "))
	tokens := tokenSet(hay)
	for _, r := range diagramRules {
		for _, kw := range r.keywords {
			norm := normalizeHaystack(kw)
			if strings.Contains(norm, " ") {
				if strings.Contains(hay, norm) {
					return r.kind
				}
			} else if tokens[norm] {
				return r.kind
			}
		}
	}
	ft := strings.ToLower(strings.TrimSpace(figureType))
	switch ft {
	case "", "unknown", "none", "n/a", "other":
		return DiagramKindUnknown
	case "diagram", "diagrams":
		return DiagramKindGeneral
	default:
		return ft
	}
}

// markdownSeparatorRe matches markdown table separator cells like "---",
// ":---:", "| --- | --- |" fragments are handled per-cell by
// isSeparatorRow after splitting.
var tableSeparatorCellRe = regexp.MustCompile(`^:?-{2,}:?$`)

// isSeparatorRow reports whether every cell looks like a markdown alignment
// marker (---, :---, ---:, :---:), in which case the row carries no data.
func isSeparatorRow(cells []string) bool {
	if len(cells) == 0 {
		return true
	}
	for _, c := range cells {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if !tableSeparatorCellRe.MatchString(c) {
			return false
		}
	}
	return true
}

// splitTableLine splits one text line into cells. Pipes take precedence
// (markdown tables), then tabs, then commas (CSV-ish). It reports false when
// the line carries no delimiter and therefore is prose, not a table row.
func splitTableLine(line string) ([]string, bool) {
	switch {
	case strings.Contains(line, "|"):
		parts := strings.Split(line, "|")
		cells := make([]string, 0, len(parts))
		for _, p := range parts {
			cells = append(cells, strings.TrimSpace(p))
		}
		// Drop the empty fringes produced by leading/trailing pipes.
		for len(cells) > 0 && cells[0] == "" {
			cells = cells[1:]
		}
		for len(cells) > 0 && cells[len(cells)-1] == "" {
			cells = cells[:len(cells)-1]
		}
		return cells, true
	case strings.Contains(line, "\t"):
		parts := strings.Split(line, "\t")
		cells := make([]string, 0, len(parts))
		for _, p := range parts {
			cells = append(cells, strings.TrimSpace(p))
		}
		return cells, true
	case strings.Contains(line, ","):
		parts := strings.Split(line, ",")
		cells := make([]string, 0, len(parts))
		for _, p := range parts {
			cells = append(cells, strings.TrimSpace(p))
		}
		return cells, true
	default:
		return nil, false
	}
}

// ParseTableText extracts rows and cells from markdown/CSV-ish table text.
// Each output row is a slice of trimmed cell strings. Blank lines, markdown
// separator rows (|---|---|), all-empty rows, and lines without any table
// delimiter (|, tab, comma) are skipped. It never returns nil for
// table-bearing input and returns an empty (non-nil) slice when no table
// rows are found. It never fails.
func ParseTableText(text string) [][]string {
	rows := [][]string{}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" {
			continue
		}
		cells, ok := splitTableLine(line)
		if !ok {
			continue
		}
		empty := true
		for _, c := range cells {
			if strings.TrimSpace(c) != "" {
				empty = false
				break
			}
		}
		if empty || isSeparatorRow(cells) {
			continue
		}
		rows = append(rows, cells)
	}
	return rows
}

// truncateCell shortens one cell for summaries.
func truncateCell(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

// SummarizeTable renders a one-line human-readable summary of parsed table
// rows: dimensions, header names, and a preview of the first data row.
// Empty input yields "Empty table.". Column width is normalized to the
// widest row. It never fails.
func SummarizeTable(rows [][]string) string {
	if len(rows) == 0 {
		return "Empty table."
	}
	cols := 0
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Table with %d row%s x %d column%s.", len(rows), plural(len(rows)), cols, plural(cols))
	headers := make([]string, 0, len(rows[0]))
	for _, h := range rows[0] {
		h = strings.TrimSpace(h)
		if h == "" {
			h = "(blank)"
		}
		headers = append(headers, truncateCell(h, 40))
	}
	fmt.Fprintf(&b, " Headers: %s.", strings.Join(headers, ", "))
	if len(rows) > 1 {
		first := make([]string, 0, len(rows[1]))
		for _, c := range rows[1] {
			first = append(first, truncateCell(c, 40))
		}
		fmt.Fprintf(&b, " First row: %s.", strings.Join(first, ", "))
		if len(rows) > 2 {
			fmt.Fprintf(&b, " (+%d more row%s.)", len(rows)-2, plural(len(rows)-2))
		}
	}
	return b.String()
}

// normalizeHaystack lowercases text and folds hyphens/underscores to spaces
// so hyphenated variants ("flow-chart") match their spaced forms.
func normalizeHaystack(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")
	return s
}

// tokenSet splits normalized text into a set of alphanumeric tokens.
func tokenSet(s string) map[string]bool {
	set := make(map[string]bool)
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool {
		return r < 'a' || r > 'z' // normalized haystack is lowercase ASCII folds
	}) {
		if tok != "" {
			set[tok] = true
		}
	}
	return set
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// mathDelimRe matches delimited math and LaTeX fragments in document order:
//   - $$...$$ display math
//   - $...$ inline math (single line)
//   - \[...\] display math and \(...\) inline math
//   - \begin{<env>}...\end{<env>} blocks (dot matches newlines via (?s:...))
//   - standalone LaTeX commands (\frac{a}{b}, \sqrt{x}, \sum, \int, ...)
var mathDelimRe = regexp.MustCompile(`\$\$[^$]+?\$\$|\$[^$\n]+?\$|\\\[.+?\\\]|\\\(.+?\\\)|\\begin\{[a-zA-Z*]+\}(?s:.+?)\\end\{[a-zA-Z*]+\}|\\(frac|sqrt|sum|int|lim|infty|alpha|beta|gamma|delta|theta|lambda|pi|sigma|omega|times|div|leq|geq|neq|approx|partial|nabla|in|notin|forall|exists)(?:\{[^{}]*\}){0,2}`)

// mathLineHintRe requires an "=" line to also carry a math-ish token
// (digit, backslash command, exponent/operator) before it counts as an
// equation, keeping prose with "=" (e.g. "a = b" in plain words is still
// math, but "name = value" notes are excluded unless numeric/symbolic).
var mathLineHintRe = regexp.MustCompile(`[0-9\\^_+\-*/|<>]`)

// ExtractMathExpressions extracts math expressions from text in document
// order: $...$ and $$...$$ segments, \(...\) / \[...\] segments,
// \begin{...}...\end{...} LaTeX blocks, standalone LaTeX commands, and
// equation lines containing "=". Duplicates are removed (by normalized
// form) keeping the first occurrence; matches are trimmed. It returns an
// empty (non-nil) slice when no math is found. It never fails.
func ExtractMathExpressions(text string) []string {
	out := []string{}
	seen := make(map[string]struct{})
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		k := NormalizeMath(s)
		if k == "" {
			return
		}
		if _, dup := seen[k]; dup {
			return
		}
		seen[k] = struct{}{}
		out = append(out, s)
	}
	for _, m := range mathDelimRe.FindAllString(text, -1) {
		add(m)
	}
	// Equation lines: only when the line is short enough to be an equation
	// rather than a paragraph that happens to contain "=".
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || len(t) > 300 || !strings.Contains(t, "=") {
			continue
		}
		if !mathLineHintRe.MatchString(t) {
			continue
		}
		// Skip lines already covered by a delimited match.
		covered := false
		for _, m := range out {
			if strings.Contains(m, t) || strings.Contains(t, m) {
				covered = true
				break
			}
		}
		if !covered {
			add(t)
		}
	}
	return out
}

// NormalizeMath normalizes a math expression for comparison and display:
// surrounding whitespace is trimmed, one layer of outer math delimiters
// ($$, $, \(...\), \[...\], \begin{env}...\end{env}) is unwrapped, and all
// internal whitespace runs collapse to single spaces. Empty input yields "".
// It never fails.
func NormalizeMath(expr string) string {
	s := strings.TrimSpace(expr)
	if s == "" {
		return ""
	}
	// Unwrap \begin{env}...\end{env}: keep the inner body.
	if strings.HasPrefix(s, `\begin`) {
		if close := strings.Index(s, "}"); close != -1 {
			if end := strings.LastIndex(s, `\end`); end > close {
				inner := strings.TrimSpace(s[close+1 : end])
				if inner != "" {
					s = inner
				}
			}
		}
	}
	// Unwrap one layer of paired delimiters.
	pairs := [][2]string{
		{"$$", "$$"},
		{`\[`, `\]`},
		{`\(`, `\)`},
		{"$", "$"},
	}
	for _, p := range pairs {
		if len(s) > 2*len(p[0]) && strings.HasPrefix(s, p[0]) && strings.HasSuffix(s, p[1]) {
			s = strings.TrimSpace(s[len(p[0]) : len(s)-len(p[1])])
			break
		}
	}
	s = strings.Join(strings.Fields(s), " ")
	return s
}
