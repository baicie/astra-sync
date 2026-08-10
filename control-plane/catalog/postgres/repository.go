package postgres

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"io.astrasync/control-plane/catalog"
)

//go:embed migrations/001_catalog.sql
var migration string

type Repository struct {
	db *sql.DB
}

func Open(ctx context.Context, dataSourceName string) (*Repository, error) {
	if dataSourceName == "" {
		return nil, fmt.Errorf("database URL must not be blank")
	}
	db, err := sql.Open("pgx", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("open catalog PostgreSQL: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect catalog PostgreSQL: %w", err)
	}
	return New(db), nil
}

func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Migrate(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, migration); err != nil {
		return fmt.Errorf("migrate connector catalog: %w", err)
	}
	return nil
}

func (r *Repository) Close() error {
	return r.db.Close()
}

func (r *Repository) Ping(ctx context.Context) error {
	return r.db.PingContext(ctx)
}

func (r *Repository) Activate(
	ctx context.Context, candidate catalog.Snapshot, event catalog.AuditEvent,
) (changed bool, resultErr error) {
	if err := candidate.Validate(); err != nil {
		return false, err
	}
	if err := event.Validate(); err != nil {
		return false, err
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return false, fmt.Errorf("begin catalog activation: %w", err)
	}
	defer func() {
		if resultErr != nil {
			_ = tx.Rollback()
		}
	}()

	for _, descriptor := range candidate.Descriptors {
		if err := insertDescriptor(ctx, tx, descriptor, candidate.ActivatedAt); err != nil {
			return false, err
		}
	}
	if err := insertInventory(ctx, tx, candidate); err != nil {
		return false, err
	}
	for position, descriptor := range candidate.Descriptors {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO astrasync_connector_inventory_descriptors
                (inventory_revision, position, descriptor_revision)
             VALUES ($1, $2, $3)
             ON CONFLICT (inventory_revision, position) DO NOTHING`,
			candidate.InventoryRevision, position, descriptor.Revision); err != nil {
			return false, fmt.Errorf("link connector descriptor snapshot: %w", err)
		}
	}
	var oldRevision string
	err = tx.QueryRowContext(ctx,
		`SELECT inventory_revision
           FROM astrasync_connector_inventory_activation
          WHERE execution_profile = $1
          FOR UPDATE`, candidate.ExecutionProfile).Scan(&oldRevision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("lock connector inventory activation: %w", err)
	}
	changed = oldRevision != candidate.InventoryRevision
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO astrasync_connector_inventory_activation
            (execution_profile, inventory_revision, activated_at)
         VALUES ($1, $2, $3)
         ON CONFLICT (execution_profile) DO UPDATE
           SET inventory_revision = EXCLUDED.inventory_revision,
               activated_at = EXCLUDED.activated_at`,
		candidate.ExecutionProfile, candidate.InventoryRevision, candidate.ActivatedAt); err != nil {
		return false, fmt.Errorf("activate connector inventory: %w", err)
	}
	attributes, err := json.Marshal(map[string]any{
		"oldInventoryRevision": event.OldRevision,
		"newInventoryRevision": event.NewRevision,
		"descriptorCount":      event.DescriptorCount,
	})
	if err != nil {
		return false, fmt.Errorf("encode catalog audit attributes: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO astrasync_security_audit_events
            (event_id, event_type, actor_id, request_id, outcome, attributes, occurred_at)
         VALUES ($1, 'connector.inventory.activate', $2, $3, $4, $5::jsonb, $6)`,
		event.EventID, event.ActorID, event.RequestID, event.Outcome, attributes, event.OccurredAt); err != nil {
		return false, fmt.Errorf("write catalog activation audit: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit catalog activation: %w", err)
	}
	return changed, nil
}

func (r *Repository) Current(ctx context.Context, executionProfile string) (catalog.Snapshot, error) {
	return scanSnapshot(r.db.QueryRowContext(ctx,
		`SELECT i.inventory_revision, i.compiler_revision, i.execution_profile, i.payload, a.activated_at
           FROM astrasync_connector_inventory_activation a
           JOIN astrasync_connector_inventories i ON i.inventory_revision = a.inventory_revision
          WHERE a.execution_profile = $1`, executionProfile), r.db, ctx)
}

func (r *Repository) ListRecent(
	ctx context.Context, executionProfile string, limit int,
) ([]catalog.Snapshot, error) {
	if limit <= 0 || limit > 100 {
		return nil, fmt.Errorf("catalog retention limit must be between 1 and 100")
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT inventory_revision, compiler_revision, execution_profile, payload, activated_at
           FROM astrasync_connector_inventories
          WHERE execution_profile = $1
          ORDER BY activated_at DESC, inventory_revision
          LIMIT $2`, executionProfile, limit)
	if err != nil {
		return nil, fmt.Errorf("list retained connector inventories: %w", err)
	}
	defer rows.Close()
	result := make([]catalog.Snapshot, 0)
	for rows.Next() {
		snapshot, scanErr := scanSnapshot(rows, r.db, ctx)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate retained connector inventories: %w", err)
	}
	return result, nil
}

func insertDescriptor(ctx context.Context, tx *sql.Tx, descriptor catalog.DescriptorSnapshot, createdAt any) error {
	result, err := tx.ExecContext(ctx,
		`INSERT INTO astrasync_connector_descriptor_snapshots
            (descriptor_revision, connector_name, artifact_version, payload, created_at)
         VALUES ($1, $2, $3, $4, $5)
         ON CONFLICT (descriptor_revision) DO NOTHING`,
		descriptor.Revision, descriptor.Name, descriptor.ArtifactVersion, descriptor.Payload, createdAt)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return catalog.ErrRevisionCollision
		}
		return fmt.Errorf("insert connector descriptor snapshot: %w", err)
	}
	inserted, _ := result.RowsAffected()
	if inserted > 0 {
		return nil
	}
	var payload []byte
	if err := tx.QueryRowContext(ctx,
		`SELECT payload FROM astrasync_connector_descriptor_snapshots WHERE descriptor_revision = $1`,
		descriptor.Revision).Scan(&payload); err != nil {
		return fmt.Errorf("verify connector descriptor snapshot: %w", err)
	}
	if !bytes.Equal(payload, descriptor.Payload) {
		return catalog.ErrRevisionCollision
	}
	return nil
}

func insertInventory(ctx context.Context, tx *sql.Tx, candidate catalog.Snapshot) error {
	result, err := tx.ExecContext(ctx,
		`INSERT INTO astrasync_connector_inventories
            (inventory_revision, compiler_revision, execution_profile, payload, activated_at)
         VALUES ($1, $2, $3, $4, $5)
         ON CONFLICT (inventory_revision) DO NOTHING`,
		candidate.InventoryRevision, candidate.CompilerRevision, candidate.ExecutionProfile,
		candidate.Payload, candidate.ActivatedAt)
	if err != nil {
		return fmt.Errorf("insert connector inventory: %w", err)
	}
	inserted, _ := result.RowsAffected()
	if inserted > 0 {
		return nil
	}
	var payload []byte
	if err := tx.QueryRowContext(ctx,
		`SELECT payload FROM astrasync_connector_inventories WHERE inventory_revision = $1`,
		candidate.InventoryRevision).Scan(&payload); err != nil {
		return fmt.Errorf("verify connector inventory: %w", err)
	}
	if !bytes.Equal(payload, candidate.Payload) {
		return catalog.ErrRevisionCollision
	}
	return nil
}

type rowScanner interface {
	Scan(...any) error
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func scanSnapshot(source rowScanner, db queryer, ctx context.Context) (catalog.Snapshot, error) {
	var result catalog.Snapshot
	if err := source.Scan(
		&result.InventoryRevision,
		&result.CompilerRevision,
		&result.ExecutionProfile,
		&result.Payload,
		&result.ActivatedAt,
	); errors.Is(err, sql.ErrNoRows) {
		return catalog.Snapshot{}, catalog.ErrNotFound
	} else if err != nil {
		return catalog.Snapshot{}, fmt.Errorf("scan connector inventory: %w", err)
	}
	rows, err := db.QueryContext(ctx,
		`SELECT d.descriptor_revision, d.connector_name, d.artifact_version, d.payload
           FROM astrasync_connector_inventory_descriptors i
           JOIN astrasync_connector_descriptor_snapshots d ON d.descriptor_revision = i.descriptor_revision
          WHERE i.inventory_revision = $1
          ORDER BY i.position`, result.InventoryRevision)
	if err != nil {
		return catalog.Snapshot{}, fmt.Errorf("read connector inventory descriptors: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var descriptor catalog.DescriptorSnapshot
		if err := rows.Scan(&descriptor.Revision, &descriptor.Name, &descriptor.ArtifactVersion, &descriptor.Payload); err != nil {
			return catalog.Snapshot{}, fmt.Errorf("scan connector descriptor snapshot: %w", err)
		}
		result.Descriptors = append(result.Descriptors, descriptor)
	}
	if err := rows.Err(); err != nil {
		return catalog.Snapshot{}, fmt.Errorf("iterate connector descriptor snapshots: %w", err)
	}
	if err := result.Validate(); err != nil {
		return catalog.Snapshot{}, fmt.Errorf("validate stored connector inventory: %w", err)
	}
	return result, nil
}
