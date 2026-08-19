package channel

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestConfig_Defaults(t *testing.T) {
	cfg := Config{}

	if cfg.Region != "" {
		t.Error("expected empty region")
	}
	if cfg.PeerEndpoint != "" {
		t.Error("expected empty peer endpoint")
	}
}

func TestConfig_WithPeerEndpoint(t *testing.T) {
	cfg := Config{}
	WithPeerEndpoint("localhost:50051")(&cfg)

	if cfg.PeerEndpoint != "localhost:50051" {
		t.Errorf("expected localhost:50051, got %s", cfg.PeerEndpoint)
	}
}

func TestConfig_WithTLS(t *testing.T) {
	cfg := Config{}
	WithTLS("/ca.crt", "/client.crt", "/client.key", "server-name")(&cfg)

	if !cfg.EnableTLS {
		t.Error("expected TLS to be enabled")
	}
	if cfg.CACertPath != "/ca.crt" {
		t.Errorf("expected CA path /ca.crt, got %s", cfg.CACertPath)
	}
	if cfg.ServerName != "server-name" {
		t.Errorf("expected server name server-name, got %s", cfg.ServerName)
	}
}

func TestNewClient_NoPeerEndpoint(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	handler := EventHandlerFunc(func(ctx context.Context, event *Event) error { return nil })

	_, err := NewClient(context.Background(), logger, "us-east-1", handler)
	if !errors.Is(err, ErrNoPeerEndpoint) {
		t.Errorf("expected ErrNoPeerEndpoint, got %v", err)
	}
}

func TestNewClient_WithPeerEndpoint(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	handler := EventHandlerFunc(func(ctx context.Context, event *Event) error { return nil })

	client, err := NewClient(context.Background(), logger, "us-east-1", handler,
		WithPeerEndpoint("eu-west-1.astrasync.example:50051"),
		WithTLS("/ca.crt", "/client.crt", "/client.key", "eu-west-1"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.cfg.Region != "us-east-1" {
		t.Errorf("expected region us-east-1, got %s", client.cfg.Region)
	}
	if client.cfg.PeerEndpoint != "eu-west-1.astrasync.example:50051" {
		t.Errorf("unexpected peer endpoint: %s", client.cfg.PeerEndpoint)
	}
}

func TestClient_Close(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	handler := EventHandlerFunc(func(ctx context.Context, event *Event) error { return nil })

	client, err := NewClient(context.Background(), logger, "us-east-1", handler,
		WithPeerEndpoint("eu-west-1.astrasync.example:50051"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := client.Close(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if err := client.Close(); err != nil {
		t.Error("second close should not error")
	}
}

func TestClient_SendEvent_Closed(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	handler := EventHandlerFunc(func(ctx context.Context, event *Event) error { return nil })

	client, err := NewClient(context.Background(), logger, "us-east-1", handler,
		WithPeerEndpoint("eu-west-1.astrasync.example:50051"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	client.Close()

	event := NewHealthEvent("us-east-1", "eu-west-1", true)
	err = client.SendEvent(context.Background(), event)
	if !errors.Is(err, ErrClosed) {
		t.Errorf("expected ErrClosed, got %v", err)
	}
}

func TestNewServer(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	handler := EventHandlerFunc(func(ctx context.Context, event *Event) error { return nil })

	server := NewServer(logger, "us-east-1", handler)
	if server.cfg.Region != "us-east-1" {
		t.Errorf("expected region us-east-1, got %s", server.cfg.Region)
	}
}

func TestServer_HandleEvent(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	var receivedEvent *Event

	handler := EventHandlerFunc(func(ctx context.Context, event *Event) error {
		receivedEvent = event
		return nil
	})

	server := NewServer(logger, "us-east-1", handler)

	event := NewHealthEvent("eu-west-1", "us-east-1", true)
	err := server.HandleEvent(context.Background(), event)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if receivedEvent == nil {
		t.Fatal("event was not received by handler")
	}
	if receivedEvent.SourceRegion != "eu-west-1" {
		t.Errorf("expected source eu-west-1, got %s", receivedEvent.SourceRegion)
	}
}

func TestServer_HandleEvent_NotForThisRegion(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	var receivedCount int

	handler := EventHandlerFunc(func(ctx context.Context, event *Event) error {
		receivedCount++
		return nil
	})

	server := NewServer(logger, "us-east-1", handler)

	// Event targeted at ap-south-1, not us-east-1
	event := NewHealthEvent("eu-west-1", "ap-south-1", true)
	err := server.HandleEvent(context.Background(), event)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if receivedCount != 0 {
		t.Errorf("event should not have been received, got %d", receivedCount)
	}
}

func TestServer_HandleEvent_Wildcard(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	var receivedCount int

	handler := EventHandlerFunc(func(ctx context.Context, event *Event) error {
		receivedCount++
		return nil
	})

	server := NewServer(logger, "us-east-1", handler)

	// Event targeted at all regions (wildcard)
	event := NewHealthEvent("eu-west-1", "*", true)
	err := server.HandleEvent(context.Background(), event)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if receivedCount != 1 {
		t.Errorf("wildcard event should have been received, got %d", receivedCount)
	}
}

func TestServer_HandleEvent_NilEvent(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	handler := EventHandlerFunc(func(ctx context.Context, event *Event) error { return nil })

	server := NewServer(logger, "us-east-1", handler)

	err := server.HandleEvent(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil event")
	}
}

func TestNewHealthEvent(t *testing.T) {
	event := NewHealthEvent("us-east-1", "eu-west-1", true)

	if event.Type != EventTypeHealth {
		t.Errorf("expected EventTypeHealth, got %v", event.Type)
	}
	if event.SourceRegion != "us-east-1" {
		t.Errorf("expected source us-east-1, got %s", event.SourceRegion)
	}
	if event.TargetRegion != "eu-west-1" {
		t.Errorf("expected target eu-west-1, got %s", event.TargetRegion)
	}
	if event.Timestamp.IsZero() {
		t.Error("timestamp should be set")
	}
	if string(event.Payload) != "healthy" {
		t.Errorf("expected payload 'healthy', got %s", event.Payload)
	}
}

func TestNewCheckpointEvent(t *testing.T) {
	event := NewCheckpointEvent("us-east-1", "eu-west-1", "s3://bucket/checkpoint", 42)

	if event.Type != EventTypeCheckpoint {
		t.Errorf("expected EventTypeCheckpoint, got %v", event.Type)
	}
	if event.SourceRegion != "us-east-1" {
		t.Errorf("expected source us-east-1, got %s", event.SourceRegion)
	}
	if event.TargetRegion != "eu-west-1" {
		t.Errorf("expected target eu-west-1, got %s", event.TargetRegion)
	}
}

func TestNewTopologyEvent(t *testing.T) {
	event := NewTopologyEvent("us-east-1", "eu-west-1", 5)

	if event.Type != EventTypeTopology {
		t.Errorf("expected EventTypeTopology, got %v", event.Type)
	}
	if event.SourceRegion != "us-east-1" {
		t.Errorf("expected source us-east-1, got %s", event.SourceRegion)
	}
}

func TestEventType_String(t *testing.T) {
	tests := []struct {
		eventType EventType
		want      string
	}{
		{EventTypeUnknown, "unknown"},
		{EventTypeCheckpoint, "checkpoint"},
		{EventTypeTopology, "topology"},
		{EventTypeHealth, "health"},
	}

	for _, tt := range tests {
		if got := tt.eventType.String(); got != tt.want {
			t.Errorf("EventType(%d).String() = %s, want %s", tt.eventType, got, tt.want)
		}
	}
}

func TestClient_Stats(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	handler := EventHandlerFunc(func(ctx context.Context, event *Event) error { return nil })

	client, err := NewClient(context.Background(), logger, "us-east-1", handler,
		WithPeerEndpoint("eu-west-1.astrasync.example:50051"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stats := client.Stats()
	if stats.Connected {
		t.Error("expected not connected before Connect()")
	}
}

func TestServer_RegisterUnregisterStream(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	handler := EventHandlerFunc(func(ctx context.Context, event *Event) error { return nil })

	server := NewServer(logger, "us-east-1", handler)

	events := make(chan *Event, 10)
	server.RegisterStream("eu-west-1", events)
	server.UnregisterStream("eu-west-1")

	// Should not panic
}

func TestContext_Cancellation(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	handler := EventHandlerFunc(func(ctx context.Context, event *Event) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
			return nil
		}
	})

	server := NewServer(logger, "us-east-1", handler)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	event := NewHealthEvent("eu-west-1", "us-east-1", true)
	err := server.HandleEvent(ctx, event)
	if err == nil {
		t.Error("expected timeout error")
	}
}