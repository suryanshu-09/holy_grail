package logging

import (
	"log/slog"
	"os"
)

// New returns a structured slog.Logger for the given environment.
// Production uses JSON output; development uses human-friendly text.
func New(env string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}

	var handler slog.Handler
	if env == "production" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}
