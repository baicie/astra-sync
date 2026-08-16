package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

func TestNewComponentLoggerEmitsJSONWithComponent(t *testing.T) {
	var output bytes.Buffer
	logger := newComponentLogger("console", &output, "DEBUG")
	logger.Debug("ready", "request_id", "request-123")

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("decode log record: %v", err)
	}
	if record["component"] != "console" {
		t.Fatalf("component = %v, want console", record["component"])
	}
	if record["request_id"] != "request-123" {
		t.Fatalf("request_id = %v, want request-123", record["request_id"])
	}
}

func TestMetricsServerIsDisabledWithoutListenAddress(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server, err := metricsServer(context.Background(), logger, " ")
	if err != nil {
		t.Fatalf("metricsServer() error = %v", err)
	}
	if server != nil {
		t.Fatal("metricsServer() returned a server while disabled")
	}
}
