package promotion

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestNewPromotion(t *testing.T) {
	p := NewPromotion("job-1", "us-east-1", "eu-west-1", "key-123", 42)

	if p.JobID != "job-1" {
		t.Errorf("expected job-1, got %s", p.JobID)
	}
	if p.PreviousRegion != "us-east-1" {
		t.Errorf("expected us-east-1, got %s", p.PreviousRegion)
	}
	if p.TargetRegion != "eu-west-1" {
		t.Errorf("expected eu-west-1, got %s", p.TargetRegion)
	}
	if p.IdempotencyKey != "key-123" {
		t.Errorf("expected key-123, got %s", p.IdempotencyKey)
	}
	if p.PreviousEpoch != 42 {
		t.Errorf("expected previous epoch 42, got %d", p.PreviousEpoch)
	}
	if p.NewEpoch != 43 {
		t.Errorf("expected new epoch 43, got %d", p.NewEpoch)
	}
	if p.State != StatePending {
		t.Errorf("expected StatePending, got %v", p.State)
	}
	if p.StartedAt.IsZero() {
		t.Error("StartedAt should be set")
	}
}

func TestPromotionState_String(t *testing.T) {
	tests := []struct {
		state PromotionState
		want  string
	}{
		{StatePending, "promotion_pending"},
		{StateEpochBumped, "epoch_bumped"},
		{StateEpochWritten, "epoch_written"},
		{StateCapabilityRevalidating, "capability_revalidating"},
		{StateCapabilityConfirmed, "capability_confirmed"},
		{StateFailoverComplete, "failover_complete"},
		{StatePromotionFailed, "promotion_failed"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("PromotionState(%d).String() = %s, want %s", tt.state, got, tt.want)
		}
	}
}

func TestPromotion_TransitionTo(t *testing.T) {
	p := NewPromotion("job-1", "us-east-1", "eu-west-1", "key-123", 42)

	// Valid transition
	if err := p.TransitionTo(StateEpochBumped, ""); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if p.State != StateEpochBumped {
		t.Errorf("expected StateEpochBumped, got %v", p.State)
	}

	// Transition to same state
	if err := p.TransitionTo(StateEpochBumped, ""); err == nil {
		t.Error("expected error for same state transition")
	}

	// Transition to earlier state
	if err := p.TransitionTo(StatePending, ""); err == nil {
		t.Error("expected error for earlier state transition")
	}
}

func TestPromotion_TransitionTo_Failed(t *testing.T) {
	p := NewPromotion("job-1", "us-east-1", "eu-west-1", "key-123", 42)
	p.TransitionTo(StatePromotionFailed, "test error")

	// Cannot transition from failed state
	if err := p.TransitionTo(StateEpochBumped, ""); err == nil {
		t.Error("expected error transitioning from failed state")
	}

	if !p.IsFailed() {
		t.Error("expected IsFailed() to return true")
	}
}

func TestPromotion_TransitionTo_Complete(t *testing.T) {
	p := NewPromotion("job-1", "us-east-1", "eu-west-1", "key-123", 42)
	p.TransitionTo(StateFailoverComplete, "")

	if !p.IsComplete() {
		t.Error("expected IsComplete() to return true")
	}
	if p.IsFailed() {
		t.Error("expected IsFailed() to return false")
	}
	if p.CompletedAt == nil {
		t.Error("CompletedAt should be set")
	}
}

func TestPromotion_SetEpoch(t *testing.T) {
	p := NewPromotion("job-1", "us-east-1", "eu-west-1", "key-123", 42)

	p.SetEpoch(100)
	if p.GetEpoch() != 100 {
		t.Errorf("expected epoch 100, got %d", p.GetEpoch())
	}
}

// Mock implementations

type mockPromotionStore struct {
	promotions map[string]*Promotion
}

func newMockPromotionStore() *mockPromotionStore {
	return &mockPromotionStore{
		promotions: make(map[string]*Promotion),
	}
}

func (m *mockPromotionStore) Get(ctx context.Context, jobID, idempotencyKey string) (*Promotion, error) {
	key := jobID + ":" + idempotencyKey
	if p, ok := m.promotions[key]; ok {
		return p, nil
	}
	return nil, nil
}

func (m *mockPromotionStore) Create(ctx context.Context, promotion *Promotion) error {
	key := promotion.JobID + ":" + promotion.IdempotencyKey
	m.promotions[key] = promotion
	return nil
}

func (m *mockPromotionStore) Update(ctx context.Context, promotion *Promotion) error {
	key := promotion.JobID + ":" + promotion.IdempotencyKey
	m.promotions[key] = promotion
	return nil
}

func (m *mockPromotionStore) GetLatest(ctx context.Context, jobID string) (*Promotion, error) {
	for _, p := range m.promotions {
		if p.JobID == jobID {
			return p, nil
		}
	}
	return nil, nil
}

type mockEpochAssigner struct {
	epoch int64
}

func (m *mockEpochAssigner) Assign(ctx context.Context, jobID string) (int64, error) {
	m.epoch++
	return m.epoch, nil
}

type mockJobReader struct {
	epoch int64
}

func (m *mockJobReader) GetEpoch(ctx context.Context, jobID string) (int64, error) {
	return m.epoch, nil
}

type mockJobWriter struct {
	updatedEpoch int64
}

func (m *mockJobWriter) UpdateEpoch(ctx context.Context, jobID string, expectedEpoch int64, newEpoch int64) error {
	m.updatedEpoch = newEpoch
	return nil
}

type mockCapabilityRevalidator struct {
	err error
}

func (m *mockCapabilityRevalidator) Revalidate(ctx context.Context, jobID string, timeout time.Duration) error {
	return m.err
}

func TestManager_Promote(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	store := newMockPromotionStore()
	assigner := &mockEpochAssigner{epoch: 42}
	jobReader := &mockJobReader{epoch: 42}
	jobWriter := &mockJobWriter{}
	revalidator := &mockCapabilityRevalidator{}

	m, err := NewManager(logger, store, assigner, jobReader, jobWriter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m.revalidator = revalidator

	promotion, err := m.Promote(context.Background(), "job-1", "eu-west-1", "key-123", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if promotion.State != StateFailoverComplete {
		t.Errorf("expected StateFailoverComplete, got %v", promotion.State)
	}
	if promotion.NewEpoch != 43 {
		t.Errorf("expected new epoch 43, got %d", promotion.NewEpoch)
	}
}

func TestManager_Promote_Idempotent(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	store := newMockPromotionStore()
	assigner := &mockEpochAssigner{epoch: 42}
	jobReader := &mockJobReader{epoch: 42}
	jobWriter := &mockJobWriter{}
	revalidator := &mockCapabilityRevalidator{}

	m, err := NewManager(logger, store, assigner, jobReader, jobWriter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m.revalidator = revalidator

	// First promotion
	p1, err := m.Promote(context.Background(), "job-1", "eu-west-1", "key-123", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Duplicate promotion with same idempotency key
	p2, err := m.Promote(context.Background(), "job-1", "eu-west-1", "key-123", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p1 != p2 {
		t.Error("expected same promotion instance")
	}
}

func TestManager_Promote_VersionConflict(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	store := newMockPromotionStore()
	assigner := &mockEpochAssigner{epoch: 42}
	jobReader := &mockJobReader{epoch: 42}
	jobWriter := &mockJobWriter{}
	revalidator := &mockCapabilityRevalidator{}

	m, err := NewManager(logger, store, assigner, jobReader, jobWriter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m.revalidator = revalidator

	// Wrong version
	_, err = m.Promote(context.Background(), "job-1", "eu-west-1", "key-123", 99)
	if !errors.Is(err, ErrEpochConflict) {
		t.Errorf("expected ErrEpochConflict, got %v", err)
	}
}

func TestManager_Promote_CapabilityTimeout(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	store := newMockPromotionStore()
	assigner := &mockEpochAssigner{epoch: 42}
	jobReader := &mockJobReader{epoch: 42}
	jobWriter := &mockJobWriter{}
	revalidator := &mockCapabilityRevalidator{err: context.DeadlineExceeded}

	m, err := NewManager(logger, store, assigner, jobReader, jobWriter,
		WithCapabilityTimeout(100*time.Millisecond))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m.revalidator = revalidator

	_, err = m.Promote(context.Background(), "job-1", "eu-west-1", "key-123", 42)
	if err == nil {
		t.Error("expected error")
	}

	// Verify promotion failed
	promotion, _ := store.GetLatest(context.Background(), "job-1")
	if !promotion.IsFailed() {
		t.Error("expected promotion to be failed")
	}
}

func TestManager_GetStatus(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	store := newMockPromotionStore()
	assigner := &mockEpochAssigner{epoch: 42}
	jobReader := &mockJobReader{epoch: 42}
	jobWriter := &mockJobWriter{}
	revalidator := &mockCapabilityRevalidator{}

	m, err := NewManager(logger, store, assigner, jobReader, jobWriter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m.revalidator = revalidator

	// Create promotion
	_, err = m.Promote(context.Background(), "job-1", "eu-west-1", "key-123", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Get status with idempotency key
	status, err := m.GetStatus(context.Background(), "job-1", "key-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.JobID != "job-1" {
		t.Errorf("expected job-1, got %s", status.JobID)
	}

	// Get latest status without idempotency key
	latest, err := m.GetStatus(context.Background(), "job-1", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if latest.JobID != "job-1" {
		t.Errorf("expected job-1, got %s", latest.JobID)
	}
}

func TestManager_Stats(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	store := newMockPromotionStore()
	assigner := &mockEpochAssigner{epoch: 42}
	jobReader := &mockJobReader{epoch: 42}
	jobWriter := &mockJobWriter{}
	revalidator := &mockCapabilityRevalidator{}

	m, err := NewManager(logger, store, assigner, jobReader, jobWriter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m.revalidator = revalidator

	// Create promotions
	m.Promote(context.Background(), "job-1", "eu-west-1", "key-1", 42)
	m.Promote(context.Background(), "job-2", "eu-west-1", "key-2", 42)

	stats, err := m.Stats(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.TotalPromotions != 2 {
		t.Errorf("expected 2 total promotions, got %d", stats.TotalPromotions)
	}
	if stats.SuccessfulPromotions != 2 {
		t.Errorf("expected 2 successful promotions, got %d", stats.SuccessfulPromotions)
	}
}
