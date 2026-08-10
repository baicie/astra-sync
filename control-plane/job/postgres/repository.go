package postgres

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"io.astrasync/control-plane/job"
)

//go:embed migrations/001_jobs.sql
var migration string

//go:embed migrations/002_job_mutations.sql
var mutationMigration string

type Repository struct {
	db *sql.DB
}

func Open(ctx context.Context, dataSourceName string) (*Repository, error) {
	if dataSourceName == "" {
		return nil, fmt.Errorf("database URL must not be blank")
	}
	db, err := sql.Open("pgx", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	return New(db), nil
}

func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Migrate(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, migration); err != nil {
		return fmt.Errorf("migrate control-plane jobs: %w", err)
	}
	return nil
}

// MigrateMutations must run after the auth and Connection schemas because the
// mutation tables reference tenants and execution bindings.
func (r *Repository) MigrateMutations(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, mutationMigration); err != nil {
		return fmt.Errorf("migrate atomic Job mutations: %w", err)
	}
	return nil
}

func (r *Repository) Ping(ctx context.Context) error {
	return r.db.PingContext(ctx)
}

func (r *Repository) Close() error {
	return r.db.Close()
}

func (r *Repository) Create(ctx context.Context, candidate job.Job) (job.Job, error) {
	if err := candidate.Validate(); err != nil {
		return job.Job{}, err
	}
	spec, status, err := documents(candidate)
	if err != nil {
		return job.Job{}, err
	}
	_, err = r.db.ExecContext(
		ctx,
		`INSERT INTO astrasync_control_jobs
            (namespace, name, uid, version, spec, status, created_at, updated_at)
         VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7, $8)`,
		candidate.Key.Namespace,
		candidate.Key.Name,
		candidate.UID,
		candidate.Version,
		spec,
		status,
		candidate.CreatedAt,
		candidate.UpdatedAt,
	)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return job.Job{}, job.ErrAlreadyExists
		}
		return job.Job{}, fmt.Errorf("create job: %w", err)
	}
	return candidate.Clone(), nil
}

func (r *Repository) Get(ctx context.Context, key job.Key) (job.Job, error) {
	if err := key.Validate(); err != nil {
		return job.Job{}, err
	}
	stored, err := scanJob(r.db.QueryRowContext(
		ctx,
		`SELECT namespace, name, uid::text, version, spec, status, created_at, updated_at
           FROM astrasync_control_jobs
          WHERE namespace = $1 AND name = $2`,
		key.Namespace,
		key.Name,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return job.Job{}, job.ErrNotFound
	}
	if err != nil {
		return job.Job{}, fmt.Errorf("get job: %w", err)
	}
	return stored, nil
}

func (r *Repository) List(ctx context.Context, namespace string, page job.Page) (job.PageResult, error) {
	if err := (job.Key{Namespace: namespace, Name: "validation"}).Validate(); err != nil {
		return job.PageResult{}, err
	}
	if err := page.Validate(); err != nil {
		return job.PageResult{}, err
	}
	var total int
	if err := r.db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM astrasync_control_jobs WHERE namespace = $1`,
		namespace,
	).Scan(&total); err != nil {
		return job.PageResult{}, fmt.Errorf("count jobs: %w", err)
	}
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT namespace, name, uid::text, version, spec, status, created_at, updated_at
           FROM astrasync_control_jobs
          WHERE namespace = $1
          ORDER BY name
          LIMIT $2 OFFSET $3`,
		namespace,
		page.Size,
		(page.Number-1)*page.Size,
	)
	if err != nil {
		return job.PageResult{}, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()

	result := job.PageResult{Jobs: []job.Job{}, Total: total}
	for rows.Next() {
		stored, scanErr := scanJob(rows)
		if scanErr != nil {
			return job.PageResult{}, fmt.Errorf("scan listed job: %w", scanErr)
		}
		result.Jobs = append(result.Jobs, stored)
	}
	if err := rows.Err(); err != nil {
		return job.PageResult{}, fmt.Errorf("iterate listed jobs: %w", err)
	}
	return result, nil
}

func (r *Repository) Update(ctx context.Context, candidate job.Job, expectedVersion int64) (job.Job, error) {
	if err := candidate.Validate(); err != nil {
		return job.Job{}, err
	}
	spec, status, err := documents(candidate)
	if err != nil {
		return job.Job{}, err
	}
	stored, err := scanJob(r.db.QueryRowContext(
		ctx,
		`UPDATE astrasync_control_jobs
            SET spec = $1::jsonb,
                status = $2::jsonb,
                version = version + 1,
                updated_at = $3
          WHERE namespace = $4 AND name = $5 AND version = $6 AND uid = $7::uuid
          RETURNING namespace, name, uid::text, version, spec, status, created_at, updated_at`,
		spec,
		status,
		candidate.UpdatedAt,
		candidate.Key.Namespace,
		candidate.Key.Name,
		expectedVersion,
		candidate.UID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return job.Job{}, r.conflictOrNotFound(ctx, candidate.Key)
	}
	if err != nil {
		return job.Job{}, fmt.Errorf("update job: %w", err)
	}
	return stored, nil
}

func (r *Repository) Delete(ctx context.Context, key job.Key, expectedVersion int64) error {
	if err := key.Validate(); err != nil {
		return err
	}
	if expectedVersion <= 0 {
		return job.ErrConflict
	}
	result, err := r.db.ExecContext(
		ctx,
		`DELETE FROM astrasync_control_jobs
          WHERE namespace = $1 AND name = $2 AND version = $3`,
		key.Namespace,
		key.Name,
		expectedVersion,
	)
	if err != nil {
		return fmt.Errorf("delete job: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted row count: %w", err)
	}
	if deleted == 0 {
		return r.conflictOrNotFound(ctx, key)
	}
	return nil
}

func (r *Repository) conflictOrNotFound(ctx context.Context, key job.Key) error {
	var exists bool
	if err := r.db.QueryRowContext(
		ctx,
		`SELECT EXISTS (
            SELECT 1 FROM astrasync_control_jobs WHERE namespace = $1 AND name = $2
         )`,
		key.Namespace,
		key.Name,
	).Scan(&exists); err != nil {
		return fmt.Errorf("resolve job version conflict: %w", err)
	}
	if exists {
		return job.ErrConflict
	}
	return job.ErrNotFound
}

func documents(candidate job.Job) (string, string, error) {
	spec, err := json.Marshal(candidate.Spec)
	if err != nil {
		return "", "", fmt.Errorf("encode job spec: %w", err)
	}
	status, err := json.Marshal(candidate.Status)
	if err != nil {
		return "", "", fmt.Errorf("encode job status: %w", err)
	}
	return string(spec), string(status), nil
}

type scanner interface {
	Scan(...any) error
}

func scanJob(source scanner) (job.Job, error) {
	var stored job.Job
	var spec []byte
	var status []byte
	err := source.Scan(
		&stored.Key.Namespace,
		&stored.Key.Name,
		&stored.UID,
		&stored.Version,
		&spec,
		&status,
		&stored.CreatedAt,
		&stored.UpdatedAt,
	)
	if err != nil {
		return job.Job{}, err
	}
	if err := json.Unmarshal(spec, &stored.Spec); err != nil {
		return job.Job{}, fmt.Errorf("decode job spec: %w", err)
	}
	if err := json.Unmarshal(status, &stored.Status); err != nil {
		return job.Job{}, fmt.Errorf("decode job status: %w", err)
	}
	if err := stored.Validate(); err != nil {
		return job.Job{}, fmt.Errorf("validate stored job: %w", err)
	}
	return stored, nil
}
