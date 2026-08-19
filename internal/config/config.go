package config

import "os"

// Port returns the HTTP port from environment or default
func Port() string {
	if p := os.Getenv("PORT"); p != "" {
		return p
	}
	return "8080"
}
