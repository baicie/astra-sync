// Package promotion provides operator-initiated region promotion logic.
package promotion

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Common errors for promotion operations.
var (
	ErrNotStandbyRegion    = errors.New("promotion: target region is not a standby region")
	ErrAlreadyPromoting    = errors.New("promotion: promotion already in progress for this job")
	ErrJobNotFound         = errors.New("promotion: job not found")
	ErrEpochConflict       = errors.New("promotion: epoch conflict")
	ErrCapabilityTimeout   = errors.New("promotion: sink capability revalidation timeout")
	ErrCapabilityFailed    = errors.New("promotion: sink capability revalidation failed")
	ErrPromotionAborted    = errors.New("promotion: promotion aborted")
)

// PromotionState represents the state of a promotion.
type PromotionState int

const (
	StatePending PromotionState = iota
	StateEpochBumped
	StateEpochWritten
	StateCapabilityRevalidating
	StateCapabilityConfirmed
	StateFailoverComplete
	StatePromotionFailed
)

// String returns the string representation of the state.
func (s PromotionState) String() string {
	switch s {
	case StatePending:
		return "promotion_pending"
	case StateEpochBumped:
		return "epoch_bumped"
	case StateEpochWritten:
		return "epoch_written"
	case StateCapabilityRevalidating:
		return "capability_revalidating"
	case StateCapabilityConfirmed:
		return "capability_confirmed"
	case StateFailoverComplete:
		return "failover_complete"
	case StatePromotionFailed:
		return "promotion_failed"
	default:
		return "unknown"
	}
}

// Promotion represents a region promotion operation.
type Promotion struct {
	ID               string
	JobID            string
	PreviousRegion   string
	TargetRegion     string
	IdempotencyKey   string
	PreviousEpoch    int64
	NewEpoch         int64
	State            PromotionState
	ErrorMessage     string
	StartedAt        time.Time
	CompletedAt      *time.Time
	mu               sync.RWMutex
}

// NewPromotion creates a new promotion record.
func NewPromotion(jobID, previousRegion, targetRegion, idempotencyKey string, previousEpoch int64) *Promotion {
	return &Promotion{
		ID:               idempotencyKey,
		JobID:            jobID,
		PreviousRegion:   previousRegion,
		TargetRegion:     targetRegion,
		IdempotencyKey:  idempotencyKey,
		PreviousEpoch:    previousEpoch,
		NewEpoch:        previousEpoch + 1,
		State:            StatePending,
		StartedAt:        time.Now().UTC(),
	}
}

// TransitionTo transitions the promotion to a new state.
func (p *Promotion) TransitionTo(state PromotionState, errMsg string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.State == StatePromotionFailed {
		return fmt.Errorf("cannot transition from failed state")
	}

	if state <= p.State {
		return fmt.Errorf("cannot transition to earlier or same state: %s -> %s", p.State, state)
	}

	p.State = state
	if errMsg != "" {
		p.ErrorMessage = errMsg
	}
	if state == StatePromotionFailed || state == StateFailoverComplete {
		now := time.Now().UTC()
		p.CompletedAt = &now
	}

	return nil
}

// SetEpoch sets the new epoch for the promotion.
func (p *Promotion) SetEpoch(epoch int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.NewEpoch = epoch
}

// GetEpoch returns the new epoch for the promotion.
func (p *Promotion) GetEpoch() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.NewEpoch
}

// IsComplete returns true if the promotion is complete (success or failure).
func (p *Promotion) IsComplete() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.State == StateFailoverComplete || p.State == StatePromotionFailed
}

// IsFailed returns true if the promotion failed.
func (p *Promotion) IsFailed() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.State == StatePromotionFailed
}

// PromotionStore stores promotion records.
type PromotionStore interface {
	// Get returns a promotion by idempotency key.
	Get(ctx context.Context, jobID, idempotencyKey string) (*Promotion, error)
	// Create creates a new promotion.
	Create(ctx context.Context, promotion *Promotion) error
	// Update updates an existing promotion.
	Update(ctx context.Context, promotion *Promotion) error
	// GetLatest returns the most recent promotion for a job.
	GetLatest(ctx context.Context, jobID string) (*Promotion, error)
}

// EpochAssigner assigns new epochs.
type EpochAssigner interface {
	// Assign assigns a new epoch for the given job.
	Assign(ctx context.Context, jobID string) (int64, error)
}

// JobReader reads job records.
type JobReader interface {
	// GetEpoch returns the current epoch for a job.
	GetEpoch(ctx context.Context, jobID string) (int64, error)
}

// JobWriter writes job records.
type JobWriter interface {
	// UpdateEpoch updates the epoch for a job with optimistic locking.
	UpdateEpoch(ctx context.Context, jobID string, expectedEpoch int64, newEpoch int64) error
}

// CapabilityRevalidator revalidates sink capabilities.
type CapabilityRevalidator interface {
	// Revalidate revalidates the sink capability for a job.
	Revalidate(ctx context.Context, jobID string, timeout time.Duration) error
}

// Manager manages region promotions.
type Manager struct {
	cfg      Config
	logger   *zap.Logger
	store    PromotionStore
	assigner EpochAssigner
	jobReader JobReader
	jobWriter JobWriter
	revalidator CapabilityRevalidator

	mu          sync.RWMutex
	promotions  map[string]*Promotion // key: jobID
}

// Config holds the configuration for the promotion manager.
type Config struct {
	// Capability revalidation timeout.
	CapabilityTimeout time.Duration
}

// Option is a functional option for manager configuration.
type Option func(*Config)

// WithCapabilityTimeout sets the capability revalidation timeout.
func WithCapabilityTimeout(d time.Duration) Option {
	return func(c *Config) {
		c.CapabilityTimeout = d
	}
}

// NewManager creates a new promotion manager.
func NewManager(logger *zap.Logger, store PromotionStore, assigner EpochAssigner, jobReader JobReader, jobWriter JobWriter, opts ...Option) (*Manager, error) {
	cfg := Config{
		CapabilityTimeout: 60 * time.Second,
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	return &Manager{
		cfg:        cfg,
		logger:     logger.With(zap.String("component", "promotion")),
		store:      store,
		assigner:   assigner,
		jobReader:  jobReader,
		jobWriter:  jobWriter,
		promotions: make(map[string]*Promotion),
	}, nil
}

// Promote initiates a region promotion.
func (m *Manager) Promote(ctx context.Context, jobID, targetRegion, idempotencyKey string, expectedVersion int64) (*Promotion, error) {
	// Check if idempotency key already exists
	if existing, err := m.store.Get(ctx, jobID, idempotencyKey); err == nil && existing != nil {
		m.logger.Info("returning existing promotion",
			zap.String("jobID", jobID),
			zap.String("idempotencyKey", idempotencyKey))
		return existing, nil
	}

	// Get current job epoch
	currentEpoch, err := m.jobReader.GetEpoch(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("get current epoch: %w", err)
	}

	// Check version
	if currentEpoch != expectedVersion {
		return nil, ErrEpochConflict
	}

	// Create promotion
	promotion := NewPromotion(jobID, "current-region", targetRegion, idempotencyKey, currentEpoch)

	// Track in memory
	m.mu.Lock()
	m.promotions[jobID] = promotion
	m.mu.Unlock()

	if err := m.store.Create(ctx, promotion); err != nil {
		return nil, fmt.Errorf("create promotion: %w", err)
	}

	// Transition to epoch bumped
	if err := promotion.TransitionTo(StateEpochBumped, ""); err != nil {
		return nil, fmt.Errorf("transition to epoch bumped: %w", err)
	}

	// Assign new epoch
	newEpoch, err := m.assigner.Assign(ctx, jobID)
	if err != nil {
		promotion.TransitionTo(StatePromotionFailed, err.Error())
		m.store.Update(ctx, promotion)
		return nil, fmt.Errorf("assign epoch: %w", err)
	}
	promotion.SetEpoch(newEpoch)

	// Transition to epoch written
	if err := promotion.TransitionTo(StateEpochWritten, ""); err != nil {
		return nil, fmt.Errorf("transition to epoch written: %w", err)
	}

	// Write epoch to job record
	if err := m.jobWriter.UpdateEpoch(ctx, jobID, currentEpoch, newEpoch); err != nil {
		promotion.TransitionTo(StatePromotionFailed, err.Error())
		m.store.Update(ctx, promotion)
		return nil, fmt.Errorf("update job epoch: %w", err)
	}

	// Transition to capability revalidating
	if err := promotion.TransitionTo(StateCapabilityRevalidating, ""); err != nil {
		return nil, fmt.Errorf("transition to capability revalidating: %w", err)
	}

	// Revalidate sink capability
	if m.revalidator != nil {
		if err := m.revalidator.Revalidate(ctx, jobID, m.cfg.CapabilityTimeout); err != nil {
			promotion.TransitionTo(StatePromotionFailed, err.Error())
			m.store.Update(ctx, promotion)
			return nil, fmt.Errorf("revalidate capability: %w", err)
		}
	}

	// Transition to capability confirmed
	if err := promotion.TransitionTo(StateCapabilityConfirmed, ""); err != nil {
		return nil, fmt.Errorf("transition to capability confirmed: %w", err)
	}

	// Transition to failover complete
	if err := promotion.TransitionTo(StateFailoverComplete, ""); err != nil {
		return nil, fmt.Errorf("transition to failover complete: %w", err)
	}

	// Update store
	if err := m.store.Update(ctx, promotion); err != nil {
		m.logger.Warn("failed to update promotion in store", zap.Error(err))
	}

	m.logger.Info("promotion completed",
		zap.String("jobID", jobID),
		zap.String("targetRegion", targetRegion),
		zap.Int64("newEpoch", newEpoch))

	return promotion, nil
}

// GetStatus returns the current promotion status.
func (m *Manager) GetStatus(ctx context.Context, jobID, idempotencyKey string) (*Promotion, error) {
	if idempotencyKey != "" {
		return m.store.Get(ctx, jobID, idempotencyKey)
	}
	return m.store.GetLatest(ctx, jobID)
}

// PromotionStats holds statistics for promotions.
type PromotionStats struct {
	TotalPromotions    int64
	SuccessfulPromotions int64
	FailedPromotions   int64
}

// Stats returns promotion statistics.
func (m *Manager) Stats(ctx context.Context) (PromotionStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var stats PromotionStats
	for _, p := range m.promotions {
		stats.TotalPromotions++
		if p.IsComplete() {
			if p.IsFailed() {
				stats.FailedPromotions++
			} else {
				stats.SuccessfulPromotions++
			}
		}
	}
	return stats, nil
}
