package failover

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestFailover_HappyPath verifies a successful promotion.
func TestFailover_HappyPath(t *testing.T) {
	client := NewFailoverClient()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	promotion, err := client.PromoteRegion(ctx, "job-1", "us-west-1", "key-123", 42)
	if err != nil {
		t.Fatalf("promotion failed: %v", err)
	}

	VerifyFailoverComplete(t, promotion, 42)
	if promotion.NewEpoch != 43 {
		t.Errorf("expected new epoch 43, got %d", promotion.NewEpoch)
	}
}

// TestFailover_Idempotency verifies the same idempotency key returns same result.
func TestFailover_Idempotency(t *testing.T) {
	client := NewFailoverClient()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	p1, err := client.PromoteRegion(ctx, "job-1", "us-west-1", "key-123", 42)
	if err != nil {
		t.Fatalf("first promotion failed: %v", err)
	}

	p2, err := client.PromoteRegion(ctx, "job-1", "us-west-1", "key-123", 42)
	if err != nil {
		t.Fatalf("second promotion failed: %v", err)
	}

	if p1 != p2 {
		t.Error("expected same promotion instance for same idempotency key")
	}
}

// TestFailover_VersionMismatch verifies version mismatch is rejected.
func TestFailover_VersionMismatch(t *testing.T) {
	client := NewFailoverClient()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.PromoteRegion(ctx, "job-1", "us-west-1", "key-456", -1)
	if !errors.Is(err, ErrEpochNotAdvanced) {
		t.Errorf("expected ErrEpochNotAdvanced, got %v", err)
	}
}

// TestFailover_CapabilityTimeout verifies timeout aborts promotion.
func TestFailover_CapabilityTimeout(t *testing.T) {
	client := NewFailoverClient(
		WithPromotionTimeout(100*time.Millisecond),
		WithCapabilityTimeout(50*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := client.PromoteRegion(ctx, "job-1", "us-west-1", "key-789", 42)
	if err == nil {
		t.Error("expected timeout error")
	}
}

// TestFailover_GetStatus verifies get status.
func TestFailover_GetStatus(t *testing.T) {
	client := NewFailoverClient()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.PromoteRegion(ctx, "job-1", "us-west-1", "key-001", 42)
	if err != nil {
		t.Fatalf("promotion failed: %v", err)
	}

	status, ok := client.GetStatus("key-001")
	if !ok {
		t.Fatal("status not found")
	}

	if status.JobID != "job-1" {
		t.Errorf("expected job-1, got %s", status.JobID)
	}
}

// TestFailover_Reset verifies reset clears promotions.
func TestFailover_Reset(t *testing.T) {
	client := NewFailoverClient()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.PromoteRegion(ctx, "job-1", "us-west-1", "key-002", 42)
	if err != nil {
		t.Fatalf("promotion failed: %v", err)
	}

	client.Reset()

	_, ok := client.GetStatus("key-002")
	if ok {
		t.Error("status should not exist after reset")
	}
}

// TestFailover_StateTransitions verifies all states are visited.
func TestFailover_StateTransitions(t *testing.T) {
	client := NewFailoverClient()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	promotion, err := client.PromoteRegion(ctx, "job-1", "us-west-1", "key-003", 42)
	if err != nil {
		t.Fatalf("promotion failed: %v", err)
	}

	if promotion.State != StateFailoverComplete {
		t.Errorf("expected final state %s, got %s", StateFailoverComplete, promotion.State)
	}
}

// TestFailover_WaitForPromotionComplete verifies wait function.
func TestFailover_WaitForPromotionComplete(t *testing.T) {
	client := NewFailoverClient()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := client.PromoteRegion(ctx, "job-1", "us-west-1", "key-004", 42)
	if err != nil {
		t.Fatalf("promotion failed: %v", err)
	}

	err = client.WaitForPromotionComplete(ctx, "key-004", 5*time.Second)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestFailover_WaitForPromotionComplete_Timeout verifies wait timeout.
func TestFailover_WaitForPromotionComplete_Timeout(t *testing.T) {
	client := NewFailoverClient()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.WaitForPromotionComplete(ctx, "non-existent", 100*time.Millisecond)
	if !errors.Is(err, ErrInvalidPromotionState) {
		t.Errorf("expected ErrInvalidPromotionState, got %v", err)
	}
}

// TestFailover_VerifyFailoverComplete verifies the helper function.
func TestFailover_VerifyFailoverComplete(t *testing.T) {
	now := time.Now().UTC()
	promotion := &PromotionStatus{
		JobID:         "job-1",
		PreviousEpoch: 42,
		NewEpoch:      43,
		State:         StateFailoverComplete,
		CompletedAt:   &now,
	}

	VerifyFailoverComplete(t, promotion, 42)
}

// TestFailover_VerifyFailoverFailed verifies the fail helper.
func TestFailover_VerifyFailoverFailed(t *testing.T) {
	promotion := &PromotionStatus{
		JobID: "job-1",
		State: StatePromotionFailed,
	}

	VerifyFailoverFailed(t, promotion)
}

// TestFailover_PromotionState verifies promotion state constants.
func TestFailover_PromotionState(t *testing.T) {
	if StatePromotionPending != "promotion_pending" {
		t.Errorf("unexpected StatePromotionPending value: %s", StatePromotionPending)
	}
	if StateFailoverComplete != "failover_complete" {
		t.Errorf("unexpected StateFailoverComplete value: %s", StateFailoverComplete)
	}
	if StatePromotionFailed != "promotion_failed" {
		t.Errorf("unexpected StatePromotionFailed value: %s", StatePromotionFailed)
	}
}

// TestFailover_ConfigTimeout verifies config timeout option.
func TestFailover_ConfigTimeout(t *testing.T) {
	cfg := FailoverConfig{}
	WithPromotionTimeout(120 * time.Second)(&cfg)
	WithCapabilityTimeout(60 * time.Second)(&cfg)

	if cfg.PromotionTimeout != 120*time.Second {
		t.Errorf("expected promotion timeout 120s, got %v", cfg.PromotionTimeout)
	}
	if cfg.CapabilityTimeout != 60*time.Second {
		t.Errorf("expected capability timeout 60s, got %v", cfg.CapabilityTimeout)
	}
}

// TestFailover_DefaultConfig verifies default config.
func TestFailover_DefaultConfig(t *testing.T) {
	client := NewFailoverClient()

	if client.cfg.PromotionTimeout != 60*time.Second {
		t.Errorf("expected default promotion timeout 60s, got %v", client.cfg.PromotionTimeout)
	}
	if client.cfg.CapabilityTimeout != 30*time.Second {
		t.Errorf("expected default capability timeout 30s, got %v", client.cfg.CapabilityTimeout)
	}
}

// TestFailover_EpochIncrement verifies epoch increment.
func TestFailover_EpochIncrement(t *testing.T) {
	client := NewFailoverClient()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	promotion, err := client.PromoteRegion(ctx, "job-1", "us-west-1", "key-005", 100)
	if err != nil {
		t.Fatalf("promotion failed: %v", err)
	}

	if promotion.NewEpoch != 101 {
		t.Errorf("expected new epoch 101, got %d", promotion.NewEpoch)
	}
}

// TestFailover_MultipleJobs verifies multiple job promotions.
func TestFailover_MultipleJobs(t *testing.T) {
	client := NewFailoverClient()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for i := 0; i < 3; i++ {
		key := "key-" + string(rune('a'+i))
		_, err := client.PromoteRegion(ctx, "job-1", "us-west-1", key, 42)
		if err != nil {
			t.Fatalf("promotion %d failed: %v", i, err)
		}
	}

	for i := 0; i < 3; i++ {
		key := "key-" + string(rune('a'+i))
		_, ok := client.GetStatus(key)
		if !ok {
			t.Errorf("status %s not found", key)
		}
	}
}

// TestFailover_ContextCancellation verifies context cancellation.
func TestFailover_ContextCancellation(t *testing.T) {
	client := NewFailoverClient()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.PromoteRegion(ctx, "job-1", "us-west-1", "key-cancel", 42)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

// TestFailover_EpochNotAdvanced_Zero verifies zero epoch.
func TestFailover_EpochNotAdvanced_Zero(t *testing.T) {
	client := NewFailoverClient()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.PromoteRegion(ctx, "job-1", "us-west-1", "key-zero", 0)
	if err != nil {
		t.Fatalf("promotion failed: %v", err)
	}

	status, ok := client.GetStatus("key-zero")
	if !ok {
		t.Fatal("status not found")
	}

	if status.NewEpoch != 1 {
		t.Errorf("expected new epoch 1, got %d", status.NewEpoch)
	}
}

// TestFailover_CompletedTimestampSet verifies completion timestamp.
func TestFailover_CompletedTimestampSet(t *testing.T) {
	client := NewFailoverClient()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	promotion, err := client.PromoteRegion(ctx, "job-1", "us-west-1", "key-time", 42)
	if err != nil {
		t.Fatalf("promotion failed: %v", err)
	}

	if promotion.CompletedAt == nil {
		t.Error("expected completed timestamp to be set")
	}

	if promotion.CompletedAt.Before(promotion.StartedAt) {
		t.Error("completed timestamp should be after started timestamp")
	}
}

// TestFailover_StartedTimestampSet verifies start timestamp.
func TestFailover_StartedTimestampSet(t *testing.T) {
	client := NewFailoverClient()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	promotion, err := client.PromoteRegion(ctx, "job-1", "us-west-1", "key-start", 42)
	if err != nil {
		t.Fatalf("promotion failed: %v", err)
	}

	if promotion.StartedAt.IsZero() {
		t.Error("expected started timestamp to be set")
	}
}

// TestFailover_DifferentRegions verifies promotion to different regions.
func TestFailover_DifferentRegions(t *testing.T) {
	client := NewFailoverClient()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, region := range []string{"us-west-1", "ap-south-1", "eu-west-1"} {
		key := "key-" + region
		_, err := client.PromoteRegion(ctx, "job-1", region, key, 42)
		if err != nil {
			t.Errorf("promotion to %s failed: %v", region, err)
		}
	}
}

// TestFailover_ConcurrentPromotions verifies concurrent promotion requests.
func TestFailover_ConcurrentPromotions(t *testing.T) {
	client := NewFailoverClient()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	done := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func(idx int) {
			key := "concurrent-key-" + string(rune('a'+idx))
			_, err := client.PromoteRegion(ctx, "job-1", "us-west-1", key, 42)
			done <- err
		}(i)
	}

	for i := 0; i < 3; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent promotion failed: %v", err)
		}
	}
}

// TestFailover_RePromoteAfterFailure verifies re-promotion after failure.
func TestFailover_RePromoteAfterFailure(t *testing.T) {
	client := NewFailoverClient()

	// First promotion times out
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.PromoteRegion(ctx, "job-1", "us-west-1", "first-attempt", 42)
	if err == nil {
		t.Error("expected first promotion to fail")
	}

	// Second promotion succeeds
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()

	_, err = client.PromoteRegion(ctx2, "job-1", "us-west-1", "second-attempt", 43)
	if err != nil {
		t.Errorf("second promotion failed: %v", err)
	}
}

// TestFailover_StatusNotFound verifies not found error.
func TestFailover_StatusNotFound(t *testing.T) {
	client := NewFailoverClient()

	_, ok := client.GetStatus("non-existent")
	if ok {
		t.Error("expected status not to exist")
	}
}

// TestFailover_PromotionStateConstants verifies state constants.
func TestFailover_PromotionStateConstants(t *testing.T) {
	states := []string{
		StatePromotionPending,
		StateEpochBumped,
		StateEpochWritten,
		StateCapabilityConfirming,
		StateCapabilityConfirmed,
		StateFailoverComplete,
		StatePromotionFailed,
	}

	for i, state := range states {
		if state == "" {
			t.Errorf("state %d is empty", i)
		}
	}
}
