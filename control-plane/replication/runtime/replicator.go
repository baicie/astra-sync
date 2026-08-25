package runtime

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"io.astrasync/control-plane/replication/channel"
	"io.astrasync/control-plane/replication/recovery"
)

var ErrInvalidReplicator = errors.New("replication replicator: invalid configuration")

type EventEncoder func(*recovery.WALEntry) (*channel.Event, error)

type ReplicatorConfig struct {
	BatchSize    int
	PollInterval time.Duration
	RetryInitial time.Duration
	RetryMax     time.Duration
}

type Replicator struct {
	logger        *zap.Logger
	reader        recovery.WALEntryReader
	sender        channel.EventSender
	encode        EventEncoder
	config        ReplicatorConfig
	onAcknowledge func(context.Context, int64) error
}

func NewReplicator(logger *zap.Logger, reader recovery.WALEntryReader, sender channel.EventSender, encode EventEncoder, cfg ReplicatorConfig) (*Replicator, error) {
	if logger == nil || reader == nil || sender == nil || encode == nil {
		return nil, fmt.Errorf("%w: logger, reader, sender, and encoder are required", ErrInvalidReplicator)
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 128
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = time.Second
	}
	if cfg.RetryInitial == 0 {
		cfg.RetryInitial = 100 * time.Millisecond
	}
	if cfg.RetryMax == 0 {
		cfg.RetryMax = 5 * time.Second
	}
	if cfg.BatchSize < 0 || cfg.PollInterval < 0 || cfg.RetryInitial < 0 || cfg.RetryMax < cfg.RetryInitial {
		return nil, fmt.Errorf("%w: invalid batch, poll, or retry settings", ErrInvalidReplicator)
	}
	return &Replicator{logger: logger, reader: reader, sender: sender, encode: encode, config: cfg}, nil
}

func (r *Replicator) SetAcknowledgeCallback(callback func(context.Context, int64) error) {
	r.onAcknowledge = callback
}

func (r *Replicator) Run(ctx context.Context, sinceSequence int64) error {
	if sinceSequence < 0 {
		return fmt.Errorf("%w: sequence must not be negative", ErrInvalidReplicator)
	}
	for {
		entries, err := r.reader.ReadEntries(ctx, sinceSequence)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			return fmt.Errorf("read WAL entries after %d: %w", sinceSequence, err)
		}
		if len(entries) == 0 {
			if err := wait(ctx, r.config.PollInterval); err != nil {
				return err
			}
			continue
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Sequence < entries[j].Sequence })
		for index, entry := range entries {
			if entry == nil || entry.Sequence <= sinceSequence || (index > 0 && entry.Sequence != entries[index-1].Sequence+1) {
				return fmt.Errorf("%w: WAL sequence gap after %d", ErrInvalidReplicator, sinceSequence)
			}
		}
		if len(entries) > r.config.BatchSize {
			entries = entries[:r.config.BatchSize]
		}
		for _, entry := range entries {
			if entry == nil || entry.Sequence <= sinceSequence {
				return fmt.Errorf("%w: WAL entries are invalid or not strictly increasing", ErrInvalidReplicator)
			}
			if err := r.sendWithRetry(ctx, entry); err != nil {
				return err
			}
			if r.onAcknowledge != nil {
				if err := r.onAcknowledge(ctx, entry.Sequence); err != nil {
					return fmt.Errorf("persist acknowledged WAL sequence %d: %w", entry.Sequence, err)
				}
			}
			sinceSequence = entry.Sequence
		}
	}
}

func (r *Replicator) sendWithRetry(ctx context.Context, entry *recovery.WALEntry) error {
	event, err := r.encode(entry)
	if err != nil {
		return fmt.Errorf("encode WAL sequence %d: %w", entry.Sequence, err)
	}
	if event == nil {
		return fmt.Errorf("%w: encoder returned nil event for sequence %d", ErrInvalidReplicator, entry.Sequence)
	}
	backoff := r.config.RetryInitial
	for {
		if err := r.sender.SendEvent(ctx, event); err == nil {
			return nil
		} else {
			if !retryableSendError(err) {
				return fmt.Errorf("send WAL sequence %d: %w", entry.Sequence, err)
			}
			r.logger.Warn("checkpoint replication send failed", zap.Int64("sequence", entry.Sequence), zap.Error(err))
		}
		if err := wait(ctx, backoff); err != nil {
			return fmt.Errorf("send WAL sequence %d: %w", entry.Sequence, err)
		}
		if backoff < r.config.RetryMax {
			backoff *= 2
			if backoff > r.config.RetryMax {
				backoff = r.config.RetryMax
			}
		}
	}
}

func retryableSendError(err error) bool {
	switch status.Code(err) {
	case codes.Unknown, codes.Canceled, codes.InvalidArgument, codes.AlreadyExists, codes.PermissionDenied, codes.Unauthenticated, codes.FailedPrecondition:
		return status.Code(err) == codes.Unknown
	case codes.ResourceExhausted, codes.Aborted, codes.DeadlineExceeded, codes.Unavailable:
		return true
	default:
		return false
	}
}

func wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
