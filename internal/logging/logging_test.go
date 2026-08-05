package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"trace": LevelTrace,
		"TRACE": LevelTrace,
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"":      slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
	}
	for in, want := range cases {
		got, err := ParseLevel(in)
		if err != nil {
			t.Fatalf("ParseLevel(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}

	if _, err := ParseLevel("bogus"); err == nil {
		t.Fatal("expected error for unknown level")
	}
}

func TestNewJSONIncludesTraceLabel(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New("json", LevelTrace, &buf)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	Trace(logger, "response detail", "status", 200)

	out := buf.String()
	if !strings.Contains(out, `"level":"TRACE"`) {
		t.Fatalf("expected TRACE level label in JSON output, got: %s", out)
	}
	if !strings.Contains(out, `"status":200`) {
		t.Fatalf("expected status attr in JSON output, got: %s", out)
	}
}

func TestNewRejectsUnknownFormat(t *testing.T) {
	if _, err := New("xml", slog.LevelInfo, &bytes.Buffer{}); err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New("text", slog.LevelInfo, &buf)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	logger.Debug("should be filtered")
	Trace(logger, "should be filtered too")
	logger.Info("should appear")

	out := buf.String()
	if strings.Contains(out, "should be filtered") {
		t.Fatalf("debug/trace lines leaked through info level filter: %s", out)
	}
	if !strings.Contains(out, "should appear") {
		t.Fatalf("info line missing: %s", out)
	}
}
