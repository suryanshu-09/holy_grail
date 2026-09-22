package extraction

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Phase 18: vision prompt building must instruct strict JSON with
// description + figure_type over the canonical label set.
func TestPhase18_BuildVisionPrompt(t *testing.T) {
	p := BuildVisionPrompt(DescribeInput{Name: "page-001-img-000.png", Page: 1})
	for _, want := range []string{
		"diagram, graph, math_figure, table, chart, photo, unknown",
		"strict JSON",
		`"description"`,
		`"figure_type"`,
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q: %s", want, p)
		}
	}
	// Context grounds the description.
	pCtx := BuildVisionPrompt(DescribeInput{Name: "a.png", Context: "photosynthesis light reactions"})
	if !strings.Contains(pCtx, "photosynthesis light reactions") {
		t.Errorf("prompt should include page context: %s", pCtx)
	}
	// Long context is truncated to 500 chars (never unbounded).
	long := strings.Repeat("y", 1000)
	pLong := BuildVisionPrompt(DescribeInput{Context: long})
	if strings.Contains(pLong, long) {
		t.Errorf("long context was not truncated")
	}
	if !strings.Contains(pLong, strings.Repeat("y", 500)) {
		t.Errorf("truncated context should keep first 500 chars")
	}
}

// Phase 18: figure-type normalization must cover all six visual classes
// plus common aliases and map garbage/empty to unknown.
func TestPhase18_NormalizeFigureTypeAllTypes(t *testing.T) {
	cases := map[string]string{
		// canonical
		"diagram": "diagram", "graph": "graph", "math_figure": "math_figure",
		"table": "table", "chart": "chart", "photo": "photo", "unknown": "unknown",
		// aliases per type
		"diagrams": "diagram",
		"plots":    "graph", "plot": "graph", "graphs": "graph",
		"mathematical figure": "math_figure", "equation": "math_figure",
		"formula": "math_figure", "math": "math_figure",
		"tables": "table", "tabular": "table",
		"bar chart": "chart", "pie chart": "chart", "histogram": "chart", "charts": "chart",
		"photograph": "photo", "photos": "photo",
		// case-insensitivity
		"Diagram": "diagram", "GRAPH": "graph", "Math_Figure": "math_figure",
		"TABLE": "table", "Chart": "chart", "PHOTO": "photo",
		// unknown/empty
		"": "unknown", "nonsense": "unknown", "none": "unknown", "other": "unknown",
	}
	for in, want := range cases {
		if got := NormalizeFigureType(in); got != want {
			t.Errorf("NormalizeFigureType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPhase18_VisionModelFromEnv(t *testing.T) {
	t.Setenv("OPENAI_VISION_MODEL", "")
	if got := VisionModelFromEnv(); got != DefaultVisionModel {
		t.Fatalf("default = %q, want %q", got, DefaultVisionModel)
	}
	t.Setenv("OPENAI_VISION_MODEL", "gpt-4o-test")
	if got := VisionModelFromEnv(); got != "gpt-4o-test" {
		t.Fatalf("override = %q, want gpt-4o-test", got)
	}
}

func TestPhase18_NoopDescriberNeverFails(t *testing.T) {
	var d NoopDescriber
	out, err := d.Describe(context.Background(), DescribeInput{Name: "x.png"})
	if err != nil {
		t.Fatalf("noop Describe: %v", err)
	}
	if out.FigureType != FigureTypeUnknown || out.DescribedBy != "noop" {
		t.Fatalf("noop output = %+v", out)
	}
}

// Phase 18: description-to-question association. Each of the six figure
// types carries a vision description on ImageRef; questions must pick up
// the images on their page range with descriptions intact on the page.
func TestPhase18_DescriptionToQuestionAssociation(t *testing.T) {
	figureTypes := []struct {
		ft   string
		desc string
	}{
		{FigureTypeDiagram, "Block diagram of the CPU datapath with ALU and registers."},
		{FigureTypeGraph, "Line graph of throughput vs load showing saturation at 80%."},
		{FigureTypeMathFigure, "Triangle with sides labelled for Pythagoras theorem proof."},
		{FigureTypeTable, "Table of process states with burst times and priorities."},
		{FigureTypeChart, "Bar chart of sales per quarter for 2024."},
		{FigureTypePhoto, "Photograph of the laboratory experimental setup."},
	}
	pages := make([]Page, 0, len(figureTypes))
	for i, ft := range figureTypes {
		pages = append(pages, Page{
			Number: i + 1,
			Text:   "Q" + strings.Repeat("0", 0) + " content placeholder",
			Images: []ImageRef{{
				Name:        "page-00-img.png",
				Page:        i + 1,
				Description: ft.desc,
				FigureType:  ft.ft,
				DescribedBy: "gpt-4o-mini",
			}},
		})
	}
	// Give each page a real numbered question so parsing is deterministic.
	for i := range pages {
		pages[i].Text = "Q" + p18Itoa(i+1) + ". Explain the concept shown in the figure on this page in detail?"
		pages[i].Images[0].Name = "page-00" + p18Itoa(i+1) + "-img-000.png"
	}
	de := DocumentExtraction{DocumentID: "doc-p18", PageCount: len(pages), Pages: pages}
	qs := parseQuestionsFromExtraction(de)
	if len(qs) != len(figureTypes) {
		t.Fatalf("got %d questions, want %d", len(qs), len(figureTypes))
	}
	for i, q := range qs {
		if len(q.Images) != 1 {
			t.Fatalf("q%d images = %v, want 1 image", i+1, q.Images)
		}
		// Description lives on the ImageRef (question carries the name
		// reference); verify the source page still carries it.
		got := pages[i].Images[0]
		if got.Description != figureTypes[i].desc {
			t.Errorf("q%d description = %q, want %q", i+1, got.Description, figureTypes[i].desc)
		}
		if got.FigureType != figureTypes[i].ft {
			t.Errorf("q%d figure_type = %q, want %q", i+1, got.FigureType, figureTypes[i].ft)
		}
	}
}

// Phase 18: Y-heuristic refinement — two questions sharing one page must
// each receive their nearest image, with association notes recorded.
func TestPhase18_RefineImageAssociationYHeuristic(t *testing.T) {
	de := DocumentExtraction{
		DocumentID: "doc-p18-y",
		PageCount:  1,
		Pages: []Page{{
			Number: 1,
			Text:   "Q1. What does the top diagram show?\nQ2. What does the bottom chart show?",
			Images: []ImageRef{
				{Name: "top-diagram.png", Page: 1, Y: 650, Description: "Top schematic.", FigureType: FigureTypeDiagram},
				{Name: "bottom-chart.png", Page: 1, Y: 150, Description: "Bottom bars.", FigureType: FigureTypeChart},
			},
		}},
	}
	qs := parseQuestionsFromExtraction(de)
	if len(qs) != 2 {
		t.Fatalf("got %d questions, want 2", len(qs))
	}
	// Each question should end up with exactly one image after refinement.
	for i, q := range qs {
		if len(q.Images) != 1 {
			t.Fatalf("q%d images = %v, want exactly 1 after Y refinement", i+1, q.Images)
		}
	}
	if qs[0].Images[0] == qs[1].Images[0] {
		t.Fatalf("both questions got %q; Y heuristic should split them", qs[0].Images[0])
	}
	// Top question should own the top image (higher Y).
	if qs[0].Images[0] != "top-diagram.png" {
		t.Errorf("q1 images = %v, want [top-diagram.png]", qs[0].Images)
	}
	if qs[1].Images[0] != "bottom-chart.png" {
		t.Errorf("q2 images = %v, want [bottom-chart.png]", qs[1].Images)
	}
	found := false
	for _, q := range qs {
		for _, n := range q.ExtractionNotes {
			if strings.Contains(n, "image-associated:") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected image-associated: notes, got %+v", qs)
	}
}

// Phase 18: vision fields must survive JSON manifest round-trips
// (pages.json / images.json / question debug artifacts).
func TestPhase18_ImageRefVisionFieldsPersist(t *testing.T) {
	types := []string{FigureTypeDiagram, FigureTypeGraph, FigureTypeMathFigure, FigureTypeTable, FigureTypeChart, FigureTypePhoto}
	for _, ft := range types {
		ref := ImageRef{
			Name: "fig.png", Page: 1,
			Description: "desc for " + ft, FigureType: ft, DescribedBy: "gpt-4o-mini",
		}
		raw, err := json.Marshal(ref)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var back ImageRef
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if back.Description != ref.Description || back.FigureType != ft || back.DescribedBy != "gpt-4o-mini" {
			t.Errorf("round-trip mismatch for %q: %+v", ft, back)
		}
	}
}

func p18Itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [16]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}
