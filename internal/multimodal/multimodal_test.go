package multimodal

import (
	"strings"
	"testing"
)

func TestDescribeImage_Passthrough(t *testing.T) {
	got := DescribeImage("fig.png", []byte("bytes"), "  Block diagram of the CPU.  ")
	if got != "Block diagram of the CPU." {
		t.Fatalf("passthrough = %q, want trimmed vision description", got)
	}
}

func TestDescribeImage_DeterministicHash(t *testing.T) {
	a := DescribeImage("fig.png", []byte("bytes-1"), "")
	b := DescribeImage("fig.png", []byte("bytes-1"), "")
	c := DescribeImage("fig.png", []byte("bytes-2"), "")
	if a != b {
		t.Fatalf("same input gave different stubs: %q vs %q", a, b)
	}
	if a == c {
		t.Fatalf("different bytes gave identical stub: %q", a)
	}
	for _, s := range []string{a, c} {
		if !strings.Contains(s, "fig.png") || !strings.Contains(s, "vision unavailable") {
			t.Errorf("stub missing name/hash context: %q", s)
		}
	}
	if got := DescribeImage("", nil, ""); !strings.Contains(got, "unnamed") {
		t.Errorf("empty stub should mention unnamed, got %q", got)
	}
}

func TestEmbedImageText(t *testing.T) {
	cases := []struct {
		name, ft, desc, want string
	}{
		{"fig.png", "diagram", "CPU datapath.", "Figure fig.png (diagram): CPU datapath."},
		{"fig.png", "graph", "", "Figure fig.png (graph)"},
		{"fig.png", "", "Some description.", "Figure fig.png: Some description."},
		{"", "", "", "Figure unnamed figure"},
	}
	for _, c := range cases {
		if got := EmbedImageText(c.name, c.ft, c.desc); got != c.want {
			t.Errorf("EmbedImageText(%q,%q,%q) = %q, want %q", c.name, c.ft, c.desc, got, c.want)
		}
	}
}

func TestClassifyDiagram(t *testing.T) {
	cases := []struct {
		ft, name, desc, want string
	}{
		{"diagram", "fig.png", "Flowchart of the login process.", "flowchart"},
		{"diagram", "fig.png", "Circuit diagram with resistor R1 in series with capacitor C1.", "circuit"},
		{"graph", "fig.png", "Line graph of throughput vs load showing saturation.", "graph"},
		{"chart", "fig.png", "Bar chart of sales per quarter.", "chart"},
		{"table", "fig.png", "Table of burst times and priorities.", "table"},
		{"math_figure", "fig.png", "Triangle labelled for the Pythagoras theorem proof.", "geometry"},
		{"diagram", "topology.png", "Network topology with two routers.", "network"},
		{"diagram", "fig.png", "Block diagram of the CPU datapath with ALU and registers.", "general_diagram"},
		{"photo", "lab.png", "Photograph of the laboratory setup.", "photo"},
		{"", "", "", "unknown"},
	}
	for _, c := range cases {
		if got := ClassifyDiagram(c.ft, c.name, c.desc); got != c.want {
			t.Errorf("ClassifyDiagram(%q,%q,%q) = %q, want %q", c.ft, c.name, c.desc, got, c.want)
		}
	}
}

func TestParseTableText_Markdown(t *testing.T) {
	text := "| Process | Burst | Priority |\n|---|---|---|\n| P1 | 5 | 2 |\n| P2 | 3 | 1 |"
	rows := ParseTableText(text)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3: %v", len(rows), rows)
	}
	if rows[0][0] != "Process" || rows[0][2] != "Priority" {
		t.Errorf("header = %v", rows[0])
	}
	if rows[2][0] != "P2" || rows[2][1] != "3" {
		t.Errorf("last row = %v", rows[2])
	}
}

func TestParseTableText_CSVAndProse(t *testing.T) {
	text := "name,age,city\nAlice,30,Paris\n\nThis line is prose without delimiters\nBob,25,Rome"
	rows := ParseTableText(text)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3: %v", len(rows), rows)
	}
	if rows[1][0] != "Alice" || rows[2][2] != "Rome" {
		t.Errorf("rows = %v", rows)
	}
	if got := ParseTableText("just some prose\nmore prose here"); len(got) != 0 {
		t.Errorf("prose should yield no rows, got %v", got)
	}
}

func TestSummarizeTable(t *testing.T) {
	if got := SummarizeTable(nil); got != "Empty table." {
		t.Errorf("empty = %q", got)
	}
	rows := [][]string{{"Process", "Burst"}, {"P1", "5"}, {"P2", "3"}}
	got := SummarizeTable(rows)
	for _, want := range []string{"3 rows", "2 columns", "Process", "Burst", "P1", "5", "+1 more"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q missing %q", got, want)
		}
	}
}

func TestExtractMathExpressions(t *testing.T) {
	text := "Solve $E=mc^2$ and $$x^2 + y^2 = z^2$$ plus \\frac{a}{b}.\n" +
		"\\begin{equation}\nF = m \\cdot a\n\\end{equation}\n" +
		"Consider v = u + a*t for motion."
	got := ExtractMathExpressions(text)
	if len(got) == 0 {
		t.Fatal("expected math expressions, got none")
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{"$E=mc^2$", "x^2 + y^2 = z^2", `\frac{a}{b}`, "F = m", "v = u + a*t"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %v", want, got)
		}
	}
	// Duplicates collapse.
	dup := ExtractMathExpressions("$x+1$ and $x+1$")
	if len(dup) != 1 {
		t.Errorf("duplicates should collapse, got %v", dup)
	}
	if got := ExtractMathExpressions("plain prose without math"); len(got) != 0 {
		t.Errorf("prose should yield no math, got %v", got)
	}
}

func TestNormalizeMath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  $$ x^2 +  y  $$  ", "x^2 + y"},
		{`\(a+b\)`, "a+b"},
		{`\[  \frac{a}{b}  \]`, `\frac{a}{b}`},
		{`$E=mc^2$`, "E=mc^2"},
		{`\begin{equation} F = m a \end{equation}`, "F = m a"},
		{"", ""},
	}
	for _, c := range cases {
		if got := NormalizeMath(c.in); got != c.want {
			t.Errorf("NormalizeMath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
