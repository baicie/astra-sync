package replication

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"
	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
)

func TestPushCheckpointPublishesToTargetSubscriberAndAcknowledgesSequence(t *testing.T) {
	service := NewService(nil, nil, WithCheckpointDeduplicator(newTestDeduplicator()))
	subscriber := make(chan *controlv1.CheckpointEvent, 1)
	service.subscribers["eu-west-1"] = map[chan *controlv1.CheckpointEvent]struct{}{subscriber: {}}
	event := checkpointEvent("us-east-1")
	event.WalEntry.Sequence = 42

	response, err := service.PushCheckpoint(context.Background(), &controlv1.PushCheckpointRequest{
		Event: event, SourceRegion: "us-east-1", TargetRegion: "eu-west-1",
	})
	if err != nil {
		t.Fatalf("push checkpoint: %v", err)
	}
	if response.GetAcknowledgedSequence() != 42 {
		t.Fatalf("acknowledged sequence = %d, want 42", response.GetAcknowledgedSequence())
	}
	if received := <-subscriber; received.GetWalEntry().GetSequence() != 42 {
		t.Fatalf("received sequence = %d, want 42", received.GetWalEntry().GetSequence())
	}
}

func TestPushCheckpointRejectsDuplicateWithoutRepublishing(t *testing.T) {
	service := NewService(nil, nil, WithCheckpointDeduplicator(newTestDeduplicator()))
	subscriber := make(chan *controlv1.CheckpointEvent, 2)
	service.subscribers["eu-west-1"] = map[chan *controlv1.CheckpointEvent]struct{}{subscriber: {}}
	event := checkpointEvent("us-east-1")
	request := &controlv1.PushCheckpointRequest{Event: event, SourceRegion: "us-east-1", TargetRegion: "eu-west-1"}
	if _, err := service.PushCheckpoint(context.Background(), request); err != nil {
		t.Fatalf("first push checkpoint: %v", err)
	}
	if _, err := service.PushCheckpoint(context.Background(), request); err != nil {
		t.Fatalf("duplicate push checkpoint: %v", err)
	}
	if len(subscriber) != 1 {
		t.Fatalf("subscriber queue length = %d, want 1", len(subscriber))
	}
}

func TestPushCheckpointRejectsRegionMismatch(t *testing.T) {
	service := NewService(nil, nil)
	event := checkpointEvent("eu-west-1")
	_, err := service.PushCheckpoint(context.Background(), &controlv1.PushCheckpointRequest{
		Event: event, SourceRegion: "us-east-1", TargetRegion: "eu-west-1",
	})
	if err == nil {
		t.Fatal("push checkpoint succeeded for mismatched source region")
	}
}

func TestStreamCheckpointsFiltersByJobAndResumeSequence(t *testing.T) {
	service := NewService(nil, nil)
	stream := newRecordingCheckpointStream(context.Background())
	request := &controlv1.StreamCheckpointsRequest{Region: "eu-west-1", JobId: "job-a", ResumeFromSequence: 2}
	streamDone := make(chan error, 1)
	go func() { streamDone <- service.StreamCheckpoints(request, stream) }()

	waitForSubscription(t, service)
	for _, event := range []*controlv1.CheckpointEvent{
		checkpointEventWithJob("eu-west-1", "job-a", 1),
		checkpointEventWithJob("eu-west-1", "job-b", 3),
		checkpointEventWithJob("eu-west-1", "job-a", 3),
	} {
		if err := service.PublishCheckpoint(context.Background(), event); err != nil {
			t.Fatalf("publish checkpoint: %v", err)
		}
	}

	select {
	case event := <-stream.events:
		if event.GetWalEntry().GetJobId() != "job-a" || event.GetWalEntry().GetSequence() != 3 {
			t.Fatalf("received event = %s/%d, want job-a/3", event.GetWalEntry().GetJobId(), event.GetWalEntry().GetSequence())
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for filtered checkpoint")
	}
	stream.cancel()
	if err := <-streamDone; err == nil {
		t.Fatal("stream returned nil after cancellation")
	}
}

func TestStreamCheckpointsRejectsNegativeResumeSequence(t *testing.T) {
	service := NewService(nil, nil)
	stream := newRecordingCheckpointStream(context.Background())
	if err := service.StreamCheckpoints(&controlv1.StreamCheckpointsRequest{Region: "eu-west-1", ResumeFromSequence: -1}, stream); err == nil {
		t.Fatal("stream accepted negative resume sequence")
	}
}

func checkpointEventWithJob(region, jobID string, sequence int64) *controlv1.CheckpointEvent {
	event := checkpointEvent(region)
	event.WalEntry.JobId = jobID
	event.WalEntry.Sequence = sequence
	return event
}

type recordingCheckpointStream struct {
	ctx    context.Context
	cancel context.CancelFunc
	events chan *controlv1.CheckpointEvent
}

func newRecordingCheckpointStream(parent context.Context) *recordingCheckpointStream {
	ctx, cancel := context.WithCancel(parent)
	return &recordingCheckpointStream{ctx: ctx, cancel: cancel, events: make(chan *controlv1.CheckpointEvent, 8)}
}

func (s *recordingCheckpointStream) Send(event *controlv1.CheckpointEvent) error {
	s.events <- event
	return nil
}
func (s *recordingCheckpointStream) SetHeader(metadata.MD) error  { return nil }
func (s *recordingCheckpointStream) SendHeader(metadata.MD) error { return nil }
func (s *recordingCheckpointStream) SetTrailer(metadata.MD)       {}
func (s *recordingCheckpointStream) Context() context.Context     { return s.ctx }
func (s *recordingCheckpointStream) SendMsg(any) error            { return nil }
func (s *recordingCheckpointStream) RecvMsg(any) error            { return nil }

func waitForSubscription(t *testing.T, service *Service) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		service.mu.RLock()
		count := len(service.subscription)
		service.mu.RUnlock()
		if count == 1 {
			return
		}
	}
	t.Fatal("checkpoint stream did not subscribe")
}
