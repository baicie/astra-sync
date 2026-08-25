package adapters

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"io.astrasync/control-plane/replication/promotion"
)

// PostgreSQLStore persists promotion state and job epochs in PostgreSQL.
type PostgreSQLStore struct{ db *sql.DB }

// NewPostgreSQLStore creates an adapter over an existing PostgreSQL connection.
func NewPostgreSQLStore(db *sql.DB) (*PostgreSQLStore, error) {
	if db == nil {
		return nil, errors.New("replication PostgreSQL database is required")
	}
	return &PostgreSQLStore{db: db}, nil
}

// Migrate creates replication-owned tables without changing control-plane job tables.
func (s *PostgreSQLStore) Migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS astrasync_replication_job_epochs (
    job_id TEXT PRIMARY KEY,
    epoch BIGINT NOT NULL CHECK (epoch >= 0),
    updated_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS astrasync_replication_promotions (
    idempotency_key TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    previous_region TEXT NOT NULL,
    target_region TEXT NOT NULL,
    previous_epoch BIGINT NOT NULL,
    new_epoch BIGINT NOT NULL,
    state INTEGER NOT NULL,
    error_message TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ NULL
);
CREATE INDEX IF NOT EXISTS astrasync_replication_promotions_job_idx
    ON astrasync_replication_promotions (job_id, started_at DESC);
CREATE TABLE IF NOT EXISTS astrasync_replication_replication_progress (
    source_region TEXT NOT NULL,
    target_region TEXT NOT NULL,
    acknowledged_sequence BIGINT NOT NULL CHECK (acknowledged_sequence >= 0),
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (source_region, target_region)
);
CREATE TABLE IF NOT EXISTS astrasync_replication_checkpoint_admissions (
    source_region TEXT NOT NULL,
    target_region TEXT NOT NULL,
    job_id TEXT NOT NULL,
    sequence BIGINT NOT NULL CHECK (sequence >= 0),
    epoch BIGINT NOT NULL CHECK (epoch >= 0),
    checkpoint_uri TEXT NOT NULL,
    crc32c BIGINT NOT NULL CHECK (crc32c >= 0),
    fingerprint TEXT NOT NULL,
    committed BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (source_region, target_region, job_id, sequence)
);`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("migrate replication metadata: %w", err)
	}
	return nil
}

// Assign allocates a new epoch monotonically for a job.
func (s *PostgreSQLStore) Assign(ctx context.Context, jobID string) (int64, error) {
	if jobID == "" {
		return 0, errors.New("job id must not be blank")
	}
	var epoch int64
	err := s.db.QueryRowContext(ctx, `
INSERT INTO astrasync_replication_job_epochs (job_id, epoch, updated_at)
VALUES ($1, 1, $2)
ON CONFLICT (job_id) DO UPDATE SET epoch = astrasync_replication_job_epochs.epoch + 1, updated_at = EXCLUDED.updated_at
RETURNING epoch`, jobID, time.Now().UTC()).Scan(&epoch)
	if err != nil {
		return 0, fmt.Errorf("assign replication epoch: %w", err)
	}
	return epoch, nil
}

// GetEpoch returns the current epoch, or zero when no epoch has been allocated.
func (s *PostgreSQLStore) GetEpoch(ctx context.Context, jobID string) (int64, error) {
	var epoch int64
	err := s.db.QueryRowContext(ctx, `SELECT epoch FROM astrasync_replication_job_epochs WHERE job_id = $1`, jobID).Scan(&epoch)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get replication epoch: %w", err)
	}
	return epoch, nil
}

// UpdateEpoch applies the promotion's optimistic epoch write.
func (s *PostgreSQLStore) UpdateEpoch(ctx context.Context, jobID string, expectedEpoch, newEpoch int64) error {
	result, err := s.db.ExecContext(ctx, `UPDATE astrasync_replication_job_epochs SET epoch = $1, updated_at = $2 WHERE job_id = $3 AND epoch = $4`, newEpoch, time.Now().UTC(), jobID, expectedEpoch)
	if err != nil {
		return fmt.Errorf("update replication epoch: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check replication epoch update: %w", err)
	}
	if count == 1 {
		return nil
	}
	var current int64
	if err := s.db.QueryRowContext(ctx, `SELECT epoch FROM astrasync_replication_job_epochs WHERE job_id = $1`, jobID).Scan(&current); err != nil {
		return fmt.Errorf("read replication epoch after update conflict: %w", err)
	}
	if current == newEpoch {
		return nil
	}
	return promotion.ErrEpochConflict
}

// Fence verifies that the expected epoch remains current.
func (s *PostgreSQLStore) Fence(ctx context.Context, jobID string, expectedEpoch int64) error {
	current, err := s.GetEpoch(ctx, jobID)
	if err != nil {
		return err
	}
	if current != expectedEpoch {
		return promotion.ErrEpochConflict
	}
	return nil
}

// Create stores a promotion record.
func (s *PostgreSQLStore) Create(ctx context.Context, p *promotion.Promotion) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO astrasync_replication_promotions (idempotency_key, job_id, previous_region, target_region, previous_epoch, new_epoch, state, error_message, started_at, completed_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, p.IdempotencyKey, p.JobID, p.PreviousRegion, p.TargetRegion, p.PreviousEpoch, p.NewEpoch, p.State, p.ErrorMessage, p.StartedAt, p.CompletedAt)
	if err != nil {
		return fmt.Errorf("create promotion: %w", err)
	}
	return nil
}

// Update stores the current promotion state.
func (s *PostgreSQLStore) Update(ctx context.Context, p *promotion.Promotion) error {
	_, err := s.db.ExecContext(ctx, `UPDATE astrasync_replication_promotions SET new_epoch=$1, state=$2, error_message=$3, completed_at=$4 WHERE idempotency_key=$5`, p.NewEpoch, p.State, p.ErrorMessage, p.CompletedAt, p.IdempotencyKey)
	if err != nil {
		return fmt.Errorf("update promotion: %w", err)
	}
	return nil
}

// Get loads a promotion by job and idempotency key.
func (s *PostgreSQLStore) Get(ctx context.Context, jobID, key string) (*promotion.Promotion, error) {
	return s.get(ctx, `WHERE job_id=$1 AND idempotency_key=$2`, jobID, key)
}

// GetLatest loads the newest promotion for a job.
func (s *PostgreSQLStore) GetLatest(ctx context.Context, jobID string) (*promotion.Promotion, error) {
	return s.get(ctx, `WHERE job_id=$1 ORDER BY started_at DESC LIMIT 1`, jobID)
}

func (s *PostgreSQLStore) get(ctx context.Context, suffix string, args ...any) (*promotion.Promotion, error) {
	var p promotion.Promotion
	var state int
	var completed sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT idempotency_key, job_id, previous_region, target_region, previous_epoch, new_epoch, state, error_message, started_at, completed_at FROM astrasync_replication_promotions `+suffix, args...).Scan(&p.IdempotencyKey, &p.JobID, &p.PreviousRegion, &p.TargetRegion, &p.PreviousEpoch, &p.NewEpoch, &state, &p.ErrorMessage, &p.StartedAt, &completed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get promotion: %w", err)
	}
	p.ID = p.IdempotencyKey
	p.State = promotion.PromotionState(state)
	if completed.Valid {
		value := completed.Time
		p.CompletedAt = &value
	}
	return &p, nil
}

var _ promotion.PromotionStore = (*PostgreSQLStore)(nil)
var _ promotion.EpochAssigner = (*PostgreSQLStore)(nil)
var _ promotion.JobReader = (*PostgreSQLStore)(nil)
var _ promotion.JobWriter = (*PostgreSQLStore)(nil)
var _ promotion.EpochFencer = (*PostgreSQLStore)(nil)
