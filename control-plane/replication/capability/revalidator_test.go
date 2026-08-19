package capability

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestCapability_String(t *testing.T) {
	tests := []struct {
		capability Capability
		want       string
	}{
		{CapabilityExactlyOnce, "exactly-once"},
		{CapabilityAtLeastOnce, "at-least-once"},
		{CapabilityAtMostOnce, "at-most-once"},
	}

	for _, tt := range tests {
		if got := tt.capability.String(); got != tt.want {
			t.Errorf("Capability(%d).String() = %s, want %s", tt.capability, got, tt.want)
		}
	}
}

func TestRevalidationResult_String(t *testing.T) {
	result := &RevalidationResult{
		Reachable:  true,
		Capability: CapabilityExactlyOnce,
		Duration:   100 * time.Millisecond,
	}

	got := result.String()
	if got == "" {
		t.Error("expected non-empty string for reachable result")
	}

	result.Reachable = false
	result.ErrorMessage = "connection refused"
	got = result.String()
	if got == "" {
		t.Error("expected non-empty string for unreachable result")
	}
}

// Mock implementations

type mockConnectionCatalog struct {
	connection *ConnectionInfo
	err        error
}

func (m *mockConnectionCatalog) GetConnection(ctx context.Context, jobID string) (*ConnectionInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.connection, nil
}

type mockCapabilityNegotiator struct {
	capability Capability
	err        error
}

func (m *mockCapabilityNegotiator) Negotiate(ctx context.Context, endpoint string) (Capability, error) {
	return m.capability, m.err
}

type mockAuditLogger struct {
	events []string
}

func (m *mockAuditLogger) LogCapabilityRevalidationStarted(ctx context.Context, jobID string) {
	m.events = append(m.events, "started:"+jobID)
}

func (m *mockAuditLogger) LogCapabilityConfirmed(ctx context.Context, jobID string, capability Capability, duration time.Duration) {
	m.events = append(m.events, "confirmed:"+jobID)
}

func (m *mockAuditLogger) LogCapabilityRejected(ctx context.Context, jobID string, reason string) {
	m.events = append(m.events, "rejected:"+jobID+":"+reason)
}

func (m *mockAuditLogger) LogPromotionAborted(ctx context.Context, jobID string, reason string) {
	m.events = append(m.events, "aborted:"+jobID+":"+reason)
}

func TestRevalidator_Revalidate(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	catalog := &mockConnectionCatalog{
		connection: &ConnectionInfo{
			JobID:        "job-1",
			SinkEndpoint: "jdbc:postgresql://sink:5432/db",
		},
	}
	negotiator := &mockCapabilityNegotiator{
		capability: CapabilityExactlyOnce,
	}
	auditor := &mockAuditLogger{}

	revalidator := NewRevalidator(logger, catalog, negotiator, auditor)

	err := revalidator.Revalidate(context.Background(), "job-1", 0)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(auditor.events) != 2 {
		t.Errorf("expected 2 audit events, got %d: %v", len(auditor.events), auditor.events)
	}
}

func TestRevalidator_Revalidate_GetConnectionError(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	catalog := &mockConnectionCatalog{
		err: errors.New("connection not found"),
	}
	negotiator := &mockCapabilityNegotiator{}
	auditor := &mockAuditLogger{}

	revalidator := NewRevalidator(logger, catalog, negotiator, auditor)

	err := revalidator.Revalidate(context.Background(), "job-1", 0)
	if err == nil {
		t.Error("expected error")
	}

	if len(auditor.events) != 3 {
		t.Errorf("expected 3 audit events (started, rejected, aborted), got %d: %v", len(auditor.events), auditor.events)
	}
}

func TestRevalidator_Revalidate_NegotiationError(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	catalog := &mockConnectionCatalog{
		connection: &ConnectionInfo{
			JobID:        "job-1",
			SinkEndpoint: "jdbc:postgresql://sink:5432/db",
		},
	}
	negotiator := &mockCapabilityNegotiator{
		err: errors.New("negotiation failed"),
	}
	auditor := &mockAuditLogger{}

	revalidator := NewRevalidator(logger, catalog, negotiator, auditor,
		WithMaxRetries(1),
		WithRetryBackoff(10*time.Millisecond))

	err := revalidator.Revalidate(context.Background(), "job-1", 0)
	if err == nil {
		t.Error("expected error")
	}

	if len(auditor.events) != 3 {
		t.Errorf("expected 3 audit events, got %d: %v", len(auditor.events), auditor.events)
	}
}

func TestRevalidator_Revalidate_SuccessOnRetry(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	catalog := &mockConnectionCatalog{
		connection: &ConnectionInfo{
			JobID:        "job-1",
			SinkEndpoint: "jdbc:postgresql://sink:5432/db",
		},
	}
	negotiator := &mockCapabilityNegotiator{
		capability: CapabilityExactlyOnce,
	}
	auditor := &mockAuditLogger{}

	revalidator := NewRevalidator(logger, catalog, negotiator, auditor)

	err := revalidator.Revalidate(context.Background(), "job-1", 0)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRevalidator_GetRevalidationResult(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	catalog := &mockConnectionCatalog{
		connection: &ConnectionInfo{
			JobID:        "job-1",
			SinkEndpoint: "jdbc:postgresql://sink:5432/db",
		},
	}
	negotiator := &mockCapabilityNegotiator{
		capability: CapabilityExactlyOnce,
	}

	revalidator := NewRevalidator(logger, catalog, negotiator, nil)

	result, err := revalidator.GetRevalidationResult(context.Background(), "job-1", 0)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if !result.Reachable {
		t.Error("expected reachable")
	}
	if result.Capability != CapabilityExactlyOnce {
		t.Errorf("expected CapabilityExactlyOnce, got %v", result.Capability)
	}
}

func TestRevalidator_GetRevalidationResult_GetConnectionError(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	catalog := &mockConnectionCatalog{
		err: errors.New("not found"),
	}
	negotiator := &mockCapabilityNegotiator{}

	revalidator := NewRevalidator(logger, catalog, negotiator, nil)

	result, err := revalidator.GetRevalidationResult(context.Background(), "job-1", 0)
	if err == nil {
		t.Error("expected error")
	}

	if result.Reachable {
		t.Error("expected not reachable")
	}
}

func TestNewRevalidator_Defaults(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	revalidator := NewRevalidator(logger, nil, nil, nil)

	if revalidator.cfg.Timeout != 60*time.Second {
		t.Errorf("expected default timeout 60s, got %v", revalidator.cfg.Timeout)
	}
	if revalidator.cfg.MaxRetries != 3 {
		t.Errorf("expected default maxRetries 3, got %d", revalidator.cfg.MaxRetries)
	}
}

func TestWithTimeout(t *testing.T) {
	cfg := Config{}
	WithTimeout(30 * time.Second)(&cfg)

	if cfg.Timeout != 30*time.Second {
		t.Errorf("expected timeout 30s, got %v", cfg.Timeout)
	}
}

func TestWithProbeTimeout(t *testing.T) {
	cfg := Config{}
	WithProbeTimeout(10 * time.Second)(&cfg)

	if cfg.ProbeTimeout != 10*time.Second {
		t.Errorf("expected probe timeout 10s, got %v", cfg.ProbeTimeout)
	}
}

func TestWithMaxRetries(t *testing.T) {
	cfg := Config{}
	WithMaxRetries(5)(&cfg)

	if cfg.MaxRetries != 5 {
		t.Errorf("expected maxRetries 5, got %d", cfg.MaxRetries)
	}
}

func TestWithRetryBackoff(t *testing.T) {
	cfg := Config{}
	WithRetryBackoff(5 * time.Second)(&cfg)

	if cfg.RetryBackoff != 5*time.Second {
		t.Errorf("expected retry backoff 5s, got %v", cfg.RetryBackoff)
	}
}
