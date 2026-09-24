package database

import (
	"strings"
	"testing"
	"time"

	"github.com/suryanshu-09/holy_grail/internal/config"
)

// badConnConfig builds a config pointing at an address that fails fast
// without requiring a live Postgres (refused TCP port / missing socket).
func badConnConfig(url string) *config.AppConfig {
	return &config.AppConfig{
		DatabaseURL:       url,
		DBMaxOpenConns:    2,
		DBMaxIdleConns:    1,
		DBConnMaxLifetime: time.Minute,
	}
}

func TestNewConnectionFailsWithoutDatabase(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"refused tcp port", "postgres://u:p@127.0.0.1:1/db?sslmode=disable"},
		{"missing unix socket dir", "postgres://u:p@/db?sslmode=disable&host=/nonexistent-socket-dir-xyz"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db, err := NewConnection(badConnConfig(tc.url))
			if err == nil {
				if db != nil {
					db.Close()
				}
				t.Fatalf("NewConnection(%q) succeeded, want ping failure", tc.url)
			}
			if !strings.HasPrefix(err.Error(), "database:") {
				t.Fatalf("error %q missing database: prefix", err)
			}
			if db != nil {
				db.Close()
			}
		})
	}
}

func TestNewConnectionRejectsBadDSN(t *testing.T) {
	// Control characters make the DSN unparseable at the driver level.
	db, err := NewConnection(badConnConfig("postgres://\x7f/"))
	if err == nil {
		if db != nil {
			db.Close()
		}
		t.Fatalf("NewConnection with malformed DSN succeeded, want error")
	}
}
