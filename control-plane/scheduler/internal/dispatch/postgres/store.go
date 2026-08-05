package postgres

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"time"

	"io.astrasync/control-plane/scheduler/internal/dispatch"
)

//go:embed migrations/002_scheduler_dispatches.sql
var migration string

const admissionLockID int64 = 0x415354524153594e

type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Migrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("dispatch database must not be nil")
	}
	if _, err := s.db.ExecContext(ctx, migration); err != nil {
		return fmt.Errorf("migrate scheduler dispatches: %w", err)
	}
	return nil
}

func (s *Store) Claim(
	ctx context.Context,
	ownerID string,
	maxActive int,
	leaseDuration time.Duration,
	now time.Time,
) (_ []dispatch.Record, resultErr error) {
	if ownerID == "" {
		return nil, fmt.Errorf("owner ID must not be blank")
	}
	if maxActive <= 0 {
		return nil, fmt.Errorf("maximum active dispatches must be positive")
	}
	if leaseDuration <= 0 {
		return nil, fmt.Errorf("lease duration must be positive")
	}
	now = now.UTC()
	leaseExpiresAt := now.Add(leaseDuration)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin dispatch claim: %w", err)
	}
	defer func() {
		if resultErr != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, admissionLockID); err != nil {
		return nil, fmt.Errorf("lock scheduler admission: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE astrasync_scheduler_dispatches
            SET lease_expires_at = $2, updated_at = $3
          WHERE owner_id = $1
            AND phase IN ('CLAIMED', 'STARTING', 'RUNNING', 'STOPPING')`,
		ownerID,
		leaseExpiresAt,
		now,
	); err != nil {
		return nil, fmt.Errorf("renew owned dispatch leases: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE astrasync_scheduler_dispatches
            SET owner_id = $1,
                lease_expires_at = $2,
                attempt = attempt + 1,
                updated_at = $3
          WHERE phase IN ('CLAIMED', 'STARTING', 'RUNNING', 'STOPPING')
            AND lease_expires_at <= $3`,
		ownerID,
		leaseExpiresAt,
		now,
	); err != nil {
		return nil, fmt.Errorf("take over expired dispatch leases: %w", err)
	}
	var activeCount int
	if err := tx.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
           FROM astrasync_scheduler_dispatches
          WHERE phase IN ('CLAIMED', 'STARTING', 'RUNNING', 'STOPPING')`,
	).Scan(&activeCount); err != nil {
		return nil, fmt.Errorf("count active dispatches: %w", err)
	}
	available := maxActive - activeCount
	if available > 0 {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO astrasync_scheduler_dispatches
                (job_uid, execution_epoch, namespace, name, owner_id, phase,
                 lease_expires_at, attempt, last_error, created_at, updated_at)
             SELECT jobs.uid,
                    (jobs.status->>'epoch')::bigint,
                    jobs.namespace,
                    jobs.name,
                    $1,
                    'CLAIMED',
                    $2,
                    1,
                    '',
                    $3,
                    $3
               FROM astrasync_control_jobs AS jobs
              WHERE jobs.status->>'desiredState' = 'RUNNING'
                AND jobs.status->>'state' = 'INITIALIZING'
                AND (jobs.status->>'epoch')::bigint > 0
                AND NOT EXISTS (
                    SELECT 1
                      FROM astrasync_scheduler_dispatches AS dispatches
                     WHERE dispatches.job_uid = jobs.uid
                       AND dispatches.execution_epoch = (jobs.status->>'epoch')::bigint
                )
              ORDER BY jobs.updated_at, jobs.namespace, jobs.name
              LIMIT $4
             ON CONFLICT DO NOTHING`,
			ownerID,
			leaseExpiresAt,
			now,
			available,
		); err != nil {
			return nil, fmt.Errorf("claim pending dispatches: %w", err)
		}
	}
	rows, err := tx.QueryContext(
		ctx,
		`SELECT job_uid::text, execution_epoch, namespace, name, owner_id, phase,
                lease_expires_at, attempt, last_error, created_at, updated_at
           FROM astrasync_scheduler_dispatches
          WHERE owner_id = $1
            AND phase IN ('CLAIMED', 'STARTING', 'RUNNING', 'STOPPING')
          ORDER BY created_at, namespace, name`,
		ownerID,
	)
	if err != nil {
		return nil, fmt.Errorf("list claimed dispatches: %w", err)
	}
	defer rows.Close()
	claimed := make([]dispatch.Record, 0)
	for rows.Next() {
		record, scanErr := scanRecord(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan claimed dispatch: %w", scanErr)
		}
		claimed = append(claimed, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claimed dispatches: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit dispatch claim: %w", err)
	}
	return claimed, nil
}

func (s *Store) Update(
	ctx context.Context,
	identity dispatch.Identity,
	ownerID string,
	phase dispatch.Phase,
	lastError string,
	leaseDuration time.Duration,
	now time.Time,
) error {
	if err := identity.Validate(); err != nil {
		return err
	}
	if ownerID == "" || !dispatch.Active(phase) || leaseDuration <= 0 {
		return fmt.Errorf("active dispatch update is invalid")
	}
	now = now.UTC()
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE astrasync_scheduler_dispatches
            SET phase = $4, last_error = $5, lease_expires_at = $6, updated_at = $7
          WHERE job_uid = $1::uuid
            AND execution_epoch = $2
            AND owner_id = $3
            AND lease_expires_at > $7
            AND phase IN ('CLAIMED', 'STARTING', 'RUNNING', 'STOPPING')`,
		identity.JobUID,
		identity.Epoch,
		ownerID,
		phase,
		lastError,
		now.Add(leaseDuration),
		now,
	)
	if err != nil {
		return fmt.Errorf("update dispatch: %w", err)
	}
	return requireAffected(result)
}

func (s *Store) Complete(
	ctx context.Context,
	identity dispatch.Identity,
	ownerID string,
	phase dispatch.Phase,
	lastError string,
	now time.Time,
) error {
	if err := identity.Validate(); err != nil {
		return err
	}
	if ownerID == "" || !dispatch.Terminal(phase) {
		return fmt.Errorf("terminal dispatch update is invalid")
	}
	now = now.UTC()
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE astrasync_scheduler_dispatches
            SET phase = $4, last_error = $5, lease_expires_at = $6, updated_at = $6
          WHERE job_uid = $1::uuid
            AND execution_epoch = $2
            AND owner_id = $3
            AND lease_expires_at > $6
            AND phase IN ('CLAIMED', 'STARTING', 'RUNNING', 'STOPPING')`,
		identity.JobUID,
		identity.Epoch,
		ownerID,
		phase,
		lastError,
		now,
	)
	if err != nil {
		return fmt.Errorf("complete dispatch: %w", err)
	}
	return requireAffected(result)
}

func requireAffected(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read dispatch update count: %w", err)
	}
	if count != 1 {
		return dispatch.ErrLeaseLost
	}
	return nil
}

type scanner interface {
	Scan(...any) error
}

func scanRecord(source scanner) (dispatch.Record, error) {
	var record dispatch.Record
	err := source.Scan(
		&record.Identity.JobUID,
		&record.Identity.Epoch,
		&record.Key.Namespace,
		&record.Key.Name,
		&record.OwnerID,
		&record.Phase,
		&record.LeaseExpiresAt,
		&record.Attempt,
		&record.LastError,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		return dispatch.Record{}, err
	}
	if err := record.Identity.Validate(); err != nil {
		return dispatch.Record{}, err
	}
	if err := record.Key.Validate(); err != nil {
		return dispatch.Record{}, err
	}
	if !dispatch.Active(record.Phase) {
		return dispatch.Record{}, fmt.Errorf("claimed dispatch has invalid phase %q", record.Phase)
	}
	return record, nil
}
