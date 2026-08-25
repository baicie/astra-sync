package replication

import (
	"context"
	"errors"
)

// ErrCheckpointAdmissionConflict indicates that a sequence was already admitted with different content.
var ErrCheckpointAdmissionConflict = errors.New("checkpoint admission conflicts with an existing sequence")

// ErrCheckpointAdmissionInProgress indicates that another request is delivering the sequence.
var ErrCheckpointAdmissionInProgress = errors.New("checkpoint admission is in progress")

// CheckpointDeduplicator records checkpoint admissions and rejects conflicting replays.
type CheckpointDeduplicator interface {
	ClaimCheckpoint(ctx context.Context, sourceRegion, targetRegion, jobID string, sequence, epoch int64, checkpointURI string, crc32c uint32) (bool, error)
	CommitCheckpoint(ctx context.Context, sourceRegion, targetRegion, jobID string, sequence int64) error
	ReleaseCheckpoint(ctx context.Context, sourceRegion, targetRegion, jobID string, sequence int64) error
}
