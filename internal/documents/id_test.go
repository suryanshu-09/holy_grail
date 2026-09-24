package documents

import (
	"strings"
	"testing"
)

func TestIsValidID(t *testing.T) {
	generated, err := NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}

	tests := []struct {
		name string
		id   string
		want bool
	}{
		{"generated", generated, true},
		{"empty", "", false},
		{"too short", "abc", false},
		{"too long", generated + "a", false},
		{"missing dashes", strings.ReplaceAll(generated, "-", "0"), false},
		{"dash in wrong place", "12345678123412341234123456789abc", false},
		{"non hex char", generated[:10] + "g" + generated[11:], false},
		{"uppercase hex rejected", strings.ToUpper(generated), false},
		{"spaces rejected", " " + generated[1:], false},
		{"traversal rejected", "../../etc/passwd.............", false},
		{"dots only", strings.Repeat(".", 36), false},
		{"slashes rejected", "12345678/1234/234/1234/123456789abc", false},
		{"backslash rejected", `12345678\1234\234\1234\123456789abc`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsValidID(tc.id); got != tc.want {
				t.Fatalf("IsValidID(%q) = %v, want %v", tc.id, got, tc.want)
			}
		})
	}
}

func TestNewIDFormat(t *testing.T) {
	id, err := NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	if len(id) != 36 {
		t.Fatalf("NewID length = %d, want 36", len(id))
	}
	for _, pos := range []int{8, 13, 18, 23} {
		if id[pos] != '-' {
			t.Fatalf("NewID[%d] = %q, want '-'", pos, id[pos])
		}
	}
	if id[14] != '4' {
		t.Fatalf("NewID version nibble = %q, want '4' (UUIDv4)", id[14])
	}
	switch id[19] {
	case '8', '9', 'a', 'b':
	default:
		t.Fatalf("NewID variant nibble = %q, want one of 8,9,a,b", id[19])
	}
	if !IsValidID(id) {
		t.Fatalf("generated ID %q fails IsValidID", id)
	}
}

func TestNewIDUnique(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		id, err := NewID()
		if err != nil {
			t.Fatalf("NewID: %v", err)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate ID generated: %q", id)
		}
		seen[id] = struct{}{}
	}
}
