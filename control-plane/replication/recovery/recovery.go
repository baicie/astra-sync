// Package recovery provides checkpoint-coupled recovery for cross-region failover.
package recovery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Common errors for recovery operations.
var (
	ErrCheckpointNotFound     = errors.New("recovery: checkpoint not found")
	ErrCheckpointNotReplicated = errors.New("recovery: checkpoint not replicated to secondary")
	ErrCheckpointCorrupted    = errors.New("recovery: checkpoint data corrupted")
	ErrCheckpointEpochMismatch = errors.New("recovery: checkpoint epoch mismatch")
	ErrCheckpointSeqMismatch  = errors.New("recovery: checkpoint sequence mismatch")
	ErrRecoveryAborted        = errors.New("recovery: recovery aborted")
)

// CheckpointManifest represents a checkpoint manifest.
type CheckpointManifest struct {
	JobID          string
	Epoch          int64
	Sequence       int64
	CheckpointURI  string
	Files          []CheckpointFile
	CreatedAt      time.Time
	CRC32C         uint32
}

// CheckpointFile represents a file within a checkpoint.
type CheckpointFile struct {
	Name         string
	URI          string
	Size         int64
	CRC32C       uint32
}

// RecoveryState represents the state of a recovery operation.
type RecoveryState int

const (
	StateRecoveryPending RecoveryState = iota
	StateCheckpointLocated
	StateCheckpointValidated
	StateFilesDownloading
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
	case StateFilesDownloading:
		return "files_downloading"
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

// Recovery represents a recovery operation.
type Recovery struct {
	JobID         string
	NewEpoch      int64
	Checkpoint    *CheckpointManifest
	State         RecoveryState
	ErrorMessage  string
	StartedAt     time.Time
	CompletedAt   *time.Time
	mu            sync.RWMutex
}

// NewRecovery creates a new recovery record.
func NewRecovery(jobID string, newEpoch int64) *Recovery {
	return &Recovery{
		JobID:     jobID,
		NewEpoch:  newEpoch,
		State:     StateRecoveryPending,
		StartedAt: time.Now().UTC(),
	}
}

// TransitionTo transitions the recovery to a new state.
func (r *Recovery) TransitionTo(state RecoveryState, errMsg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State == StateRecoveryFailed {
		return fmt.Errorf("cannot transition from failed state")
	}

	if state <= r.State {
		return fmt.Errorf("cannot transition to earlier or same state: %s -> %s", r.State, state)
	}

	r.State = state
	if errMsg != "" {
		r.ErrorMessage = errMsg
	}
	if state == StateRecoveryComplete || state == StateRecoveryFailed {
		now := time.Now().UTC()
		r.CompletedAt = &now
	}

	return nil
}

// IsComplete returns true if the recovery is complete.
func (r *Recovery) IsComplete() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.State == StateRecoveryComplete || r.State == StateRecoveryFailed
}

// IsFailed returns true if the recovery failed.
func (r *Recovery) IsFailed() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.State == StateRecoveryFailed
}

// WALEntryReader reads WAL entries to find the latest replicated checkpoint.
type WALEntryReader interface {
	// ReadEntries reads WAL entries since the given sequence.
	ReadEntries(ctx context.Context, sinceSequence int64) ([]*WALEntry, error)
	// GetLatestCheckpoint returns the latest replicated checkpoint.
	GetLatestCheckpoint(ctx context.Context) (*CheckpointManifest, error)
}

// WALEntry represents a WAL entry for checkpoint replication.
type WALEntry struct {
	Sequence      int64
	Region       string
	Epoch        int64
	CheckpointURI string
	JobID        string
	Timestamp    time.Time
	CRC32C       uint32
}

// ObjectStorage provides access to object storage.
type ObjectStorage interface {
	// GetObject downloads an object.
	GetObject(ctx context.Context, uri string) ([]byte, error)
	// GetObjectReader returns a reader for an object.
	GetObjectReader(ctx context.Context, uri string) (io.ReadCloser, error)
}

// ManifestParser parses checkpoint manifests.
type ManifestParser interface {
	// Parse parses a checkpoint manifest.
	Parse(data []byte) (*CheckpointManifest, error)
}

// Validator validates checkpoints.
type Validator interface {
	// Validate validates a checkpoint.
	Validate(ctx context.Context, manifest *CheckpointManifest) error
}

// StateRestorer restores job state from checkpoint files.
type StateRestorer interface {
	// Restore restores job state from checkpoint files.
	Restore(ctx context.Context, manifest *CheckpointManifest) error
}

// AuditLogger logs recovery audit events.
type AuditLogger interface {
	// LogRecoveryStarted logs the start of a recovery.
	LogRecoveryStarted(ctx context.Context, jobID string, epoch int64)
	// LogCheckpointLocated logs when a checkpoint is located.
	LogCheckpointLocated(ctx context.Context, jobID string, uri string)
	// LogCheckpointValidated logs when a checkpoint is validated.
	LogCheckpointValidated(ctx context.Context, jobID string)
	// LogRecoveryComplete logs when recovery completes.
	LogRecoveryComplete(ctx context.Context, jobID string, duration time.Duration)
	// LogRecoveryFailed logs when recovery fails.
	LogRecoveryFailed(ctx context.Context, jobID string, reason string)
}

// Manager manages checkpoint-coupled recovery.
type Manager struct {
	cfg       Config
	logger    *zap.Logger
	walReader WALEntryReader
	storage   ObjectStorage
	parser    ManifestParser
	validator Validator
	restorer  StateRestorer
	auditor   AuditLogger
}

// Config holds the configuration for the recovery manager.
type Config struct {
	// Timeout for downloading checkpoint files.
	DownloadTimeout time.Duration
	// Timeout for validating checkpoint.
	ValidationTimeout time.Duration
	// Timeout for restoring state.
	RestoreTimeout time.Duration
}

// Option is a functional option for manager configuration.
type Option func(*Config)

// WithDownloadTimeout sets the download timeout.
func WithDownloadTimeout(d time.Duration) Option {
	return func(c *Config) {
		c.DownloadTimeout = d
	}
}

// WithValidationTimeout sets the validation timeout.
func WithValidationTimeout(d time.Duration) Option {
	return func(c *Config) {
		c.ValidationTimeout = d
	}
}

// WithRestoreTimeout sets the restore timeout.
func WithRestoreTimeout(d time.Duration) Option {
	return func(c *Config) {
		c.RestoreTimeout = d
	}
}

// NewManager creates a new recovery manager.
func NewManager(
	logger *zap.Logger,
	walReader WALEntryReader,
	storage ObjectStorage,
	parser ManifestParser,
	validator Validator,
	restorer StateRestorer,
	auditor AuditLogger,
	opts ...Option,
) *Manager {
	cfg := Config{
		DownloadTimeout:   10 * time.Minute,
		ValidationTimeout: 30 * time.Second,
		RestoreTimeout:    5 * time.Minute,
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	return &Manager{
		cfg:       cfg,
		logger:    logger.With(zap.String("component", "recovery")),
		walReader: walReader,
		storage:   storage,
		parser:    parser,
		validator: validator,
		restorer:  restorer,
		auditor:   auditor,
	}
}

// Recover performs checkpoint-coupled recovery for a job.
func (m *Manager) Recover(ctx context.Context, jobID string, newEpoch int64) (*Recovery, error) {
	recovery := NewRecovery(jobID, newEpoch)

	// Log start
	if m.auditor != nil {
		m.auditor.LogRecoveryStarted(ctx, jobID, newEpoch)
	}

	m.logger.Info("starting checkpoint-coupled recovery",
		zap.String("jobID", jobID),
		zap.Int64("newEpoch", newEpoch))

	// Step 1: Locate checkpoint
	if err := recovery.TransitionTo(StateCheckpointLocated, ""); err != nil {
		return nil, fmt.Errorf("transition to checkpoint located: %w", err)
	}

	manifest, err := m.walReader.GetLatestCheckpoint(ctx)
	if err != nil {
		recovery.TransitionTo(StateRecoveryFailed, fmt.Sprintf("locate checkpoint: %v", err))
		if m.auditor != nil {
			m.auditor.LogRecoveryFailed(ctx, jobID, recovery.ErrorMessage)
		}
		return recovery, fmt.Errorf("get latest checkpoint: %w", err)
	}

	recovery.Checkpoint = manifest

	// Validate epoch matches
	if manifest.Epoch != newEpoch {
		recovery.TransitionTo(StateRecoveryFailed, fmt.Sprintf("epoch mismatch: got %d, want %d", manifest.Epoch, newEpoch))
		if m.auditor != nil {
			m.auditor.LogRecoveryFailed(ctx, jobID, recovery.ErrorMessage)
		}
		return recovery, ErrCheckpointEpochMismatch
	}

	if m.auditor != nil {
		m.auditor.LogCheckpointLocated(ctx, jobID, manifest.CheckpointURI)
	}

	m.logger.Info("checkpoint located",
		zap.String("jobID", jobID),
		zap.Int64("epoch", manifest.Epoch),
		zap.Int64("sequence", manifest.Sequence),
		zap.String("uri", manifest.CheckpointURI))

	// Step 2: Validate checkpoint
	if err := recovery.TransitionTo(StateCheckpointValidated, ""); err != nil {
		return nil, fmt.Errorf("transition to checkpoint validated: %w", err)
	}

	// Create validation context with timeout
	validationCtx, cancel := context.WithTimeout(ctx, m.cfg.ValidationTimeout)
	defer cancel()

	if err := m.validator.Validate(validationCtx, manifest); err != nil {
		recovery.TransitionTo(StateRecoveryFailed, fmt.Sprintf("validate checkpoint: %v", err))
		if m.auditor != nil {
			m.auditor.LogRecoveryFailed(ctx, jobID, recovery.ErrorMessage)
		}
		return recovery, fmt.Errorf("validate checkpoint: %w", err)
	}

	if m.auditor != nil {
		m.auditor.LogCheckpointValidated(ctx, jobID)
	}

	m.logger.Info("checkpoint validated",
		zap.String("jobID", jobID))

	// Step 3: Download checkpoint files
	if err := recovery.TransitionTo(StateFilesDownloading, ""); err != nil {
		return nil, fmt.Errorf("transition to files downloading: %w", err)
	}

	downloadCtx, cancel := context.WithTimeout(ctx, m.cfg.DownloadTimeout)
	defer cancel()

	for _, file := range manifest.Files {
		if err := m.downloadFile(downloadCtx, file); err != nil {
			recovery.TransitionTo(StateRecoveryFailed, fmt.Sprintf("download file %s: %v", file.Name, err))
			if m.auditor != nil {
				m.auditor.LogRecoveryFailed(ctx, jobID, recovery.ErrorMessage)
			}
			return recovery, fmt.Errorf("download file %s: %w", file.Name, err)
		}
	}

	if err := recovery.TransitionTo(StateFilesDownloaded, ""); err != nil {
		return nil, fmt.Errorf("transition to files downloaded: %w", err)
	}

	m.logger.Info("checkpoint files downloaded",
		zap.String("jobID", jobID),
		zap.Int("fileCount", len(manifest.Files)))

	// Step 4: Restore state
	if err := recovery.TransitionTo(StateRestoring, ""); err != nil {
		return nil, fmt.Errorf("transition to restoring: %w", err)
	}

	restoreCtx, cancel := context.WithTimeout(ctx, m.cfg.RestoreTimeout)
	defer cancel()

	if err := m.restorer.Restore(restoreCtx, manifest); err != nil {
		recovery.TransitionTo(StateRecoveryFailed, fmt.Sprintf("restore state: %v", err))
		if m.auditor != nil {
			m.auditor.LogRecoveryFailed(ctx, jobID, recovery.ErrorMessage)
		}
		return recovery, fmt.Errorf("restore state: %w", err)
	}

	// Step 5: Mark complete
	if err := recovery.TransitionTo(StateRecoveryComplete, ""); err != nil {
		return nil, fmt.Errorf("transition to recovery complete: %w", err)
	}

	duration := time.Since(recovery.StartedAt)
	if m.auditor != nil {
		m.auditor.LogRecoveryComplete(ctx, jobID, duration)
	}

	m.logger.Info("checkpoint-coupled recovery completed",
		zap.String("jobID", jobID),
		zap.Duration("duration", duration))

	return recovery, nil
}

// downloadFile downloads a single checkpoint file.
func (m *Manager) downloadFile(ctx context.Context, file CheckpointFile) error {
	m.logger.Debug("downloading checkpoint file",
		zap.String("name", file.Name),
		zap.Int64("size", file.Size))

	_, err := m.storage.GetObject(ctx, file.URI)
	if err != nil {
		return fmt.Errorf("get object: %w", err)
	}

	return nil
}

// RecoveryStats holds statistics for recoveries.
type RecoveryStats struct {
	TotalRecoveries    int64
	SuccessfulRecoveries int64
	FailedRecoveries   int64
}
