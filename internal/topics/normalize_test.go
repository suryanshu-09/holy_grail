package topics

import (
	"sort"
	"testing"
)

func TestNormalizeTopicName_Plan3Examples(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Deadlocks", "Deadlock"},
		{"deadlock", "Deadlock"},
		{"Deadlock Problem", "Deadlock"},
	}
	for _, tc := range cases {
		got := NormalizeTopicName(tc.in)
		if got != tc.want {
			t.Errorf("NormalizeTopicName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// All three should map to same canonical key.
	keys := make(map[string]bool)
	for _, tc := range cases {
		k := CanonicalTopicKey(tc.in)
		keys[k] = true
	}
	if len(keys) != 1 {
		t.Errorf("expected single canonical key for PLAN3 examples, got %v", keys)
	}
	if !keys["deadlock"] {
		t.Errorf("expected canonical key 'deadlock', got %v", keys)
	}
}

func TestNormalizeTopicName_TrimsAndCollapsesWhitespace(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"  deadlock  ", "Deadlock"},
		{"  deadlock   problem  ", "Deadlock"},
		{"deadlock\tproblem", "Deadlock"},
		{"\n Deadlocks \n", "Deadlock"},
		{"  Deadlock   Prevention  ", "Deadlock Prevention"},
	}
	for _, tc := range cases {
		if got := NormalizeTopicName(tc.in); got != tc.want {
			t.Errorf("NormalizeTopicName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeTopicName_Lowercases(t *testing.T) {
	cases := []struct{ in, want string }{
		{"DEADLOCKS", "Deadlock"},
		{"DEADLOCK PROBLEM", "Deadlock"},
		{"PaGiNg", "Paging"},
	}
	for _, tc := range cases {
		if got := NormalizeTopicName(tc.in); got != tc.want {
			t.Errorf("NormalizeTopicName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeTopicName_Singularization(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Processes", "Processes"}, // alias maps to Processes (canonical)
		{"Threads", "Threads"},
		{"Paging", "Paging"},
	}
	for _, tc := range cases {
		if got := NormalizeTopicName(tc.in); got != tc.want {
			t.Errorf("NormalizeTopicName(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
	// Generic singularization: trailing s removal before title-casing
	// e.g. "Semaphores" -> "Semaphore"
	if got := NormalizeTopicName("Semaphores"); got != "Semaphore" {
		t.Errorf("NormalizeTopicName(Semaphores)=%q want Semaphore", got)
	}
}

func TestNormalizeTopicName_Aliases(t *testing.T) {
	cases := []struct{ in, want string }{
		{"dead lock", "Deadlock"},
		{"dead locks", "Deadlock"},
		{"dead-lock", "Deadlock"},
		{"deadlock problems", "Deadlock"},
	}
	for _, tc := range cases {
		if got := NormalizeTopicName(tc.in); got != tc.want {
			t.Errorf("NormalizeTopicName(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestCanonicalTopicKey(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Deadlocks", "deadlock"},
		{"deadlock", "deadlock"},
		{"Deadlock Problem", "deadlock"},
		{"  DEADLOCKS  ", "deadlock"},
		{"Paging", "paging"},
		{"", ""},
		{"   ", ""},
	}
	for _, tc := range tests {
		if got := CanonicalTopicKey(tc.in); got != tc.want {
			t.Errorf("CanonicalTopicKey(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeLabels_DedupsAndMergesConfidence(t *testing.T) {
	labels := []TopicLabel{
		{Topic: "Deadlocks", Confidence: 0.8},
		{Topic: "deadlock", Confidence: 0.9},
		{Topic: "Deadlock Problem", Confidence: 0.5},
		{Topic: "Paging", Confidence: 0.7},
		{Topic: "  paging ", Confidence: 0.6},
	}
	got := NormalizeLabels(labels)
	if len(got) != 2 {
		t.Fatalf("NormalizeLabels dedup expected 2, got %d: %v", len(got), got)
	}
	// Build map for assertion
	m := map[string]float64{}
	for _, l := range got {
		m[CanonicalTopicKey(l.Topic)] = l.Confidence
	}
	if m["deadlock"] != 0.9 {
		t.Errorf("deadlock confidence = %v, want 0.9 (max)", m["deadlock"])
	}
	if m["paging"] != 0.7 {
		t.Errorf("paging confidence = %v, want 0.7 (max)", m["paging"])
	}
	// Check display names are normalized
	for _, l := range got {
		if l.Topic == "Deadlocks" || l.Topic == "deadlock" || l.Topic == "Deadlock Problem" {
			t.Errorf("label name not normalized: %q", l.Topic)
		}
	}
	// Order should preserve first occurrence: deadlock before paging
	if got[0].Topic != "Deadlock" {
		t.Errorf("first label expected Deadlock, got %q", got[0].Topic)
	}
	if got[1].Topic != "Paging" {
		t.Errorf("second label expected Paging, got %q", got[1].Topic)
	}
}

func TestNormalizeLabels_EmptyAndWhitespaceDropped(t *testing.T) {
	labels := []TopicLabel{
		{Topic: "   ", Confidence: 0.5},
		{Topic: "", Confidence: 0.9},
		{Topic: "Deadlocks", Confidence: 0.8},
	}
	got := NormalizeLabels(labels)
	if len(got) != 1 {
		t.Fatalf("expected 1, got %v", got)
	}
	if got[0].Topic != "Deadlock" {
		t.Errorf("want Deadlock got %q", got[0].Topic)
	}
}

func TestMergeCandidates(t *testing.T) {
	topics := []Topic{
		{ID: "1", Name: "Deadlocks"},
		{ID: "2", Name: "deadlock"},
		{ID: "3", Name: "Deadlock Problem"},
		{ID: "4", Name: "Paging"},
		{ID: "5", Name: "paging"},
		{ID: "6", Name: "CPU Scheduling"},
	}
	candidates := MergeCandidates(topics)
	// Should have two candidate groups: deadlock (3 topics) and paging (2 topics)
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidate groups, got %d: %v", len(candidates), candidates)
	}
	dl, ok := candidates["deadlock"]
	if !ok {
		t.Fatalf("expected deadlock candidate group")
	}
	if len(dl) != 3 {
		t.Errorf("deadlock group len=%d want 3", len(dl))
	}
	pg, ok := candidates["paging"]
	if !ok {
		t.Fatalf("expected paging candidate group")
	}
	if len(pg) != 2 {
		t.Errorf("paging group len=%d want 2", len(pg))
	}
	// CPU Scheduling is singleton, should not appear
	if _, ok := candidates["cpu scheduling"]; ok {
		t.Errorf("singleton should not be candidate")
	}
	// Verify IDs within group regardless of order
	sort.Slice(dl, func(i, j int) bool { return dl[i].ID < dl[j].ID })
	if dl[0].ID != "1" || dl[1].ID != "2" || dl[2].ID != "3" {
		t.Errorf("deadlock group IDs mismatch %v", dl)
	}
}

func TestMergeCandidates_NoDuplicates(t *testing.T) {
	topics := []Topic{
		{ID: "1", Name: "Paging"},
		{ID: "2", Name: "Deadlock"},
	}
	candidates := MergeCandidates(topics)
	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates, got %v", candidates)
	}
}

func TestNormalizeTopicName_Empty(t *testing.T) {
	if got := NormalizeTopicName(""); got != "" {
		t.Errorf("empty => %q want empty", got)
	}
	if got := NormalizeTopicName("   "); got != "" {
		t.Errorf("whitespace => %q want empty", got)
	}
}
