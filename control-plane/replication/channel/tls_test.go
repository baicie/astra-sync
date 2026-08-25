package channel

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

func TestNewClientRejectsIncompleteTLSConfiguration(t *testing.T) {
	_, err := NewClient(context.Background(), testLogger(), "east", tlsTestHandler{}, WithPeerEndpoint("127.0.0.1:1"), WithTLS("ca.pem", "client.pem", "client.key", ""))
	if err != ErrTLSConfigMissing {
		t.Fatalf("NewClient() error = %v, want %v", err, ErrTLSConfigMissing)
	}
}

type tlsTestHandler struct{}

func (tlsTestHandler) HandleEvent(context.Context, *Event) error { return nil }

func testLogger() *zap.Logger { return zap.NewNop() }
