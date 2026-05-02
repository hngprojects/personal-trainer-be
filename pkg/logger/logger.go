package logger

import (
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/lmittmann/tint"
)

// New builds a slog.Logger configured for the given level, format, and app environment.
//
// Format selection:
//   - format="pretty" -> human-friendly text with colors (when stdout is a TTY)
//   - format="json"   -> single-line JSON (best for log pipelines)
//   - format=""       -> pretty when env=="development", json otherwise
func New(level, format, env string) *slog.Logger {
	lvl := parseLevel(level)
	format = resolveFormat(format, env)

	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	} else {
		handler = tint.NewHandler(os.Stdout, &tint.Options{
			Level:      lvl,
			TimeFormat: time.TimeOnly,
			NoColor:    !isTerminal(os.Stdout),
		})
	}
	return slog.New(handler)
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func resolveFormat(format, env string) string {
	switch strings.ToLower(format) {
	case "json":
		return "json"
	case "pretty", "text":
		return "pretty"
	}
	if env == "development" {
		return "pretty"
	}
	return "json"
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
