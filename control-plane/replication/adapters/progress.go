package adapters

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// LoadReplicationProgress returns the last acknowledged sequence for a region pair.
func (s *PostgreSQLStore) LoadReplicationProgress(ctx context.Context, sourceRegion, targetRegion string) (int64, error) {
	var sequence int64
	err := s.db.QueryRowContext(ctx, `SELECT acknowledged_sequence FROM astrasync_replication_replication_progress WHERE source_region=$1 AND target_region=$2`, sourceRegion, targetRegion).Scan(&sequence)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("load replication progress: %w", err)
	}
	return sequence, nil
}

// SaveReplicationProgress advances the acknowledged sequence monotonically.
func (s *PostgreSQLStore) SaveReplicationProgress(ctx context.Context, sourceRegion, targetRegion string, sequence int64) error {
	if sequence < 0 {
		return fmt.Errorf("replication sequence must not be negative")
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO astrasync_replication_replication_progress (source_region, target_region, acknowledged_sequence, updated_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (source_region, target_region) DO UPDATE
SET acknowledged_sequence = GREATEST(astrasync_replication_replication_progress.acknowledged_sequence, EXCLUDED.acknowledged_sequence), updated_at = EXCLUDED.updated_at`, sourceRegion, targetRegion, sequence, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("save replication progress: %w", err)
	}
	return nil
}

var _ interface {
	LoadReplicationProgress(context.Context, string, string) (int64, error)
	SaveReplicationProgress(context.Context, string, string, int64) error
} = (*PostgreSQLStore)(nil)
