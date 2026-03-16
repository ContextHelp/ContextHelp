package logger

import (
	"io"
	"log/slog"
	"os"
)

// Init configures the default slog logger. verbose=true sets level to Debug,
// otherwise Warn. Writes to stderr.
func Init(verbose bool) {
	InitWithWriter(verbose, os.Stderr)
}

// InitWithWriter configures the default slog logger with a custom writer.
// Intended for testing.
func InitWithWriter(verbose bool, w io.Writer) {
	level := slog.LevelWarn
	if verbose {
		level = slog.LevelDebug
	}
	h := slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(h))
}
