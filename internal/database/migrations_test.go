package database

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestMigrations_FreshApplyOrder applies 001->009 on the test database and
// asserts every expected table exists afterwards.
func TestMigrations_FreshApplyOrder(t *testing.T) {
	db := openTestDB(t)

	files := applyMigrations(t, db)

	// Verify apply order is 001..009.
	var bases []string
	for _, f := range files {
		bases = append(bases, filepath.Base(f))
	}
	for i, want := range []string{"001_", "002_", "003_", "004_", "005_", "006_", "007_", "008_", "009_"} {
		found := false
		for _, b := range bases {
			if strings.HasPrefix(b, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("migration with prefix %q not applied (got %v)", want, bases)
		}
		_ = i
	}
	// Sorted order check: first file must be 001, last must be 009.
	if !strings.HasPrefix(bases[0], "001_") || !strings.HasPrefix(bases[len(bases)-1], "009_") {
		t.Fatalf("migrations not applied in 001-009 order: %v", bases)
	}

	wantTables := []string{
		"documents", "questions", "topics", "question_topics",
		"question_images", "embeddings",
		"quiz_sessions", "quiz_attempts",
		"jobs",
		"users", "sessions", "user_preferences",
	}
	for _, table := range wantTables {
		if !tableExists(t, db, table) {
			t.Errorf("expected table %q to exist after fresh migrations", table)
		}
	}
}
