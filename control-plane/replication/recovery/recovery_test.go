package recovery

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestRecoveryState_String(t *testing.T) {
	tests := []struct {
		state RecoveryState
		want  string
	}{
		{StateRecoveryPending, "recovery_pending"},
		{StateCheckpointLocated, "checkpoint_located"},
		{StateCheckpointValidated, "checkpoint_validated"},
		{StateFilesDownloading, "files_downloading"},
		{StateFilesDownloaded, "files_downloaded"},
		{StateRestoring, "restoring"},
		{StateRecoveryComplete, "recovery_complete"},
		{StateRecoveryFailed, "recovery_failed"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("RecoveryState(%d).String() = %s, want %s", tt.state, got, tt.want)
		}
	}
}

func TestNewRecovery(t *testing.T) {
	r := NewRecovery("job-1", 42)

	if r.JobID != "job-1" {
		t.Errorf("expected job-1, got %s", r.JobID)
	}
	if r.NewEpoch != 42 {
		t.Errorf("expected epoch 42, got %d", r.NewEpoch)
	}
	if r.State != StateRecoveryPending {
		t.Errorf("expected StateRecoveryPending, got %v", r.State)
	}
}

func TestRecovery_TransitionTo(t *testing.T) {
	r := NewRecovery("job-1", 42)

	// Valid transition
	if err := r.TransitionTo(StateCheckpointLocated, ""); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if r.State != StateCheckpointLocated {
		t.Errorf("expected StateCheckpointLocated, got %v", r.State)
	}

	// Transition to same state
	if err := r.TransitionTo(StateCheckpointLocated, ""); err == nil {
		t.Error("expected error for same state transition")
	}

	// Transition to earlier state
	if err := r.TransitionTo(StateRecoveryPending, ""); err == nil {
		t.Error("expected error for earlier state transition")
	}
}

func TestRecovery_IsComplete(t *testing.T) {
	r := NewRecovery("job-1", 42)
	r.TransitionTo(StateRecoveryComplete, "")

	if !r.IsComplete() {
		t.Error("expected IsComplete() to return true")
	}
}

func TestRecovery_IsFailed(t *testing.T) {
	r := NewRecovery("job-1", 42)
	r.TransitionTo(StateRecoveryFailed, "test error")

	if !r.IsFailed() {
		t.Error("expected IsFailed() to return true")
	}
}

// Mock implementations

type mockWALEntryReader struct {
	checkpoint *CheckpointManifest
	err        error
}

func (m *mockWALEntryReader) ReadEntries(ctx context.Context, sinceSequence int64) ([]*WALEntry, error) {
	return nil, nil
}

func (m *mockWALEntryReader) GetLatestCheckpoint(ctx context.Context) (*CheckpointManifest, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.checkpoint, nil
}

type mockObjectStorage struct {
	data map[string][]byte
}

func (m *mockObjectStorage) GetObject(ctx context.Context, uri string) ([]byte, error) {
	if m.data == nil {
		return nil, errors.New("not found")
	}
	data, ok := m.data[uri]
	if !ok {
		return nil, errors.New("not found")
	}
	return data, nil
}

func (m *mockObjectStorage) GetObjectReader(ctx context.Context, uri string) (io.ReadCloser, error) {
	// Return a dummy reader - actual data is not needed for this mock
	return io.NopCloser(io.Reader(nil)), nil
}

type mockManifestParser struct {
	manifest *CheckpointManifest
	err     error
}

func (m *mockManifestParser) Parse(data []byte) (*CheckpointManifest, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.manifest, nil
}

type mockValidator struct {
	err error
}

func (m *mockValidator) Validate(ctx context.Context, manifest *CheckpointManifest) error {
	return m.err
}

type mockStateRestorer struct {
	err error
}

func (m *mockStateRestorer) Restore(ctx context.Context, manifest *CheckpointManifest) error {
	return m.err
}

type mockAuditLogger struct {
	events []string
}

func (m *mockAuditLogger) LogRecoveryStarted(ctx context.Context, jobID string, epoch int64) {
	m.events = append(m.events, "started:"+jobID)
}

func (m *mockAuditLogger) LogCheckpointLocated(ctx context.Context, jobID string, uri string) {
	m.events = append(m.events, "located:"+jobID)
}

func (m *mockAuditLogger) LogCheckpointValidated(ctx context.Context, jobID string) {
	m.events = append(m.events, "validated:"+jobID)
}

func (m *mockAuditLogger) LogRecoveryComplete(ctx context.Context, jobID string, duration time.Duration) {
	m.events = append(m.events, "complete:"+jobID)
}

func (m *mockAuditLogger) LogRecoveryFailed(ctx context.Context, jobID string, reason string) {
	m.events = append(m.events, "failed:"+jobID+":"+reason)
}

func TestManager_Recover(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	manifest := &CheckpointManifest{
		JobID:         "job-1",
		Epoch:         42,
		Sequence:      100,
		CheckpointURI: "s3://bucket/checkpoint",
		Files:         []CheckpointFile{},
	}

	manager := NewManager(
		logger,
		&mockWALEntryReader{checkpoint: manifest},
		&mockObjectStorage{data: map[string][]byte{}},
		&mockManifestParser{manifest: manifest},
		&mockValidator{},
		&mockStateRestorer{},
		&mockAuditLogger{},
	)

	recovery, err := manager.Recover(context.Background(), "job-1", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if recovery.State != StateRecoveryComplete {
		t.Errorf("expected StateRecoveryComplete, got %v", recovery.State)
	}
}

func TestManager_Recover_CheckpointNotFound(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	manager := NewManager(
		logger,
		&mockWALEntryReader{err: ErrCheckpointNotFound},
		&mockObjectStorage{},
		&mockManifestParser{},
		&mockValidator{},
		&mockStateRestorer{},
		&mockAuditLogger{},
	)

	recovery, err := manager.Recover(context.Background(), "job-1", 42)
	if err == nil {
		t.Error("expected error")
	}

	if !recovery.IsFailed() {
		t.Error("expected recovery to be failed")
	}
}

func TestManager_Recover_EpochMismatch(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	manifest := &CheckpointManifest{
		JobID:         "job-1",
		Epoch:         41, // Different from requested epoch
		Sequence:      100,
		CheckpointURI: "s3://bucket/checkpoint",
		Files:         []CheckpointFile{},
	}

	manager := NewManager(
		logger,
		&mockWALEntryReader{checkpoint: manifest},
		&mockObjectStorage{},
		&mockManifestParser{},
		&mockValidator{},
		&mockStateRestorer{},
		&mockAuditLogger{},
	)

	recovery, err := manager.Recover(context.Background(), "job-1", 42)
	if err == nil {
		t.Error("expected error")
	}

	if !recovery.IsFailed() {
		t.Error("expected recovery to be failed")
	}
}

func TestManager_Recover_ValidationFailed(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	manifest := &CheckpointManifest{
		JobID:         "job-1",
		Epoch:         42,
		Sequence:      100,
		CheckpointURI: "s3://bucket/checkpoint",
		Files:         []CheckpointFile{},
	}

	manager := NewManager(
		logger,
		&mockWALEntryReader{checkpoint: manifest},
		&mockObjectStorage{},
		&mockManifestParser{},
		&mockValidator{err: ErrCheckpointCorrupted},
		&mockStateRestorer{},
		&mockAuditLogger{},
	)

	recovery, err := manager.Recover(context.Background(), "job-1", 42)
	if err == nil {
		t.Error("expected error")
	}

	if !recovery.IsFailed() {
		t.Error("expected recovery to be failed")
	}
}

func TestManager_Recover_AuditEvents(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	manifest := &CheckpointManifest{
		JobID:         "job-1",
		Epoch:         42,
		Sequence:      100,
		CheckpointURI: "s3://bucket/checkpoint",
		Files:         []CheckpointFile{},
	}

	auditor := &mockAuditLogger{}

	manager := NewManager(
		logger,
		&mockWALEntryReader{checkpoint: manifest},
		&mockObjectStorage{data: map[string][]byte{}},
		&mockManifestParser{manifest: manifest},
		&mockValidator{},
		&mockStateRestorer{},
		auditor,
	)

	_, err := manager.Recover(context.Background(), "job-1", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedEvents := []string{
		"started:job-1",
		"located:job-1",
		"validated:job-1",
		"complete:job-1",
	}

	if len(auditor.events) != len(expectedEvents) {
		t.Errorf("expected %d events, got %d: %v", len(expectedEvents), len(auditor.events), auditor.events)
	}
}

func TestManager_Recover_FailedAuditEvents(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	auditor := &mockAuditLogger{}

	manager := NewManager(
		logger,
		&mockWALEntryReader{err: ErrCheckpointNotFound},
		&mockObjectStorage{},
		&mockManifestParser{},
		&mockValidator{},
		&mockStateRestorer{},
		auditor,
	)

	_, err := manager.Recover(context.Background(), "job-1", 42)
	if err == nil {
		t.Error("expected error")
	}

	foundFailed := false
	for _, e := range auditor.events {
		if len(e) > 7 && e[:7] == "failed:" {
			foundFailed = true
			break
		}
	}

	if !foundFailed {
		t.Error("expected failed audit event")
	}
}

func TestNewManager_Defaults(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	manager := NewManager(
		logger,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	if manager.cfg.DownloadTimeout != 10*time.Minute {
		t.Errorf("expected default download timeout 10m, got %v", manager.cfg.DownloadTimeout)
	}
	if manager.cfg.ValidationTimeout != 30*time.Second {
		t.Errorf("expected default validation timeout 30s, got %v", manager.cfg.ValidationTimeout)
	}
	if manager.cfg.RestoreTimeout != 5*time.Minute {
		t.Errorf("expected default restore timeout 5m, got %v", manager.cfg.RestoreTimeout)
	}
}

func TestWithDownloadTimeout(t *testing.T) {
	cfg := Config{}
	WithDownloadTimeout(30 * time.Minute)(&cfg)

	if cfg.DownloadTimeout != 30*time.Minute {
		t.Errorf("expected download timeout 30m, got %v", cfg.DownloadTimeout)
	}
}

func TestWithValidationTimeout(t *testing.T) {
	cfg := Config{}
	WithValidationTimeout(60 * time.Second)(&cfg)

	if cfg.ValidationTimeout != 60*time.Second {
		t.Errorf("expected validation timeout 60s, got %v", cfg.ValidationTimeout)
	}
}

func TestWithRestoreTimeout(t *testing.T) {
	cfg := Config{}
	WithRestoreTimeout(10 * time.Minute)(&cfg)

	if cfg.RestoreTimeout != 10*time.Minute {
		t.Errorf("expected restore timeout 10m, got %v", cfg.RestoreTimeout)
	}
}
