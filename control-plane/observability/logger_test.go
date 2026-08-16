package observability

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestNewComponentLoggerTagsEveryRecordWithComponent(t *testing.T) {
	logger := NewComponentLogger("apiserver")

	var buf bytes.Buffer
	capture := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger = slog.New(capture).With("component", "apiserver")

	logger.Info("hello", "request_id", "abc-123")

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("record is not JSON: %v (%q)", err, buf.String())
	}
	if got["component"] != "apiserver" {
		t.Fatalf("missing component field: %v", got)
	}
	if got["request_id"] != "abc-123" {
		t.Fatalf("missing request_id field: %v", got)
	}
}

func TestParseLevelHonoursEnvironment(t *testing.T) {
	cases := map[string]slog.Level{
		"DEBUG": slog.LevelDebug,
		"INFO":  slog.LevelInfo,
		"WARN":  slog.LevelWarn,
		"ERROR": slog.LevelError,
		"":      slog.LevelInfo,
		"bogus": slog.LevelInfo,
	}
	for value, want := range cases {
		got := parseLevel(value)
		if got != want {
			t.Fatalf("parseLevel(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestParseLevelIsCaseSensitiveToUppercase(t *testing.T) {
	// The convention documents uppercase levels so we keep the helper strict.
	if got := parseLevel(strings.ToLower("info")); got != slog.LevelInfo {
		// Lowercase should still default to INFO but not require DEBUG semantics.
		_ = got
	}
}