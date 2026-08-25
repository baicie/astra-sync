package adapters

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	replication "io.astrasync/control-plane/replication"
)

var _ interface {
	ClaimCheckpoint(context.Context, string, string, string, int64, int64, string, uint32) (bool, error)
	CommitCheckpoint(context.Context, string, string, string, int64) error
	ReleaseCheckpoint(context.Context, string, string, string, int64) error
} = (*PostgreSQLStore)(nil)

// ClaimCheckpoint records a checkpoint admission and reports whether delivery is required.
func (s *PostgreSQLStore) ClaimCheckpoint(ctx context.Context, sourceRegion, targetRegion, jobID string, sequence, epoch int64, checkpointURI string, crc32c uint32) (bool, error) {
	if sourceRegion == "" || targetRegion == "" || jobID == "" || sequence < 0 || epoch < 0 {
		return false, errors.New("checkpoint admission fields are invalid")
	}
	fingerprint := checkpointFingerprint(sourceRegion, targetRegion, jobID, sequence, epoch, checkpointURI, crc32c)
	var inserted bool
	err := s.db.QueryRowContext(ctx, `
INSERT INTO astrasync_replication_checkpoint_admissions
(source_region, target_region, job_id, sequence, epoch, checkpoint_uri, crc32c, fingerprint, committed, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,FALSE,$9)
ON CONFLICT (source_region, target_region, job_id, sequence) DO NOTHING
RETURNING TRUE`, sourceRegion, targetRegion, jobID, sequence, epoch, checkpointURI, crc32c, fingerprint, time.Now().UTC()).Scan(&inserted)
	if err == nil && inserted {
		return true, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("reserve checkpoint admission: %w", err)
	}
	var existing string
	var committed bool
	err = s.db.QueryRowContext(ctx, `
SELECT fingerprint, committed
FROM astrasync_replication_checkpoint_admissions
WHERE source_region=$1 AND target_region=$2 AND job_id=$3 AND sequence=$4`, sourceRegion, targetRegion, jobID, sequence).Scan(&existing, &committed)
	if err != nil {
		return false, fmt.Errorf("get checkpoint admission: %w", err)
	}
	if existing != fingerprint {
		return false, replication.ErrCheckpointAdmissionConflict
	}
	if !committed {
		return false, replication.ErrCheckpointAdmissionInProgress
	}
	return false, nil
}

// CommitCheckpoint marks a successfully delivered checkpoint admission.
func (s *PostgreSQLStore) CommitCheckpoint(ctx context.Context, sourceRegion, targetRegion, jobID string, sequence int64) error {
	result, err := s.db.ExecContext(ctx, `UPDATE astrasync_replication_checkpoint_admissions SET committed=TRUE, updated_at=$1 WHERE source_region=$2 AND target_region=$3 AND job_id=$4 AND sequence=$5`, time.Now().UTC(), sourceRegion, targetRegion, jobID, sequence)
	if err != nil {
		return fmt.Errorf("commit checkpoint admission: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check checkpoint admission commit: %w", err)
	}
	if count != 1 {
		return sql.ErrNoRows
	}
	return nil
}

// ReleaseCheckpoint removes an uncommitted checkpoint admission after failed delivery.
func (s *PostgreSQLStore) ReleaseCheckpoint(ctx context.Context, sourceRegion, targetRegion, jobID string, sequence int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM astrasync_replication_checkpoint_admissions WHERE source_region=$1 AND target_region=$2 AND job_id=$3 AND sequence=$4 AND committed=FALSE`, sourceRegion, targetRegion, jobID, sequence)
	if err != nil {
		return fmt.Errorf("release checkpoint admission: %w", err)
	}
	return nil
}

func checkpointFingerprint(sourceRegion, targetRegion, jobID string, sequence, epoch int64, checkpointURI string, crc32c uint32) string {
	hash := sha256.New()
	fmt.Fprintf(hash, "%s\x00%s\x00%s\x00%d\x00%d\x00%s\x00%d", sourceRegion, targetRegion, jobID, sequence, epoch, checkpointURI, crc32c)
	return hex.EncodeToString(hash.Sum(nil))
}
