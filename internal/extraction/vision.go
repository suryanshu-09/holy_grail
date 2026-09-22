package extraction

import (
	"context"
	"os"
	"strings"
)

// Figure-type constants for vision-based image classification. Stored on
// ImageRef.FigureType and persisted in pages.json/images.json manifests.
const (
	FigureTypeDiagram    = "diagram"
	FigureTypeGraph      = "graph"
	FigureTypeMathFigure = "math_figure"
	FigureTypeTable      = "table"
	FigureTypeChart      = "chart"
	FigureTypePhoto      = "photo"
	FigureTypeUnknown    = "unknown"
)

// DefaultVisionModel is used when OPENAI_VISION_MODEL is unset.
const DefaultVisionModel = "gpt-4o-mini"

// DescribeInput is the input to a VisionDescriber for a single image.
type DescribeInput struct {
	// Name is the image file name (e.g. page-001-img-000.png).
	Name string
	// Page is the 1-based page number the image was extracted from.
	Page int
	// Path is the absolute filesystem path to the image bytes. Describers
	// read (never write) this file; it may be empty when bytes are instead
	// supplied via Data.
	Path string
	// Data holds raw image bytes when Path is unavailable.
	Data []byte
	// Format is the lowercase image extension without dot (png/jpg/...).
	Format string
	// Context is optional surrounding page text to ground the description.
	Context string
}

// DescribeOutput is the result of describing a single image.
type DescribeOutput struct {
	// Description is a concise (1-3 sentence) description of the figure.
	Description string
	// FigureType is one of the FigureType* constants.
	FigureType string
	// DescribedBy names the model/backend that produced the description
	// (e.g. "gpt-4o-mini" or "noop").
	DescribedBy string
}

// VisionDescriber produces a text description plus figure-type classification
// for an extracted image. Implementations must never fail the extraction
// pipeline: callers treat errors as "vision unavailable" and continue with
// empty descriptions. Implementations must be safe for concurrent use.
type VisionDescriber interface {
	// Describe returns the description for in, or an error when vision is
	// unavailable (no key, transport failure, unsupported format, ...).
	Describe(ctx context.Context, in DescribeInput) (DescribeOutput, error)
}

// NoopDescriber is the nil-safe fallback used when no vision backend is
// configured (e.g. OPENAI_API_KEY unset). It never calls a network service
// and never returns an error.
type NoopDescriber struct{}

// Describe returns an empty description classified as unknown.
func (NoopDescriber) Describe(_ context.Context, _ DescribeInput) (DescribeOutput, error) {
	return DescribeOutput{FigureType: FigureTypeUnknown, DescribedBy: "noop"}, nil
}

// NormalizeFigureType maps a raw model label to one of the FigureType*
// constants. Matching is case-insensitive and tolerant of common aliases
// (e.g. "graphs" -> graph, "mathematical figure" -> math_figure,
// "photograph" -> photo). Unknown or empty input maps to FigureTypeUnknown.
func NormalizeFigureType(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case FigureTypeDiagram, "diagrams":
		return FigureTypeDiagram
	case FigureTypeGraph, "graphs", "plot", "plots":
		return FigureTypeGraph
	case FigureTypeMathFigure, "math", "mathfigure", "math-figure",
		"mathematical figure", "mathematical-figure", "equation", "formula":
		return FigureTypeMathFigure
	case FigureTypeTable, "tables", "tabular":
		return FigureTypeTable
	case FigureTypeChart, "charts", "bar chart", "bar-chart", "barchart",
		"pie chart", "pie-chart", "piechart", "line chart", "histogram":
		return FigureTypeChart
	case FigureTypePhoto, "photos", "photograph", "photographs", "image", "figure":
		return FigureTypePhoto
	case FigureTypeUnknown, "", "none", "n/a", "other":
		return FigureTypeUnknown
	default:
		return FigureTypeUnknown
	}
}

// VisionModelFromEnv returns the vision model name from OPENAI_VISION_MODEL,
// falling back to DefaultVisionModel when unset or blank.
func VisionModelFromEnv() string {
	if m := strings.TrimSpace(os.Getenv("OPENAI_VISION_MODEL")); m != "" {
		return m
	}
	return DefaultVisionModel
}

// BuildVisionPrompt builds the deterministic text prompt sent to the vision
// model for an image. The model is instructed to return strict JSON with
// "description" and "figure_type" keys; callers validate and normalize it.
func BuildVisionPrompt(in DescribeInput) string {
	var b strings.Builder
	b.WriteString("Describe the educational figure in this image in 1-3 concise sentences. ")
	b.WriteString("Focus on content a student needs to answer questions about it (labels, axes, values, relationships). ")
	b.WriteString("Also classify it with exactly one figure_type from: diagram, graph, math_figure, table, chart, photo, unknown. ")
	b.WriteString("Respond with strict JSON only: {\"description\": \"...\", \"figure_type\": \"...\"}.")
	if strings.TrimSpace(in.Context) != "" {
		b.WriteString(" Page context: ")
		ctx := strings.TrimSpace(in.Context)
		if len(ctx) > 500 {
			ctx = ctx[:500]
		}
		b.WriteString(ctx)
	}
	return b.String()
}
