package extraction

import (
	"strings"

	"github.com/suryanshu-09/holy_grail/internal/multimodal"
)

// FormatImageDisplay renders a one-line human-readable display string for an
// extracted image, combining the multimodal embedding text with the heuristic
// diagram sub-kind. It is pure (no I/O) and safe for API display layers:
// empty refs yield a stable placeholder, never an error.
func FormatImageDisplay(ref ImageRef) string {
	desc := multimodal.DescribeImage(ref.Name, nil, ref.Description)
	text := multimodal.EmbedImageText(ref.Name, ref.FigureType, desc)
	if kind := multimodal.ClassifyDiagram(ref.FigureType, ref.Name, ref.Description); kind != "" &&
		kind != multimodal.DiagramKindUnknown &&
		!strings.EqualFold(kind, strings.TrimSpace(ref.FigureType)) {
		text += " [kind: " + kind + "]"
	}
	return text
}

// FormatTableDisplay summarizes markdown/CSV-ish table text for display
// (e.g. extraction previews). Table-free input yields "Empty table.".
// It is pure and never fails.
func FormatTableDisplay(tableText string) string {
	return multimodal.SummarizeTable(multimodal.ParseTableText(tableText))
}

// FormatMathDisplay summarizes the math expressions found in question text
// for display. It returns "" when no math is present. Expressions are
// normalized and capped so display strings stay short. It is pure and never
// fails.
func FormatMathDisplay(questionText string) string {
	exprs := multimodal.ExtractMathExpressions(questionText)
	if len(exprs) == 0 {
		return ""
	}
	shown := make([]string, 0, len(exprs))
	for _, e := range exprs {
		if n := multimodal.NormalizeMath(e); n != "" {
			if len(n) > 120 {
				n = n[:117] + "..."
			}
			shown = append(shown, n)
		}
		if len(shown) == 3 {
			break
		}
	}
	if len(shown) == 0 {
		return ""
	}
	return "Math expressions: " + strings.Join(shown, "; ")
}
