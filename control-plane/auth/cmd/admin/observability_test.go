package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestNewComponentLoggerEmitsJSONWithComponent(t *testing.T) {
	var output bytes.Buffer
	logger := newComponentLogger("astra-auth-admin", &output, "DEBUG")
	logger.Debug("ready", "request_id", "request-123")

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("decode log record: %v", err)
	}
	if record["component"] != "astra-auth-admin" {
		t.Fatalf("component = %v, want astra-auth-admin", record["component"])
	}
	if record["request_id"] != "request-123" {
		t.Fatalf("request_id = %v, want request-123", record["request_id"])
	}
}
