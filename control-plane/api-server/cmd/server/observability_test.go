package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestNewComponentLoggerEmitsJSONWithComponent(t *testing.T) {
	var output bytes.Buffer
	logger := newComponentLogger("apiserver", &output, "DEBUG")
	logger.Debug("ready", "request_id", "request-123")

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("decode log record: %v", err)
	}
	if record["component"] != "apiserver" {
		t.Fatalf("component = %v, want apiserver", record["component"])
	}
	if record["request_id"] != "request-123" {
		t.Fatalf("request_id = %v, want request-123", record["request_id"])
	}
}

func TestMetricsServerIsDisabledWithoutListenAddress(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server, err := metricsServer(context.Background(), logger, " ", nil)
	if err != nil {
		t.Fatalf("metricsServer() error = %v", err)
	}
	if server != nil {
		t.Fatal("metricsServer() returned a server while disabled")
	}
}

func TestMetricsServerScrapesMultiRegionRegistry(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := prometheus.NewRegistry()
	collector := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "astrasync_multi_region_test_total",
		Help: "test metric for multi-region registry wiring",
	}, []string{"outcome"})
	if err := registry.Register(collector); err != nil {
		t.Fatalf("register test collector: %v", err)
	}
	collector.WithLabelValues("success").Inc()
	server, err := metricsServer(context.Background(), logger, "127.0.0.1:0", registry)
	if err != nil {
		t.Fatalf("metricsServer() error = %v", err)
	}
	t.Cleanup(func() {
		_ = server.Shutdown(context.Background())
	})

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want 200", response.Code)
	}
	if !strings.Contains(response.Body.String(), "astrasync_multi_region_test_total") {
		t.Fatal("metrics response missing multi-region registry metric")
	}
}
