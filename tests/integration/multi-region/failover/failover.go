// Package failover provides failover integration tests.
package failover

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/baicie/astrasync/tests/integration/multi-region/framework"
	"go.uber.org/zap"
)

// Common errors for failover tests.
var (
	ErrPromotionFailed       = errors.New("failover: promotion failed")
	ErrPromotionTimeout      = errors.New("failover: promotion timeout")
	ErrInvalidPromotionState = errors.New("failover: invalid promotion state")
	ErrEpochNotAdvanced      = errors.New("failover: epoch not advanced")
)

// PromotionStatus represents the status of a failover promotion.
type PromotionStatus struct {
	JobID         string
	State         string
	PreviousEpoch int64
	NewEpoch      int64
	StartedAt     time.Time
	CompletedAt   *time.Time
}

// PromotionState string values.
const (
	StatePromotionPending  = "promotion_pending"
	StateEpochBumped       = "epoch_bumped"
	StateEpochWritten      = "epoch_written"
	StateCapabilityConfirming = "capability_revalidating"
	StateCapabilityConfirmed = "capability_confirmed"
	StateFailoverComplete  = "failover_complete"
	StatePromotionFailed   = "promotion_failed"
)

// FailoverClient simulates the failover promotion client.
type FailoverClient struct {
	cfg    FailoverConfig
	logger *zap.Logger

	mu    sync.RWMutex
	promotions map[string]*PromotionStatus
}

// FailoverConfig holds the failover client configuration.
type FailoverConfig struct {
	// Timeout for the promotion request.
	PromotionTimeout time.Duration
	// CapabilityTimeout is the timeout for capability revalidation.
	CapabilityTimeout time.Duration
}

// Option is a functional option for failover client configuration.
type Option func(*FailoverConfig)

// WithPromotionTimeout sets the promotion timeout.
func WithPromotionTimeout(d time.Duration) Option {
	return func(c *FailoverConfig) {
		c.PromotionTimeout = d
	}
}

// WithCapabilityTimeout sets the capability timeout.
func WithCapabilityTimeout(d time.Duration) Option {
	return func(c *FailoverConfig) {
		c.CapabilityTimeout = d
	}
}

// NewFailoverClient creates a new failover client.
func NewFailoverClient(opts ...Option) *FailoverClient {
	cfg := FailoverConfig{
		PromotionTimeout:  60 * time.Second,
		CapabilityTimeout: 30 * time.Second,
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	logger, _ := zap.NewDevelopment()

	return &FailoverClient{
		cfg:        cfg,
		logger:     logger.With(zap.String("component", "failover-client")),
		promotions: make(map[string]*PromotionStatus),
	}
}

// PromoteRegion promotes a job to a target region.
func (c *FailoverClient) PromoteRegion(ctx context.Context, jobID, targetRegion, idempotencyKey string, expectedVersion int64) (*PromotionStatus, error) {
	c.mu.Lock()

	// Check idempotency
	if existing, ok := c.promotions[idempotencyKey]; ok {
		c.mu.Unlock()
		c.logger.Info("returning existing promotion",
			zap.String("jobID", jobID),
			zap.String("idempotencyKey", idempotencyKey))
		return existing, nil
	}

	// Check version
	if expectedVersion < 0 {
		c.mu.Unlock()
		return nil, ErrEpochNotAdvanced
	}

	// Create promotion
	promotion := &PromotionStatus{
		JobID:         jobID,
		State:         StatePromotionPending,
		PreviousEpoch: expectedVersion,
		NewEpoch:      expectedVersion + 1,
		StartedAt:     time.Now().UTC(),
	}

	c.promotions[idempotencyKey] = promotion
	c.mu.Unlock()

	// Execute promotion states
	if err := c.executePromotion(ctx, promotion); err != nil {
		c.failPromotion(promotion, err.Error())
		return promotion, err
	}

	return promotion, nil
}

// executePromotion simulates the promotion state transitions.
func (c *FailoverClient) executePromotion(ctx context.Context, promotion *PromotionStatus) error {
	// Transition through states
	states := []string{
		StateEpochBumped,
		StateEpochWritten,
		StateCapabilityConfirming,
		StateCapabilityConfirmed,
		StateFailoverComplete,
	}

	for _, state := range states {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
			c.mu.Lock()
			promotion.State = state
			c.mu.Unlock()
			c.logger.Debug("promotion state transition",
				zap.String("jobID", promotion.JobID),
				zap.String("state", state))
		}
	}

	c.mu.Lock()
	now := time.Now().UTC()
	promotion.CompletedAt = &now
	c.mu.Unlock()

	return nil
}

// failPromotion marks a promotion as failed.
func (c *FailoverClient) failPromotion(promotion *PromotionStatus, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	promotion.State = StatePromotionFailed
	now := time.Now().UTC()
	promotion.CompletedAt = &now

	c.logger.Warn("promotion failed",
		zap.String("jobID", promotion.JobID),
		zap.String("reason", reason))
}

// GetStatus returns the promotion status by idempotency key.
func (c *FailoverClient) GetStatus(idempotencyKey string) (*PromotionStatus, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	promotion, ok := c.promotions[idempotencyKey]
	return promotion, ok
}

// Reset clears all promotions.
func (c *FailoverClient) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.promotions = make(map[string]*PromotionStatus)
}

// Helper to wait for promotion complete.
func (c *FailoverClient) WaitForPromotionComplete(ctx context.Context, idempotencyKey string, timeout time.Duration) error {
	timer := time.After(timeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer:
			return ErrPromotionTimeout
		case <-ticker.C:
			c.mu.RLock()
			promotion, ok := c.promotions[idempotencyKey]
			c.mu.RUnlock()

			if !ok {
				return ErrInvalidPromotionState
			}

			if promotion.State == StateFailoverComplete {
				return nil
			}
			if promotion.State == StatePromotionFailed {
				return ErrPromotionFailed
			}
		}
	}
}

// VerifyFailoverComplete checks that the promotion is complete and epoch is advanced.
func VerifyFailoverComplete(t *testing.T, promotion *PromotionStatus, expectedPreviousEpoch int64) {
	t.Helper()

	if promotion.State != StateFailoverComplete {
		t.Errorf("expected state %s, got %s", StateFailoverComplete, promotion.State)
	}

	if promotion.NewEpoch <= expectedPreviousEpoch {
		t.Errorf("expected epoch to advance from %d, got %d", expectedPreviousEpoch, promotion.NewEpoch)
	}

	if promotion.CompletedAt == nil {
		t.Error("expected completed timestamp to be set")
	}
}

// VerifyFailoverFailed checks that the promotion failed.
func VerifyFailoverFailed(t *testing.T, promotion *PromotionStatus) {
	t.Helper()

	if promotion.State != StatePromotionFailed {
		t.Errorf("expected state %s, got %s", StatePromotionFailed, promotion.State)
	}
}

// useFramework is a helper to use the framework in tests.
func useFramework(t *testing.T) *framework.Framework {
	t.Helper()
	return framework.New(t)
}

var _ = useFramework // Ensure framework is imported
