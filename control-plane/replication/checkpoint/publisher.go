// Package checkpoint provides the production boundary between checkpoint completion and replication WAL.
package checkpoint

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"io.astrasync/control-plane/replication/wal"
)

var (
	ErrInvalidRecord = errors.New("checkpoint publisher: invalid record")
	ErrEpochFenced   = errors.New("checkpoint publisher: epoch is fenced")
)

// Record identifies a completed, durable checkpoint.
type Record struct {
	JobID         string
	Epoch         int64
	CheckpointURI string
	CompletedAt   time.Time
}

// EpochReader returns the currently writable epoch for a job.
type EpochReader interface {
	GetEpoch(context.Context, string) (int64, error)
}

// Publisher appends completed checkpoints to the replication WAL.
type Publisher interface {
	Publish(context.Context, Record) (*wal.Entry, error)
}

// WALPublisher validates checkpoint ownership and appends it to the WAL.
type WALPublisher struct {
	writer *wal.Writer
	epochs EpochReader
}

// NewWALPublisher creates a producer over an already configured WAL writer.
func NewWALPublisher(writer *wal.Writer, epochs EpochReader) (*WALPublisher, error) {
	if writer == nil || epochs == nil {
		return nil, fmt.Errorf("%w: writer and epoch reader are required", ErrInvalidRecord)
	}
	return &WALPublisher{writer: writer, epochs: epochs}, nil
}

// Publish writes only checkpoints whose epoch is still active.
func (p *WALPublisher) Publish(ctx context.Context, record Record) (*wal.Entry, error) {
	if strings.TrimSpace(record.JobID) == "" || strings.TrimSpace(record.CheckpointURI) == "" || record.Epoch < 0 {
		return nil, ErrInvalidRecord
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	current, err := p.epochs.GetEpoch(ctx, record.JobID)
	if err != nil {
		return nil, fmt.Errorf("read active epoch: %w", err)
	}
	if current != record.Epoch {
		return nil, fmt.Errorf("%w: job=%s expected=%d current=%d", ErrEpochFenced, record.JobID, record.Epoch, current)
	}
	entry := &wal.Entry{Epoch: record.Epoch, JobID: record.JobID, CheckpointURI: record.CheckpointURI, Timestamp: record.CompletedAt}
	if err := p.writer.AppendDurable(ctx, entry); err != nil {
		return nil, fmt.Errorf("append checkpoint WAL: %w", err)
	}
	return entry, nil
}
