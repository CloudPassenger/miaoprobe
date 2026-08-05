// Package logging configures the process-wide slog.Logger: level filtering
// (including a TRACE level below slog's built-in Debug), and a choice of
// three output formats (rich colored console via lmittmann/tint, plain
// text, or JSON), so operators can pick whatever suits a terminal or a log
// collector.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/lmittmann/tint"
	"golang.org/x/term"
)

// LevelTrace sits one step below slog.LevelDebug, for very verbose output
// such as full network response dumps. DEBUG is for request-level activity;
// TRACE is for response-level detail.
const LevelTrace slog.Level = slog.LevelDebug - 4

// ParseLevel parses a case-insensitive level name, including "trace" which
// slog does not define natively.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "trace":
		return LevelTrace, nil
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q (want trace, debug, info, warn, or error)", s)
	}
}

// levelLabel renders a slog.Level as a fixed-width label, filling in the
// name for LevelTrace that slog itself does not know about.
func levelLabel(level slog.Level) string {
	if level == LevelTrace {
		return "TRACE"
	}
	return level.String()
}

func replaceLevelAttr(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && a.Key == slog.LevelKey {
		if level, ok := a.Value.Any().(slog.Level); ok {
			a.Value = slog.StringValue(levelLabel(level))
		}
	}
	return a
}

// New builds a *slog.Logger writing to w. format is one of "rich" (colored,
// human-oriented; colors are skipped automatically when w is not a
// terminal), "text" (plain slog text), or "json".
func New(format string, level slog.Level, w io.Writer) (*slog.Logger, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "rich", "":
		noColor := true
		if f, ok := w.(interface{ Fd() uintptr }); ok {
			noColor = !term.IsTerminal(int(f.Fd()))
		}
		return slog.New(tint.NewTextHandler(w, &tint.Options{
			Level:       level,
			NoColor:     noColor,
			ReplaceAttr: replaceLevelAttr,
		})), nil
	case "text":
		return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
			Level:       level,
			ReplaceAttr: replaceLevelAttr,
		})), nil
	case "json":
		return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
			Level:       level,
			ReplaceAttr: replaceLevelAttr,
		})), nil
	default:
		return nil, fmt.Errorf("unknown log format %q (want rich, text, or json)", format)
	}
}

// Discard returns a logger that drops everything, for tests and call sites
// that were not given a real logger.
func Discard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// Trace logs at LevelTrace; slog.Logger has no built-in convenience method
// for it since it is not one of the standard levels.
func Trace(logger *slog.Logger, msg string, args ...any) {
	logger.Log(context.Background(), LevelTrace, msg, args...)
}
