package replication

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/replication/channel"
)

func TestRemoteRecoveryClientInvokesRegionRecoveryService(t *testing.T) {
	service := NewService(recoveryRegions(), nil)
	service.SetRecoveryBackend(recoveryBackendFunc(func(_ context.Context, request *controlv1.RecoverForPromotionRequest) (*controlv1.RecoverForPromotionResponse, error) {
		return &controlv1.RecoverForPromotionResponse{JobId: request.GetJobId(), PromotionId: request.GetPromotionId(), State: "recovery_complete", RecoveredEpoch: request.GetNewEpoch()}, nil
	}))
	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	controlv1.RegisterRegionRecoveryServiceServer(grpcServer, service)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = listener.Close()
	})
	conn, err := grpc.DialContext(context.Background(), "bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client, err := NewRemoteRecoveryClient(conn)
	if err != nil {
		t.Fatalf("create recovery client: %v", err)
	}
	if err := client.Recover(context.Background(), "job-a", "us-east-1", "eu-west-1", 7, "promotion-id-123456"); err != nil {
		t.Fatalf("recover through region service: %v", err)
	}
}

func TestEventSenderPushesCheckpointThroughGRPC(t *testing.T) {
	service := NewService(nil, nil)
	subscriber := make(chan *controlv1.CheckpointEvent, 2)
	service.subscribers["eu-west-1"] = map[chan *controlv1.CheckpointEvent]struct{}{subscriber: {}}

	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	controlv1.RegisterReplicationServiceServer(grpcServer, service)
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		_ = grpcServer.Serve(listener)
	}()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = listener.Close()
		<-serverDone
	})

	conn, err := grpc.DialContext(context.Background(), "bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	payload, err := proto.Marshal(checkpointEvent("us-east-1"))
	if err != nil {
		t.Fatalf("marshal checkpoint: %v", err)
	}
	sender := EventSenderFactory{}.NewEventSender(conn, "us-east-1", "eu-west-1")
	event := &channel.Event{
		Type:         channel.EventTypeCheckpoint,
		SourceRegion: "us-east-1",
		TargetRegion: "eu-west-1",
		Payload:      payload,
	}
	if err := sender.SendEvent(context.Background(), event); err != nil {
		t.Fatalf("push checkpoint: %v", err)
	}
	received := <-subscriber
	if received.GetWalEntry().GetSequence() != 1 {
		t.Fatalf("received sequence = %d, want 1", received.GetWalEntry().GetSequence())
	}
}

func TestEventSenderAcceptsAtLeastOnceRetryAcknowledgement(t *testing.T) {
	service := NewService(nil, nil)
	subscriber := make(chan *controlv1.CheckpointEvent, 2)
	service.subscribers["eu-west-1"] = map[chan *controlv1.CheckpointEvent]struct{}{subscriber: {}}

	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	controlv1.RegisterReplicationServiceServer(grpcServer, service)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = listener.Close()
	})
	conn, err := grpc.DialContext(context.Background(), "bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	payload, err := proto.Marshal(checkpointEvent("us-east-1"))
	if err != nil {
		t.Fatalf("marshal checkpoint: %v", err)
	}
	sender := EventSenderFactory{}.NewEventSender(conn, "us-east-1", "eu-west-1")
	event := &channel.Event{Type: channel.EventTypeCheckpoint, SourceRegion: "us-east-1", TargetRegion: "eu-west-1", Payload: payload}
	if err := sender.SendEvent(context.Background(), event); err != nil {
		t.Fatalf("first push checkpoint: %v", err)
	}
	if err := sender.SendEvent(context.Background(), event); err != nil {
		t.Fatalf("retry push checkpoint: %v", err)
	}
	for range 2 {
		if sequence := (<-subscriber).GetWalEntry().GetSequence(); sequence != 1 {
			t.Fatalf("retry sequence = %d, want 1", sequence)
		}
	}
}
