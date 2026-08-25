//go:build integration

package acceptance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/baicie/astrasync/tests/integration/multi-region/framework"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
)

func TestReplicationDeploymentCoversTCPDeliveryDeduplicationAndRestart(t *testing.T) {
	composeFile := "../docker-compose.yaml"
	f := framework.New(t, framework.WithComposeFile(composeFile), framework.WithInsecureTransportForTest())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := f.Teardown(ctx); err != nil {
			t.Logf("deployment teardown failed: %v", err)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := f.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap deployment: %v", err)
	}

	secondaryConn, err := f.GetConnection(ctx, "us-west-1")
	if err != nil {
		t.Fatalf("connect to secondary: %v", err)
	}
	secondaryClient := controlv1.NewReplicationServiceClient(secondaryConn)
	streamCtx, streamCancel := context.WithCancel(ctx)
	t.Cleanup(streamCancel)
	stream, err := secondaryClient.StreamCheckpoints(streamCtx, &controlv1.StreamCheckpointsRequest{Region: "us-west-1"})
	if err != nil {
		t.Fatalf("subscribe to secondary checkpoints: %v", err)
	}

	event := deploymentCheckpoint("deployment-job", 7)
	request := &controlv1.PushCheckpointRequest{
		Event: event, SourceRegion: "us-east-1", TargetRegion: "us-west-1",
	}
	response, err := secondaryClient.PushCheckpoint(ctx, request)
	if err != nil {
		t.Fatalf("push checkpoint over TCP: %v", err)
	}
	if response.GetAcknowledgedSequence() != 7 {
		t.Fatalf("acknowledged sequence = %d, want 7", response.GetAcknowledgedSequence())
	}

	received, err := stream.Recv()
	if err != nil {
		t.Fatalf("receive checkpoint on secondary: %v", err)
	}
	if received.GetWalEntry().GetJobId() != "deployment-job" {
		t.Fatalf("received job = %q, want deployment-job", received.GetWalEntry().GetJobId())
	}

	duplicate, err := secondaryClient.PushCheckpoint(ctx, request)
	if err != nil {
		t.Fatalf("push duplicate checkpoint: %v", err)
	}
	if duplicate.GetAcknowledgedSequence() != 7 {
		t.Fatalf("duplicate acknowledged sequence = %d, want 7", duplicate.GetAcknowledgedSequence())
	}
	streamCancel()
	assertNoDuplicateEvent(t, f, ctx)

	if err := f.StopRegion(ctx, "us-west-1"); err != nil {
		t.Fatalf("stop secondary: %v", err)
	}
	_, err = secondaryClient.PushCheckpoint(ctx, &controlv1.PushCheckpointRequest{
		Event: deploymentCheckpoint("deployment-job", 8), SourceRegion: "us-east-1", TargetRegion: "us-west-1",
	})
	if err == nil {
		t.Fatal("expected push to stopped secondary to fail")
	}

	if err := f.StartRegion(ctx, "us-west-1"); err != nil {
		t.Fatalf("start secondary: %v", err)
	}
	if err := f.WaitForHTTPReady(ctx, "us-west-1"); err != nil {
		t.Fatalf("wait for restarted secondary: %v", err)
	}
	if err := f.WaitForGRPCReady(ctx, "us-west-1"); err != nil {
		t.Fatalf("wait for restarted secondary gRPC: %v", err)
	}

	freshConn, err := f.GetConnection(ctx, "us-west-1")
	if err != nil {
		t.Fatalf("reconnect to secondary: %v", err)
	}
	freshClient := controlv1.NewReplicationServiceClient(freshConn)
	freshStream, err := freshClient.StreamCheckpoints(ctx, &controlv1.StreamCheckpointsRequest{Region: "us-west-1"})
	if err != nil {
		t.Fatalf("subscribe after restart: %v", err)
	}
	if _, err := freshClient.PushCheckpoint(ctx, &controlv1.PushCheckpointRequest{
		Event: deploymentCheckpoint("deployment-job", 8), SourceRegion: "us-east-1", TargetRegion: "us-west-1",
	}); err != nil {
		t.Fatalf("retry checkpoint after restart: %v", err)
	}
	received, err = freshStream.Recv()
	if err != nil {
		t.Fatalf("receive retried checkpoint: %v", err)
	}
	if received.GetWalEntry().GetSequence() != 8 {
		t.Fatalf("retried sequence = %d, want 8", received.GetWalEntry().GetSequence())
	}
}

func deploymentCheckpoint(jobID string, sequence int64) *controlv1.CheckpointEvent {
	return &controlv1.CheckpointEvent{
		EventType: controlv1.CheckpointEventType_CHECKPOINT_EVENT_TYPE_CHECKPOINT_COMPLETED,
		WalEntry: &controlv1.WALEntry{
			Sequence: sequence, Region: "us-east-1", Epoch: 1, JobId: jobID,
			CheckpointUri: "replication/checkpoints/" + jobID,
			Timestamp:     timestamppb.New(time.Unix(sequence, 0).UTC()),
		},
	}
}

func assertNoDuplicateEvent(t *testing.T, f *framework.Framework, parent context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	conn, err := f.GetConnection(ctx, "us-west-1")
	if err != nil {
		t.Fatalf("connect for duplicate check: %v", err)
	}
	stream, err := controlv1.NewReplicationServiceClient(conn).StreamCheckpoints(ctx, &controlv1.StreamCheckpointsRequest{Region: "us-west-1"})
	if err != nil {
		t.Fatalf("subscribe for duplicate check: %v", err)
	}
	select {
	case receivedErr := <-recvError(stream):
		if receivedErr == nil || (!errors.Is(receivedErr, context.DeadlineExceeded) && status.Code(receivedErr) != codes.DeadlineExceeded) {
			t.Fatalf("duplicate check ended unexpectedly: %v", receivedErr)
		}
	case <-ctx.Done():
	}
}

func recvError(stream controlv1.ReplicationService_StreamCheckpointsClient) <-chan error {
	result := make(chan error, 1)
	go func() {
		_, err := stream.Recv()
		result <- err
	}()
	return result
}
