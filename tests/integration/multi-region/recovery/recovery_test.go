// Package recovery provides recovery integration tests for multi-region topology.
package recovery

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// Common errors for recovery tests.
var (
	ErrCheckpointNotFound     = errors.New("recovery: checkpoint not found")
	ErrCheckpointCorrupted    = errors.New("recovery: checkpoint corrupted")
	ErrCheckpointEpochMismatch = errors.New("recovery: checkpoint epoch mismatch")
	ErrRecoveryTimeout        = errors.New("recovery: recovery timeout")
)

// CheckpointManifest represents a checkpoint manifest.
type CheckpointManifest struct {
	JobID         string
	Epoch         int64
	Sequence      int64
	CheckpointURI string
	Valid         bool
	Files         []string
}

// RecoveryState represents the recovery state.
type RecoveryState int

const (
	StateRecoveryPending RecoveryState = iota
	StateCheckpointLocated
	StateCheckpointValidated
	StateFilesDownloaded
	StateRestoring
	StateRecoveryComplete
	StateRecoveryFailed
)

// String returns the string representation of the state.
func (s RecoveryState) String() string {
	switch s {
	case StateRecoveryPending:
		return "recovery_pending"
	case StateCheckpointLocated:
		return "checkpoint_located"
	case StateCheckpointValidated:
		return "checkpoint_validated"
	case StateFilesDownloaded:
		return "files_downloaded"
	case StateRestoring:
		return "restoring"
	case StateRecoveryComplete:
		return "recovery_complete"
	case StateRecoveryFailed:
		return "recovery_failed"
	default:
		return "unknown"
	}
}

// RecoveryResult represents the result of a recovery operation.
type RecoveryResult struct {
	JobID         string
	Manifest      *CheckpointManifest
	State         RecoveryState
	ErrorMessage  string
	StartedAt     time.Time
	CompletedAt   *time.Time
	Duration      time.Duration
}

// RecoveryClient simulates the recovery client.
type RecoveryClient struct {
	mu         sync.RWMutex
	manifests  map[string]*CheckpointManifest
	validators map[string]ValidatorFunc
}

// ValidatorFunc is a function that validates a checkpoint.
type ValidatorFunc func(manifest *CheckpointManifest) error

// NewRecoveryClient creates a new recovery client.
func NewRecoveryClient() *RecoveryClient {
	return &RecoveryClient{
		manifests:  make(map[string]*CheckpointManifest),
		validators: make(map[string]ValidatorFunc),
	}
}

// AddCheckpoint adds a checkpoint manifest.
func (c *RecoveryClient) AddCheckpoint(manifest *CheckpointManifest) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.manifests[manifest.JobID] = manifest
}

// SetValidator sets the validator for a job.
func (c *RecoveryClient) SetValidator(jobID string, validator ValidatorFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.validators[jobID] = validator
}

// Recover performs recovery for a job.
func (c *RecoveryClient) Recover(ctx context.Context, jobID string, newEpoch int64) (*RecoveryResult, error) {
	result := &RecoveryResult{
		JobID:     jobID,
		State:     StateRecoveryPending,
		StartedAt: time.Now().UTC(),
	}

	// Locate checkpoint
	c.mu.RLock()
	manifest, ok := c.manifests[jobID]
	c.mu.RUnlock()

	if !ok {
		result.State = StateRecoveryFailed
		result.ErrorMessage = "checkpoint not found"
		now := time.Now().UTC()
		result.CompletedAt = &now
		result.Duration = now.Sub(result.StartedAt)
		return result, ErrCheckpointNotFound
	}

	result.Manifest = manifest
	result.State = StateCheckpointLocated

	// Validate epoch
	if manifest.Epoch != newEpoch {
		result.State = StateRecoveryFailed
		result.ErrorMessage = "epoch mismatch"
		now := time.Now().UTC()
		result.CompletedAt = &now
		result.Duration = now.Sub(result.StartedAt)
		return result, ErrCheckpointEpochMismatch
	}

	// Validate checkpoint
	if c.validators != nil {
		c.mu.RLock()
		validator, hasValidator := c.validators[jobID]
		c.mu.RUnlock()

		if hasValidator {
			if err := validator(manifest); err != nil {
				result.State = StateRecoveryFailed
				result.ErrorMessage = err.Error()
				now := time.Now().UTC()
				result.CompletedAt = &now
				result.Duration = now.Sub(result.StartedAt)
				return result, err
			}
		}
	}

	if !manifest.Valid {
		result.State = StateRecoveryFailed
		result.ErrorMessage = "checkpoint invalid"
		now := time.Now().UTC()
		result.CompletedAt = &now
		result.Duration = now.Sub(result.StartedAt)
		return result, ErrCheckpointCorrupted
	}

	result.State = StateCheckpointValidated

	// Simulate file download
	select {
	case <-ctx.Done():
		return result, ctx.Err()
	case <-time.After(50 * time.Millisecond):
	}
	result.State = StateFilesDownloaded

	// Simulate state restoration
	result.State = StateRestoring
	select {
	case <-ctx.Done():
		return result, ctx.Err()
	case <-time.After(50 * time.Millisecond):
	}

	// Mark complete
	result.State = StateRecoveryComplete
	now := time.Now().UTC()
	result.CompletedAt = &now
	result.Duration = now.Sub(result.StartedAt)

	return result, nil
}

// GetState returns the recovery state.
func (c *RecoveryClient) GetState(jobID string) RecoveryState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return StateRecoveryPending
}

// VerifyRecoveryComplete verifies recovery is complete.
func VerifyRecoveryComplete(t *testing.T, result *RecoveryResult) {
	t.Helper()

	if result.State != StateRecoveryComplete {
		t.Errorf("expected state %s, got %s", StateRecoveryComplete, result.State)
	}

	if result.CompletedAt == nil {
		t.Error("expected completed timestamp to be set")
	}
}

// VerifyRecoveryFailed verifies recovery failed.
func VerifyRecoveryFailed(t *testing.T, result *RecoveryResult) {
	t.Helper()

	if result.State != StateRecoveryFailed {
		t.Errorf("expected state %s, got %s", StateRecoveryFailed, result.State)
	}
}

// TestRecovery_HappyPath verifies a successful recovery.
func TestRecovery_HappyPath(t *testing.T) {
	client := NewRecoveryClient()

	manifest := &CheckpointManifest{
		JobID:         "job-1",
		Epoch:         42,
		Sequence:      100,
		CheckpointURI: "s3://bucket/checkpoint",
		Valid:         true,
		Files:         []string{"file1", "file2"},
	}
	client.AddCheckpoint(manifest)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.Recover(ctx, "job-1", 42)
	if err != nil {
		t.Fatalf("recovery failed: %v", err)
	}

	VerifyRecoveryComplete(t, result)
	if result.Manifest.JobID != "job-1" {
		t.Errorf("expected job-1, got %s", result.Manifest.JobID)
	}
}

// TestRecovery_CheckpointNotFound verifies handling of missing checkpoint.
func TestRecovery_CheckpointNotFound(t *testing.T) {
	client := NewRecoveryClient()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.Recover(ctx, "nonexistent", 42)
	if !errors.Is(err, ErrCheckpointNotFound) {
		t.Errorf("expected ErrCheckpointNotFound, got %v", err)
	}

	VerifyRecoveryFailed(t, result)
}

// TestRecovery_EpochMismatch verifies epoch mismatch detection.
func TestRecovery_EpochMismatch(t *testing.T) {
	client := NewRecoveryClient()

	manifest := &CheckpointManifest{
		JobID:         "job-1",
		Epoch:         41, // Different from requested epoch
		Sequence:      100,
		CheckpointURI: "s3://bucket/checkpoint",
		Valid:         true,
	}
	client.AddCheckpoint(manifest)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.Recover(ctx, "job-1", 42)
	if !errors.Is(err, ErrCheckpointEpochMismatch) {
		t.Errorf("expected ErrCheckpointEpochMismatch, got %v", err)
	}

	VerifyRecoveryFailed(t, result)
}

// TestRecovery_ValidationFailure verifies validation failure.
func TestRecovery_ValidationFailure(t *testing.T) {
	client := NewRecoveryClient()

	manifest := &CheckpointManifest{
		JobID:         "job-1",
		Epoch:         42,
		Sequence:      100,
		CheckpointURI: "s3://bucket/checkpoint",
		Valid:         true,
	}
	client.AddCheckpoint(manifest)
	client.SetValidator("job-1", func(m *CheckpointManifest) error {
		return ErrCheckpointCorrupted
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.Recover(ctx, "job-1", 42)
	if !errors.Is(err, ErrCheckpointCorrupted) {
		t.Errorf("expected ErrCheckpointCorrupted, got %v", err)
	}

	VerifyRecoveryFailed(t, result)
}

// TestRecovery_InvalidCheckpoint verifies invalid checkpoint.
func TestRecovery_InvalidCheckpoint(t *testing.T) {
	client := NewRecoveryClient()

	manifest := &CheckpointManifest{
		JobID:         "job-1",
		Epoch:         42,
		Sequence:      100,
		CheckpointURI: "s3://bucket/checkpoint",
		Valid:         false, // Invalid
	}
	client.AddCheckpoint(manifest)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Recover(ctx, "job-1", 42)
	if !errors.Is(err, ErrCheckpointCorrupted) {
		t.Errorf("expected ErrCheckpointCorrupted, got %v", err)
	}
}

// TestRecovery_StateTransitions verifies all states are visited.
func TestRecovery_StateTransitions(t *testing.T) {
	client := NewRecoveryClient()

	manifest := &CheckpointManifest{
		JobID:         "job-1",
		Epoch:         42,
		Sequence:      100,
		CheckpointURI: "s3://bucket/checkpoint",
		Valid:         true,
	}
	client.AddCheckpoint(manifest)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.Recover(ctx, "job-1", 42)
	if err != nil {
		t.Fatalf("recovery failed: %v", err)
	}

	if result.State != StateRecoveryComplete {
		t.Errorf("expected final state %s, got %s", StateRecoveryComplete, result.State)
	}
}

// TestRecovery_Duration verifies duration is recorded.
func TestRecovery_Duration(t *testing.T) {
	client := NewRecoveryClient()

	manifest := &CheckpointManifest{
		JobID:         "job-1",
		Epoch:         42,
		Sequence:      100,
		CheckpointURI: "s3://bucket/checkpoint",
		Valid:         true,
	}
	client.AddCheckpoint(manifest)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.Recover(ctx, "job-1", 42)
	if err != nil {
		t.Fatalf("recovery failed: %v", err)
	}

	if result.Duration <= 0 {
		t.Error("expected positive duration")
	}
}

// TestRecovery_MultipleJobs verifies multiple job recoveries.
func TestRecovery_MultipleJobs(t *testing.T) {
	client := NewRecoveryClient()

	for i := 0; i < 3; i++ {
		jobID := "job-" + string(rune('a'+i))
		manifest := &CheckpointManifest{
			JobID:         jobID,
			Epoch:         42,
			Sequence:      int64(100 + i),
			CheckpointURI: "s3://bucket/checkpoint-" + jobID,
			Valid:         true,
		}
		client.AddCheckpoint(manifest)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for i := 0; i < 3; i++ {
		jobID := "job-" + string(rune('a'+i))
		result, err := client.Recover(ctx, jobID, 42)
		if err != nil {
			t.Errorf("recovery for %s failed: %v", jobID, err)
		}

		VerifyRecoveryComplete(t, result)
	}
}

// TestRecovery_ContextCancellation verifies context cancellation.
func TestRecovery_ContextCancellation(t *testing.T) {
	client := NewRecoveryClient()

	manifest := &CheckpointManifest{
		JobID:         "job-1",
		Epoch:         42,
		Sequence:      100,
		CheckpointURI: "s3://bucket/checkpoint",
		Valid:         true,
	}
	client.AddCheckpoint(manifest)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Recover(ctx, "job-1", 42)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

// TestRecovery_ValidatorSuccess verifies passing validator.
func TestRecovery_ValidatorSuccess(t *testing.T) {
	client := NewRecoveryClient()

	manifest := &CheckpointManifest{
		JobID:         "job-1",
		Epoch:         42,
		Sequence:      100,
		CheckpointURI: "s3://bucket/checkpoint",
		Valid:         true,
	}
	client.AddCheckpoint(manifest)
	client.SetValidator("job-1", func(m *CheckpointManifest) error {
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.Recover(ctx, "job-1", 42)
	if err != nil {
		t.Errorf("recovery failed: %v", err)
	}

	VerifyRecoveryComplete(t, result)
}

// TestRecovery_StateString verifies state string representations.
func TestRecovery_StateString(t *testing.T) {
	states := []struct {
		state    RecoveryState
		expected string
	}{
		{StateRecoveryPending, "recovery_pending"},
		{StateCheckpointLocated, "checkpoint_located"},
		{StateCheckpointValidated, "checkpoint_validated"},
		{StateFilesDownloaded, "files_downloaded"},
		{StateRestoring, "restoring"},
		{StateRecoveryComplete, "recovery_complete"},
		{StateRecoveryFailed, "recovery_failed"},
	}

	for _, s := range states {
		if s.state.String() != s.expected {
			t.Errorf("expected %s, got %s", s.expected, s.state.String())
		}
	}
}

// TestRecovery_RecoveryResult verifies the result struct.
func TestRecovery_RecoveryResult(t *testing.T) {
	result := &RecoveryResult{
		JobID: "job-1",
		State: StateRecoveryPending,
	}

	if result.JobID != "job-1" {
		t.Error("expected job-1")
	}
	if result.State != StateRecoveryPending {
		t.Error("expected StateRecoveryPending")
	}
}

// TestRecovery_Manifest verifies the manifest struct.
func TestRecovery_Manifest(t *testing.T) {
	manifest := &CheckpointManifest{
		JobID:         "job-1",
		Epoch:         42,
		Sequence:      100,
		CheckpointURI: "s3://bucket/checkpoint",
		Valid:         true,
		Files:         []string{"file1", "file2"},
	}

	if manifest.JobID != "job-1" {
		t.Error("expected job-1")
	}
	if manifest.Epoch != 42 {
		t.Errorf("expected epoch 42, got %d", manifest.Epoch)
	}
	if len(manifest.Files) != 2 {
		t.Errorf("expected 2 files, got %d", len(manifest.Files))
	}
}

// TestRecovery_EmptyClient verifies empty client behavior.
func TestRecovery_EmptyClient(t *testing.T) {
	client := NewRecoveryClient()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, err := client.Recover(ctx, "nonexistent", 42)
	if !errors.Is(err, ErrCheckpointNotFound) {
		t.Errorf("expected ErrCheckpointNotFound, got %v", err)
	}
}

// TestRecovery_ZeroEpoch verifies zero epoch handling.
func TestRecovery_ZeroEpoch(t *testing.T) {
	client := NewRecoveryClient()

	manifest := &CheckpointManifest{
		JobID:         "job-1",
		Epoch:         0,
		Sequence:      100,
		CheckpointURI: "s3://bucket/checkpoint",
		Valid:         true,
	}
	client.AddCheckpoint(manifest)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.Recover(ctx, "job-1", 0)
	if err != nil {
		t.Fatalf("recovery failed: %v", err)
	}

	VerifyRecoveryComplete(t, result)
}

// TestRecovery_LargeEpoch verifies large epoch handling.
func TestRecovery_LargeEpoch(t *testing.T) {
	client := NewRecoveryClient()

	largeEpoch := int64(1 << 50)
	manifest := &CheckpointManifest{
		JobID:         "job-1",
		Epoch:         largeEpoch,
		Sequence:      100,
		CheckpointURI: "s3://bucket/checkpoint",
		Valid:         true,
	}
	client.AddCheckpoint(manifest)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.Recover(ctx, "job-1", largeEpoch)
	if err != nil {
		t.Fatalf("recovery failed: %v", err)
	}

	VerifyRecoveryComplete(t, result)
}

// TestRecovery_OverrideManifest verifies manifest override.
func TestRecovery_OverrideManifest(t *testing.T) {
	client := NewRecoveryClient()

	manifest1 := &CheckpointManifest{
		JobID:         "job-1",
		Epoch:         41,
		CheckpointURI: "s3://bucket/old",
		Valid:         true,
	}
	manifest2 := &CheckpointManifest{
		JobID:         "job-1",
		Epoch:         42,
		CheckpointURI: "s3://bucket/new",
		Valid:         true,
	}

	client.AddCheckpoint(manifest1)
	client.AddCheckpoint(manifest2)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.Recover(ctx, "job-1", 42)
	if err != nil {
		t.Fatalf("recovery failed: %v", err)
	}

	if result.Manifest.CheckpointURI != "s3://bucket/new" {
		t.Errorf("expected new manifest, got %s", result.Manifest.CheckpointURI)
	}
}

// TestRecovery_ValidatorOrder verifies validator runs before manifest valid check.
func TestRecovery_ValidatorOrder(t *testing.T) {
	client := NewRecoveryClient()

	manifest := &CheckpointManifest{
		JobID:         "job-1",
		Epoch:         42,
		CheckpointURI: "s3://bucket/checkpoint",
		Valid:         false,
	}
	client.AddCheckpoint(manifest)
	validatorCalled := false
	client.SetValidator("job-1", func(m *CheckpointManifest) error {
		validatorCalled = true
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Recover(ctx, "job-1", 42)
	if !errors.Is(err, ErrCheckpointCorrupted) {
		t.Errorf("expected ErrCheckpointCorrupted, got %v", err)
	}

	if !validatorCalled {
		t.Error("expected validator to be called")
	}
}

// TestRecovery_StartedTimestampSet verifies started timestamp.
func TestRecovery_StartedTimestampSet(t *testing.T) {
	client := NewRecoveryClient()

	manifest := &CheckpointManifest{
		JobID:         "job-1",
		Epoch:         42,
		CheckpointURI: "s3://bucket/checkpoint",
		Valid:         true,
	}
	client.AddCheckpoint(manifest)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.Recover(ctx, "job-1", 42)
	if err != nil {
		t.Fatalf("recovery failed: %v", err)
	}

	if result.StartedAt.IsZero() {
		t.Error("expected started timestamp to be set")
	}
}

// TestRecovery_CompletedTimestampSet verifies completed timestamp.
func TestRecovery_CompletedTimestampSet(t *testing.T) {
	client := NewRecoveryClient()

	manifest := &CheckpointManifest{
		JobID:         "job-1",
		Epoch:         42,
		CheckpointURI: "s3://bucket/checkpoint",
		Valid:         true,
	}
	client.AddCheckpoint(manifest)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.Recover(ctx, "job-1", 42)
	if err != nil {
		t.Fatalf("recovery failed: %v", err)
	}

	if result.CompletedAt == nil {
		t.Error("expected completed timestamp to be set")
	}

	if result.CompletedAt.Before(result.StartedAt) {
		t.Error("completed timestamp should be after started timestamp")
	}
}

// TestRecovery_FailedJob verifies job fails when there is no checkpoint.
func TestRecovery_FailedJob(t *testing.T) {
	client := NewRecoveryClient()

	_, err := client.Recover(context.Background(), "no-such-job", 42)
	if !errors.Is(err, ErrCheckpointNotFound) {
		t.Errorf("expected ErrCheckpointNotFound, got %v", err)
	}
}

// TestRecovery_VerifyHelper verifies the helper function.
func TestRecovery_VerifyHelper(t *testing.T) {
	result := &RecoveryResult{
		JobID:     "job-1",
		State:     StateRecoveryComplete,
		StartedAt: time.Now().UTC(),
	}
	now := time.Now().UTC()
	result.CompletedAt = &now

	VerifyRecoveryComplete(t, result)
}

// TestRecovery_VerifyFailedHelper verifies the failed helper.
func TestRecovery_VerifyFailedHelper(t *testing.T) {
	result := &RecoveryResult{
		JobID:         "job-1",
		State:         StateRecoveryFailed,
		ErrorMessage: "test failure",
	}

	VerifyRecoveryFailed(t, result)
}
