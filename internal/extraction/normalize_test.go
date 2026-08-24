package extraction

import "testing"

func TestNormalizeText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "keeps line breaks and question numbering",
			in:   "Q1. Define deadlock.\nQ2. Explain scheduling.",
			want: "Q1. Define deadlock.\nQ2. Explain scheduling.",
		},
		{
			name: "keeps numbering patterns at line starts",
			in:   "Section A\n\n1. What is a process?\nSome answer text.\n\n2) Define thread.\nQuestion 3. Explain scheduling.\n17 What is virtual memory?",
			want: "Section A\n\n1. What is a process?\nSome answer text.\n\n2) Define thread.\nQuestion 3. Explain scheduling.\n17 What is virtual memory?",
		},
		{
			name: "collapses whitespace inside lines but not across them",
			in:   "Q1. \t What  is  a \n\t process?   Explain.",
			want: "Q1. What is a\nprocess? Explain.",
		},
		{
			name: "collapses space runs",
			in:   "Q1.    Define    deadlock.",
			want: "Q1. Define deadlock.",
		},
		{
			name: "trims each line",
			in:   "  Q1. Define deadlock.\t\n\tQ2. Explain scheduling.  ",
			want: "Q1. Define deadlock.\nQ2. Explain scheduling.",
		},
		{
			name: "collapses blank lines",
			in:   "\n\nIntro\n\n\n\nBody\n\n",
			want: "Intro\n\nBody",
		},
		{
			name: "normalizes CRLF and CR",
			in:   "one\r\ntwo\rthree",
			want: "one\ntwo\nthree",
		},
		{
			name: "tabs collapse to single space",
			in:   "a\t\tb",
			want: "a b",
		},
		{
			name: "empty input stays empty",
			in:   "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeText(tt.in); got != tt.want {
				t.Errorf("normalizeText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
