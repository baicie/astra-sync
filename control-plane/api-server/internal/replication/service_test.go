package replication

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	replicationmetrics "io.astrasync/control-plane/replication/metrics"
)

func TestPublishCheckpointRecordsSuccessfulEvent(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := replicationmetrics.NewRecorder(registry)
	if err != nil {
		t.Fatalf("create recorder: %v", err)
	}
	service := NewService(nil, nil, WithMetrics(recorder))
	subscriber := make(chan *controlv1.CheckpointEvent, 1)
	service.subscribers["eu-west-1"] = map[chan *controlv1.CheckpointEvent]struct{}{subscriber: {}}

	err = service.PublishCheckpoint(context.Background(), checkpointEvent("eu-west-1"))
	if err != nil {
		t.Fatalf("publish checkpoint: %v", err)
	}
	if got := testutil.ToFloat64(recorder.EventsTotal.WithLabelValues("eu-west-1", "checkpoint", "success")); got != 1 {
		t.Fatalf("successful event count = %v, want 1", got)
	}
	if <-subscriber == nil {
		t.Fatal("subscriber received a nil checkpoint event")
	}
}

func TestPublishCheckpointRecordsFailureWhenSubscriberQueueIsFull(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := replicationmetrics.NewRecorder(registry)
	if err != nil {
		t.Fatalf("create recorder: %v", err)
	}
	service := NewService(nil, nil, WithMetrics(recorder))
	subscriber := make(chan *controlv1.CheckpointEvent, 1)
	subscriber <- checkpointEvent("eu-west-1")
	service.subscribers["eu-west-1"] = map[chan *controlv1.CheckpointEvent]struct{}{subscriber: {}}

	err = service.PublishCheckpoint(context.Background(), checkpointEvent("eu-west-1"))
	if err == nil {
		t.Fatal("expected publish failure when subscriber queue is full")
	}
	if got := testutil.ToFloat64(recorder.EventsTotal.WithLabelValues("eu-west-1", "checkpoint", "failure")); got != 1 {
		t.Fatalf("failed event count = %v, want 1", got)
	}
}

func TestReportReplicationStatusRecordsHealthyEvent(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := replicationmetrics.NewRecorder(registry)
	if err != nil {
		t.Fatalf("create recorder: %v", err)
	}
	service := NewService(nil, nil, WithMetrics(recorder))

	response, err := service.ReportReplicationStatus(context.Background(), &controlv1.ReportReplicationStatusRequest{
		Region:                 "eu-west-1",
		ReplicationLagMillis:   12,
		LastReplicatedSequence: 42,
		IsHealthy:              true,
	})
	if err != nil {
		t.Fatalf("report replication status: %v", err)
	}
	if response.GetInstruction() != controlv1.ReplicationInstruction_REPLICATION_INSTRUCTION_CONTINUE {
		t.Fatalf("instruction = %s, want CONTINUE", response.GetInstruction())
	}
	if got := testutil.ToFloat64(recorder.EventsTotal.WithLabelValues("eu-west-1", "health", "success")); got != 1 {
		t.Fatalf("successful health event count = %v, want 1", got)
	}
}

func TestReportReplicationStatusDoesNotRegressAcknowledgedSequence(t *testing.T) {
	service := NewService(nil, nil)
	first, err := service.ReportReplicationStatus(context.Background(), &controlv1.ReportReplicationStatusRequest{
		Region: "eu-west-1", LastReplicatedSequence: 42, IsHealthy: true,
	})
	if err != nil || first.GetAcknowledgedSequence() != 42 {
		t.Fatalf("first status = %#v, error = %v", first, err)
	}
	second, err := service.ReportReplicationStatus(context.Background(), &controlv1.ReportReplicationStatusRequest{
		Region: "eu-west-1", LastReplicatedSequence: 7, IsHealthy: true,
	})
	if err != nil {
		t.Fatalf("regressing status: %v", err)
	}
	if second.GetAcknowledgedSequence() != 42 {
		t.Fatalf("acknowledged sequence = %d, want 42", second.GetAcknowledgedSequence())
	}
}

func TestReportReplicationStatusRecordsFailureForInvalidRequest(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := replicationmetrics.NewRecorder(registry)
	if err != nil {
		t.Fatalf("create recorder: %v", err)
	}
	service := NewService(nil, nil, WithMetrics(recorder))

	_, err = service.ReportReplicationStatus(context.Background(), &controlv1.ReportReplicationStatusRequest{
		Region:               "eu-west-1",
		ReplicationLagMillis: -1,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("status code = %s, want %s", status.Code(err), codes.InvalidArgument)
	}
	if got := testutil.ToFloat64(recorder.EventsTotal.WithLabelValues("eu-west-1", "health", "failure")); got != 1 {
		t.Fatalf("failed health event count = %v, want 1", got)
	}
}

func TestPromoteRegionRecordsSuccessfulPromotion(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := replicationmetrics.NewRecorder(registry)
	if err != nil {
		t.Fatalf("create recorder: %v", err)
	}
	service := NewService(nil, promotionBackend{}, WithMetrics(recorder))

	response, err := service.PromoteRegion(context.Background(), &controlv1.PromoteRegionRequest{TargetRegion: "eu-west-1"})
	if err != nil {
		t.Fatalf("promote region: %v", err)
	}
	if response.GetNewRegion() != "eu-west-1" {
		t.Fatalf("new region = %q, want eu-west-1", response.GetNewRegion())
	}
	if got := testutil.ToFloat64(recorder.PromotionsTotal.WithLabelValues("eu-west-1", "success")); got != 1 {
		t.Fatalf("successful promotion count = %v, want 1", got)
	}
}

func TestPromoteRegionRecordsFailureWhenBackendIsMissing(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := replicationmetrics.NewRecorder(registry)
	if err != nil {
		t.Fatalf("create recorder: %v", err)
	}
	service := NewService(nil, nil, WithMetrics(recorder))

	_, err = service.PromoteRegion(context.Background(), &controlv1.PromoteRegionRequest{TargetRegion: "eu-west-1"})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("status code = %s, want %s", status.Code(err), codes.FailedPrecondition)
	}
	if got := testutil.ToFloat64(recorder.PromotionsTotal.WithLabelValues("eu-west-1", "failure")); got != 1 {
		t.Fatalf("failed promotion count = %v, want 1", got)
	}
}

type promotionBackend struct{}

func (promotionBackend) Promote(_ context.Context, request *controlv1.PromoteRegionRequest) (*controlv1.PromoteRegionResponse, error) {
	return &controlv1.PromoteRegionResponse{NewRegion: request.GetTargetRegion()}, nil
}

func (promotionBackend) Status(context.Context, *controlv1.GetPromotionStatusRequest) (*controlv1.PromotionStatus, error) {
	return &controlv1.PromotionStatus{}, nil
}

func checkpointEvent(region string) *controlv1.CheckpointEvent {
	return &controlv1.CheckpointEvent{
		EventType: controlv1.CheckpointEventType_CHECKPOINT_EVENT_TYPE_CHECKPOINT_COMPLETED,
		WalEntry: &controlv1.WALEntry{
			Sequence:  1,
			Region:    region,
			Epoch:     1,
			Timestamp: timestamppb.Now(),
		},
	}
}
