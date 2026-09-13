package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	authmetrics "io.astrasync/control-plane/auth/internal/authmetrics"
)

func TestDumpMetricsLogsOneLinePerFamilyForRevokeSession(t *testing.T) {
	var buf bytes.Buffer
	logger := newComponentLogger("astra-auth-admin", &buf, "INFO")

	reg := prometheus.NewRegistry()
	rec, err := authmetrics.NewRecorder(reg)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}
	rec.ObserveSessionRevoke("00000000-0000-4000-8000-000000000001", "req-abc")

	dumpMetrics(logger, reg, opRevokeSession, []string{
		"00000000-0000-4000-8000-000000000001",
	})

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected one dump line, got %d: %q", len(lines), buf.String())
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("decode dump line: %v", err)
	}
	if record["component"] != "astra-auth-admin" {
		t.Fatalf("component = %v, want astra-auth-admin", record["component"])
	}
	if record["operation"] != string(opRevokeSession) {
		t.Fatalf("operation = %v, want %v", record["operation"], opRevokeSession)
	}
	if record["metric"] != "auth_session_revoke_total" {
		t.Fatalf("metric = %v, want auth_session_revoke_total", record["metric"])
	}
	if record["tenant_count"] != float64(1) {
		t.Fatalf("tenant_count = %v, want 1", record["tenant_count"])
	}
	if record["tenants"] != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("tenants = %v", record["tenants"])
	}
}

func TestDumpMetricsIsNoopForNonMetricOperations(t *testing.T) {
	var buf bytes.Buffer
	logger := newComponentLogger("astra-auth-admin", &buf, "INFO")

	// nil registry → no output, no panic.
	dumpMetrics(logger, nil, opShowTenant, nil)
	if buf.Len() != 0 {
		t.Fatalf("expected no output for nil registry, got %q", buf.String())
	}
}
