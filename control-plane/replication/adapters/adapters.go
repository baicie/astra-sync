// Package adapters contains deployment-owned implementations for replication SPI interfaces.
package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"io.astrasync/control-plane/replication/promotion"
	"io.astrasync/control-plane/replication/recovery"
	"io.astrasync/control-plane/replication/wal"
)

// JSONManifestParser decodes checkpoint manifests stored as JSON objects.
type JSONManifestParser struct{}

// Parse implements recovery.ManifestParser.
func (JSONManifestParser) Parse(data []byte) (*recovery.CheckpointManifest, error) {
	var manifest recovery.CheckpointManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse checkpoint manifest: %w", err)
	}
	return &manifest, nil
}

// CheckpointValidator validates manifest identity, file metadata, and checksums.
type CheckpointValidator struct{}

// Validate implements recovery.Validator.
func (CheckpointValidator) Validate(ctx context.Context, manifest *recovery.CheckpointManifest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if manifest == nil || strings.TrimSpace(manifest.JobID) == "" || manifest.Epoch < 0 || manifest.Sequence <= 0 || strings.TrimSpace(manifest.CheckpointURI) == "" {
		return recovery.ErrCheckpointCorrupted
	}
	for _, file := range manifest.Files {
		if strings.TrimSpace(file.Name) == "" || strings.TrimSpace(file.URI) == "" || file.Size < 0 {
			return fmt.Errorf("%w: invalid checkpoint file metadata", recovery.ErrCheckpointCorrupted)
		}
	}
	return nil
}

// FileStateRestorer is an explicit deployment boundary for state restoration.
// It validates that referenced files are readable; a worker-specific restorer can
// be injected later without changing the recovery manager contract.
type FileStateRestorer struct {
	storage recovery.ObjectStorage
}

// NewFileStateRestorer creates a restorer that verifies checkpoint objects exist.
func NewFileStateRestorer(storage recovery.ObjectStorage) (*FileStateRestorer, error) {
	if storage == nil {
		return nil, errors.New("state restorer storage is required")
	}
	return &FileStateRestorer{storage: storage}, nil
}

// Restore implements recovery.StateRestorer.
func (r *FileStateRestorer) Restore(ctx context.Context, manifest *recovery.CheckpointManifest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, file := range manifest.Files {
		reader, err := r.storage.GetObjectReader(ctx, file.URI)
		if err != nil {
			return fmt.Errorf("open checkpoint file %s: %w", file.Name, err)
		}
		if _, err := io.Copy(io.Discard, reader); err != nil {
			_ = reader.Close()
			return fmt.Errorf("read checkpoint file %s: %w", file.Name, err)
		}
		if err := reader.Close(); err != nil {
			return fmt.Errorf("close checkpoint file %s: %w", file.Name, err)
		}
	}
	return nil
}

// WALReader adapts the object-storage WAL reader to the recovery SPI.
type WALReader struct {
	reader *wal.Reader
	store  wal.ObjectStorage
	prefix string
	region string
}

// NewWALReader creates a recovery WAL reader over a deployment-owned object store.
func NewWALReader(ctx context.Context, store wal.ObjectStorage, logger *zap.Logger, region, prefix string, resumeFrom int64) (*WALReader, error) {
	if store == nil || logger == nil {
		return nil, errors.New("WAL reader store and logger are required")
	}
	reader, err := wal.NewReader(ctx, store, logger, region, "", prefix, resumeFrom)
	if err != nil {
		return nil, err
	}
	return &WALReader{reader: reader, store: store, prefix: strings.TrimSuffix(prefix, "/"), region: region}, nil
}

// ReadEntries implements recovery.WALEntryReader.
func (r *WALReader) ReadEntries(ctx context.Context, sinceSequence int64) ([]*recovery.WALEntry, error) {
	if sinceSequence < 0 {
		return nil, fmt.Errorf("since sequence must not be negative")
	}
	keys, err := r.store.ListObjects(ctx, fmt.Sprintf("%s/%s/", r.prefix, r.region))
	if err != nil {
		return nil, fmt.Errorf("list WAL objects: %w", err)
	}
	sequences := make([]int64, 0, len(keys))
	seen := make(map[int64]struct{}, len(keys))
	for _, key := range keys {
		sequence, ok := sequenceFromKey(key)
		if !ok || sequence <= sinceSequence {
			continue
		}
		if _, exists := seen[sequence]; exists {
			continue
		}
		seen[sequence] = struct{}{}
		sequences = append(sequences, sequence)
	}
	sort.Slice(sequences, func(i, j int) bool { return sequences[i] < sequences[j] })
	entries := make([]*recovery.WALEntry, 0, len(sequences))
	for _, sequence := range sequences {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%s/%s/%016d.wal", r.prefix, r.region, sequence)
		data, readErr := r.store.GetObject(ctx, key)
		if readErr != nil {
			return nil, fmt.Errorf("read WAL sequence %d: %w", sequence, readErr)
		}
		entry := &wal.Entry{}
		if readErr := entry.UnmarshalBinary(data); readErr != nil {
			return nil, fmt.Errorf("decode WAL sequence %d: %w", sequence, readErr)
		}
		if entry.Sequence != sequence || entry.Region != r.region {
			return nil, fmt.Errorf("%w: expected sequence=%d region=%q, got sequence=%d region=%q", wal.ErrGap, sequence, r.region, entry.Sequence, entry.Region)
		}
		entries = append(entries, toRecoveryEntry(entry))
	}
	return entries, nil
}

// GetLatestCheckpoint implements recovery.WALEntryReader.
func (r *WALReader) GetLatestCheckpoint(ctx context.Context) (*recovery.CheckpointManifest, error) {
	entries, err := r.ReadEntries(ctx, 0)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, recovery.ErrCheckpointNotFound
	}
	latest := entries[len(entries)-1]
	data, err := r.store.GetObject(ctx, latest.CheckpointURI)
	if err != nil {
		return nil, fmt.Errorf("read latest checkpoint manifest: %w", err)
	}
	manifest, err := (JSONManifestParser{}).Parse(data)
	if err != nil {
		return nil, err
	}
	if manifest.JobID != latest.JobID || manifest.Epoch != latest.Epoch || manifest.Sequence != latest.Sequence {
		return nil, recovery.ErrCheckpointCorrupted
	}
	return manifest, nil
}

func toRecoveryEntry(entry *wal.Entry) *recovery.WALEntry {
	return &recovery.WALEntry{Sequence: entry.Sequence, Region: entry.Region, Epoch: entry.Epoch, CheckpointURI: entry.CheckpointURI, JobID: entry.JobID, Timestamp: entry.Timestamp, CRC32C: entry.CRC32C}
}

func sequenceFromKey(key string) (int64, bool) {
	name := strings.TrimSuffix(key[strings.LastIndex(key, "/")+1:], ".wal")
	var sequence int64
	if _, err := fmt.Sscanf(name, "%d", &sequence); err != nil || sequence <= 0 {
		return 0, false
	}
	return sequence, true
}

// ZapAuditLogger emits recovery audit events through the process logger.
type ZapAuditLogger struct{ logger *zap.Logger }

// NewZapAuditLogger creates an audit logger.
func NewZapAuditLogger(logger *zap.Logger) (*ZapAuditLogger, error) {
	if logger == nil {
		return nil, errors.New("audit logger is required")
	}
	return &ZapAuditLogger{logger: logger}, nil
}

// LogRecoveryStarted implements recovery.AuditLogger.
func (a *ZapAuditLogger) LogRecoveryStarted(_ context.Context, jobID string, epoch int64) {
	a.logger.Info("recovery started", zap.String("job_id", jobID), zap.Int64("epoch", epoch))
}

// LogCheckpointLocated implements recovery.AuditLogger.
func (a *ZapAuditLogger) LogCheckpointLocated(_ context.Context, jobID, uri string) {
	a.logger.Info("checkpoint located", zap.String("job_id", jobID), zap.String("uri", uri))
}

// LogCheckpointValidated implements recovery.AuditLogger.
func (a *ZapAuditLogger) LogCheckpointValidated(_ context.Context, jobID string) {
	a.logger.Info("checkpoint validated", zap.String("job_id", jobID))
}

// LogRecoveryComplete implements recovery.AuditLogger.
func (a *ZapAuditLogger) LogRecoveryComplete(_ context.Context, jobID string, duration time.Duration) {
	a.logger.Info("recovery complete", zap.String("job_id", jobID), zap.Duration("duration", duration))
}

// LogRecoveryFailed implements recovery.AuditLogger.
func (a *ZapAuditLogger) LogRecoveryFailed(_ context.Context, jobID, reason string) {
	a.logger.Warn("recovery failed", zap.String("job_id", jobID), zap.String("reason", reason))
}

var _ promotion.EpochFencer = (*PostgreSQLStore)(nil)
var _ recovery.ManifestParser = JSONManifestParser{}
var _ recovery.Validator = CheckpointValidator{}
var _ recovery.StateRestorer = (*FileStateRestorer)(nil)
var _ recovery.WALEntryReader = (*WALReader)(nil)
var _ recovery.AuditLogger = (*ZapAuditLogger)(nil)
