package logger

import (
	"log/slog"
	"os"
)

// New creates a structured JSON logger suitable for Railway log aggregation.
// In development it emits human-readable text; in production it emits JSON.
func New(env string) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}

	if env == "development" {
		opts.Level = slog.LevelDebug
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}

	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}
