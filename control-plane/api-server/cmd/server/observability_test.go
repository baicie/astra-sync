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
	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.uber.org/zap"
	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	apiReplication "io.astrasync/control-plane/api-server/internal/replication"
	replicationmetrics "io.astrasync/control-plane/replication/metrics"
	recoverydomain "io.astrasync/control-plane/replication/recovery"
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

func TestMetricsServerScrapesMultiRegionBusinessSamples(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bundle, err := replicationmetrics.NewBundle()
	if err != nil {
		t.Fatalf("create multi-region metrics bundle: %v", err)
	}

	recoveryManager := recoverydomain.NewManager(
		zap.NewNop(),
		observabilityRecoveryReader{},
		observabilityObjectStorage{},
		observabilityManifestParser{},
		observabilityValidator{},
		observabilityStateRestorer{},
		nil,
		recoverydomain.WithTargetRegion("eu-west-1"),
		recoverydomain.WithMetrics(bundle.Recorder),
	)
	recoveryBackend, err := apiReplication.NewRecoveryManagerBackend(recoveryManager)
	if err != nil {
		t.Fatalf("create recovery backend: %v", err)
	}
	replicationService := apiReplication.NewService(
		[]apiReplication.TopologyRegion{{Name: "us-east-1"}, {Name: "eu-west-1"}},
		observabilityPromotionBackend{},
		apiReplication.WithMetrics(bundle.Recorder),
	)
	replicationService.SetRecoveryBackend(recoveryBackend)

	if _, err := replicationService.PushCheckpoint(context.Background(), &controlv1.PushCheckpointRequest{
		Event: &controlv1.CheckpointEvent{
			EventType: controlv1.CheckpointEventType_CHECKPOINT_EVENT_TYPE_CHECKPOINT_COMPLETED,
			WalEntry: &controlv1.WALEntry{
				JobId: "job-observability", Region: "us-east-1", Sequence: 1, Epoch: 1,
			},
		},
		SourceRegion: "us-east-1", TargetRegion: "eu-west-1",
	}); err != nil {
		t.Fatalf("push checkpoint: %v", err)
	}
	if _, err := replicationService.PromoteRegion(context.Background(), &controlv1.PromoteRegionRequest{
		TargetRegion: "eu-west-1",
	}); err != nil {
		t.Fatalf("promote region: %v", err)
	}
	if _, err := replicationService.RecoverForPromotion(context.Background(), &controlv1.RecoverForPromotionRequest{
		JobId: "job-observability", SourceRegion: "us-east-1", TargetRegion: "eu-west-1",
		NewEpoch: 1, PromotionId: "promotion-observability", IdempotencyKey: "recovery-observability-key",
	}); err != nil {
		t.Fatalf("recover for promotion: %v", err)
	}

	if got := testutil.ToFloat64(bundle.Recorder.EventsTotal.WithLabelValues("us-east-1", "checkpoint", "success")); got != 1 {
		t.Fatalf("checkpoint event count = %v, want 1", got)
	}
	if got := testutil.ToFloat64(bundle.Recorder.PromotionsTotal.WithLabelValues("eu-west-1", "success")); got != 1 {
		t.Fatalf("promotion count = %v, want 1", got)
	}
	if got := testutil.ToFloat64(bundle.Recorder.RecoveriesTotal.WithLabelValues("eu-west-1", "success")); got != 1 {
		t.Fatalf("recovery count = %v, want 1", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	server, err := metricsServer(ctx, logger, "127.0.0.1:0", bundle.Registry)
	if err != nil {
		t.Fatalf("metricsServer() error = %v", err)
	}
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want 200", response.Code)
	}
	body := response.Body.String()
	for _, metric := range []string{
		"astrasync_multi_region_event_total",
		"astrasync_multi_region_promotion_total",
		"astrasync_multi_region_recovery_total",
	} {
		if !strings.Contains(body, metric) {
			t.Fatalf("metrics response missing %s", metric)
		}
	}
	for _, sample := range []string{
		`astrasync_multi_region_event_total{event_type="checkpoint",outcome="success",peer_region="us-east-1"} 1`,
		`astrasync_multi_region_promotion_total{outcome="success",target_region="eu-west-1"} 1`,
		`astrasync_multi_region_recovery_total{outcome="success",target_region="eu-west-1"} 1`,
	} {
		if !strings.Contains(body, sample) {
			t.Fatalf("metrics response missing sample %s", sample)
		}
	}
}

type observabilityPromotionBackend struct{}

func (observabilityPromotionBackend) Promote(_ context.Context, request *controlv1.PromoteRegionRequest) (*controlv1.PromoteRegionResponse, error) {
	return &controlv1.PromoteRegionResponse{NewRegion: request.GetTargetRegion()}, nil
}

func (observabilityPromotionBackend) Status(context.Context, *controlv1.GetPromotionStatusRequest) (*controlv1.PromotionStatus, error) {
	return &controlv1.PromotionStatus{}, nil
}

type observabilityRecoveryReader struct{}

func (observabilityRecoveryReader) ReadEntries(context.Context, int64) ([]*recoverydomain.WALEntry, error) {
	return nil, nil
}

func (observabilityRecoveryReader) GetLatestCheckpoint(context.Context) (*recoverydomain.CheckpointManifest, error) {
	return &recoverydomain.CheckpointManifest{
		JobID: "job-observability", Epoch: 1, Sequence: 1, CheckpointURI: "checkpoints/job-observability.json",
	}, nil
}

type observabilityObjectStorage struct{}

func (observabilityObjectStorage) GetObject(context.Context, string) ([]byte, error) {
	return []byte("manifest"), nil
}

func (observabilityObjectStorage) GetObjectReader(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("manifest")), nil
}

type observabilityManifestParser struct{}

func (observabilityManifestParser) Parse([]byte) (*recoverydomain.CheckpointManifest, error) {
	return &recoverydomain.CheckpointManifest{
		JobID: "job-observability", Epoch: 1, Sequence: 1, CheckpointURI: "checkpoints/job-observability.json",
	}, nil
}

type observabilityValidator struct{}

func (observabilityValidator) Validate(context.Context, *recoverydomain.CheckpointManifest) error {
	return nil
}

type observabilityStateRestorer struct{}

func (observabilityStateRestorer) Restore(context.Context, *recoverydomain.CheckpointManifest) error {
	return nil
}
