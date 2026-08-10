package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"io.astrasync/control-plane/scheduler/internal/dispatch"
	"io.astrasync/control-plane/scheduler/internal/materialization"
)

type Store struct {
	database         *sql.DB
	executionProfile string
	uid              func() string
}

func New(database *sql.DB, executionProfile string, uid func() string) (*Store, error) {
	if database == nil || executionProfile == "" || uid == nil {
		return nil, fmt.Errorf("materialization store dependencies must not be nil or blank")
	}
	return &Store{database: database, executionProfile: executionProfile, uid: uid}, nil
}

func (s *Store) Load(
	ctx context.Context, record dispatch.Record, now time.Time,
) ([]materialization.Binding, error) {
	if err := verifyLease(ctx, s.database, record, now); err != nil {
		return nil, err
	}
	var activeCompilerRevision string
	if err := s.database.QueryRowContext(ctx,
		`SELECT inventory.compiler_revision
           FROM astrasync_connector_inventory_activation activation
           JOIN astrasync_connector_inventories inventory
             ON inventory.inventory_revision = activation.inventory_revision
          WHERE activation.execution_profile = $1`, s.executionProfile).Scan(&activeCompilerRevision); err != nil {
		return nil, fmt.Errorf("read active compiler revision: %w", err)
	}
	rows, err := s.database.QueryContext(ctx,
		`SELECT binding.tenant_id::text, tenant.namespace, binding.role,
                binding.job_uid::text, binding.epoch, binding.connection_uid::text,
                binding.generation, connection.connector, binding.descriptor_revision,
                binding.compiler_revision, generation.connection_schema_revision,
                generation.settings, COALESCE(generation.provider_kind, ''),
                generation.restricted_locator,
                EXISTS (
                    SELECT 1
                      FROM astrasync_connector_inventory_activation active
                      JOIN astrasync_connector_inventory_descriptors descriptor
                        ON descriptor.inventory_revision = active.inventory_revision
                     WHERE active.execution_profile = $3
                       AND descriptor.descriptor_revision = binding.descriptor_revision
                )
           FROM astrasync_execution_connection_bindings binding
           JOIN astrasync_auth_tenants tenant ON tenant.tenant_id = binding.tenant_id
           JOIN astrasync_connections connection ON connection.uid = binding.connection_uid
           JOIN astrasync_connection_generations generation
             ON generation.connection_uid = binding.connection_uid
            AND generation.generation = binding.generation
          WHERE binding.job_uid = $1::uuid AND binding.epoch = $2
          ORDER BY binding.role`, record.Identity.JobUID, record.Identity.Epoch, s.executionProfile)
	if err != nil {
		return nil, fmt.Errorf("load execution Connection bindings: %w", err)
	}
	defer rows.Close()
	result := make([]materialization.Binding, 0, 2)
	for rows.Next() {
		var binding materialization.Binding
		var role string
		var settingsDocument, locatorDocument []byte
		var providerKind string
		var descriptorActive bool
		if err := rows.Scan(
			&binding.TenantID, &binding.TenantNamespace, &role,
			&binding.JobUID, &binding.Epoch, &binding.ConnectionUID,
			&binding.Generation, &binding.Connector, &binding.DescriptorRevision,
			&binding.CompilerRevision, &binding.ConnectionSchemaRevision,
			&settingsDocument, &providerKind, &locatorDocument, &descriptorActive,
		); err != nil {
			return nil, fmt.Errorf("scan execution Connection binding: %w", err)
		}
		binding.Role = materialization.Role(role)
		if binding.CompilerRevision != activeCompilerRevision || !descriptorActive {
			return nil, materialization.ErrRevisionMismatch
		}
		if err := json.Unmarshal(settingsDocument, &binding.Settings); err != nil {
			return nil, fmt.Errorf("decode Connection generation settings: %w", err)
		}
		if providerKind != "" {
			if len(locatorDocument) == 0 {
				return nil, fmt.Errorf("Connection generation locator is missing")
			}
			if err := json.Unmarshal(locatorDocument, &binding.Locator); err != nil {
				return nil, fmt.Errorf("decode restricted Connection locator: %w", err)
			}
			if string(binding.Locator.Provider) != providerKind {
				return nil, fmt.Errorf("Connection provider kind does not match its locator")
			}
		}
		if err := binding.Validate(); err != nil {
			return nil, fmt.Errorf("validate execution Connection binding: %w", err)
		}
		result = append(result, binding)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate execution Connection bindings: %w", err)
	}
	return result, nil
}

func (s *Store) ClaimCleanup(
	ctx context.Context, record dispatch.Record, bindings []materialization.Binding, now time.Time,
) (resultErr error) {
	tx, err := s.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin materialization cleanup claim: %w", err)
	}
	defer func() {
		if resultErr != nil {
			_ = tx.Rollback()
		}
	}()
	if err := verifyLease(ctx, tx, record, now); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		if _, duplicate := seen[binding.ConnectionUID]; duplicate {
			continue
		}
		seen[binding.ConnectionUID] = struct{}{}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO astrasync_connection_cleanup_obligations
                (obligation_id, tenant_id, connection_uid, generation, job_uid, epoch,
                 state, created_at, updated_at)
             VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5::uuid, $6, 'PENDING', $7, $7)
             ON CONFLICT (connection_uid, job_uid, epoch) DO NOTHING`,
			s.uid(), binding.TenantID, binding.ConnectionUID, binding.Generation,
			binding.JobUID, binding.Epoch, now.UTC()); err != nil {
			return fmt.Errorf("claim credential cleanup obligation: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit credential cleanup claim: %w", err)
	}
	return nil
}

func (s *Store) Receipts(
	ctx context.Context, identity dispatch.Identity,
) ([]materialization.Receipt, error) {
	rows, err := s.database.QueryContext(ctx,
		`SELECT tenant_id::text, job_uid::text, epoch, role, connection_uid::text,
		        generation, descriptor_revision, provider_kind, provider_object_uid, provider_version_token,
                generated_secret_name, generated_secret_uid::text,
                generated_resource_version, created_at
           FROM astrasync_connection_materialization_receipts
          WHERE job_uid = $1::uuid AND epoch = $2
          ORDER BY role`, identity.JobUID, identity.Epoch)
	if err != nil {
		return nil, fmt.Errorf("read materialization receipts: %w", err)
	}
	defer rows.Close()
	result := make([]materialization.Receipt, 0, 2)
	for rows.Next() {
		var receipt materialization.Receipt
		if err := rows.Scan(
			&receipt.TenantID, &receipt.JobUID, &receipt.Epoch, &receipt.Role,
			&receipt.ConnectionUID, &receipt.Generation, &receipt.DescriptorRevision,
			&receipt.Provider.Kind, &receipt.Provider.ObjectUID, &receipt.Provider.VersionToken,
			&receipt.GeneratedSecretName, &receipt.GeneratedSecretUID,
			&receipt.GeneratedResourceVersion, &receipt.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan materialization receipt: %w", err)
		}
		result = append(result, receipt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate materialization receipts: %w", err)
	}
	return result, nil
}

func (s *Store) CommitReceipts(
	ctx context.Context, record dispatch.Record, receipts []materialization.Receipt, now time.Time,
) (resultErr error) {
	if len(receipts) == 0 || len(receipts) > 2 {
		return fmt.Errorf("materialization receipt count is invalid")
	}
	tx, err := s.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin materialization receipt commit: %w", err)
	}
	defer func() {
		if resultErr != nil {
			_ = tx.Rollback()
		}
	}()
	if err := verifyLease(ctx, tx, record, now); err != nil {
		return err
	}
	for _, receipt := range receipts {
		if err := receipt.Validate(); err != nil || receipt.JobUID != record.Identity.JobUID ||
			receipt.Epoch != record.Identity.Epoch {
			return fmt.Errorf("materialization receipt does not match the leased execution")
		}
		var bindingMatches bool
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS (
                SELECT 1 FROM astrasync_execution_connection_bindings
                 WHERE job_uid = $1::uuid AND epoch = $2 AND role = $3
                   AND tenant_id = $4::uuid AND connection_uid = $5::uuid
                   AND generation = $6 AND descriptor_revision = $7
             )`, receipt.JobUID, receipt.Epoch, receipt.Role, receipt.TenantID,
			receipt.ConnectionUID, receipt.Generation, receipt.DescriptorRevision).Scan(&bindingMatches); err != nil {
			return fmt.Errorf("verify receipt execution binding: %w", err)
		}
		if !bindingMatches {
			return materialization.ErrRevisionMismatch
		}
		var existing materialization.Receipt
		err := tx.QueryRowContext(ctx,
			`SELECT tenant_id::text, job_uid::text, epoch, role, connection_uid::text,
			        generation, descriptor_revision, provider_kind, provider_object_uid, provider_version_token,
                    generated_secret_name, generated_secret_uid::text,
                    generated_resource_version, created_at
               FROM astrasync_connection_materialization_receipts
              WHERE job_uid = $1::uuid AND epoch = $2 AND role = $3
              FOR UPDATE`, receipt.JobUID, receipt.Epoch, receipt.Role).Scan(
			&existing.TenantID, &existing.JobUID, &existing.Epoch, &existing.Role,
			&existing.ConnectionUID, &existing.Generation, &existing.DescriptorRevision,
			&existing.Provider.Kind, &existing.Provider.ObjectUID, &existing.Provider.VersionToken,
			&existing.GeneratedSecretName, &existing.GeneratedSecretUID,
			&existing.GeneratedResourceVersion, &existing.CreatedAt)
		if err == nil {
			if !sameReceipt(existing, receipt) {
				return materialization.ErrReceiptConflict
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("lock materialization receipt: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO astrasync_connection_materialization_receipts
                (tenant_id, job_uid, epoch, role, connection_uid, generation,
			    descriptor_revision, provider_kind, provider_object_uid, provider_version_token,
			     generated_secret_name, generated_secret_uid, generated_resource_version, created_at)
			 VALUES ($1::uuid, $2::uuid, $3, $4, $5::uuid, $6, $7, $8, $9, $10, $11, $12::uuid, $13, $14)`,
			receipt.TenantID, receipt.JobUID, receipt.Epoch, receipt.Role,
			receipt.ConnectionUID, receipt.Generation, receipt.DescriptorRevision,
			receipt.Provider.Kind, receipt.Provider.ObjectUID, receipt.Provider.VersionToken,
			receipt.GeneratedSecretName, receipt.GeneratedSecretUID,
			receipt.GeneratedResourceVersion, receipt.CreatedAt); err != nil {
			return fmt.Errorf("insert materialization receipt: %w", err)
		}
		attributes, err := json.Marshal(map[string]any{
			"jobUid": receipt.JobUID, "epoch": receipt.Epoch, "role": receipt.Role,
			"connectionUid": receipt.ConnectionUID, "generation": receipt.Generation,
			"descriptorRevision": receipt.DescriptorRevision, "providerKind": receipt.Provider.Kind,
		})
		if err != nil {
			return fmt.Errorf("encode materialization audit: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO astrasync_security_audit_events
                (event_id, event_type, actor_id, tenant_id, request_id, outcome, attributes, occurred_at)
             VALUES ($1, 'connection.materialize', 'service:scheduler', $2::uuid, $3,
                     'CHANGED', $4::jsonb, $5)`,
			s.uid(), receipt.TenantID, materializationRequestID(record), attributes, now.UTC()); err != nil {
			return fmt.Errorf("write materialization audit: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit materialization receipts: %w", err)
	}
	return nil
}

func (s *Store) CompleteCleanup(
	ctx context.Context, identity dispatch.Identity, now time.Time,
) error {
	if _, err := s.database.ExecContext(ctx,
		`UPDATE astrasync_connection_cleanup_obligations
            SET state = 'COMPLETE', updated_at = $3
          WHERE job_uid = $1::uuid AND epoch = $2 AND state = 'PENDING'`,
		identity.JobUID, identity.Epoch, now.UTC()); err != nil {
		return fmt.Errorf("complete credential cleanup obligations: %w", err)
	}
	return nil
}

type leaseQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func verifyLease(
	ctx context.Context, queryer leaseQueryer, record dispatch.Record, now time.Time,
) error {
	if err := record.Identity.Validate(); err != nil || record.OwnerID == "" {
		return materialization.ErrLeaseLost
	}
	var valid bool
	if err := queryer.QueryRowContext(ctx,
		`SELECT EXISTS (
            SELECT 1 FROM astrasync_scheduler_dispatches
             WHERE job_uid = $1::uuid AND execution_epoch = $2 AND owner_id = $3
               AND phase IN ('CLAIMED', 'STARTING', 'RUNNING')
               AND lease_expires_at > $4
         )`, record.Identity.JobUID, record.Identity.Epoch, record.OwnerID, now.UTC()).Scan(&valid); err != nil {
		return fmt.Errorf("verify materialization dispatch lease: %w", err)
	}
	if !valid {
		return materialization.ErrLeaseLost
	}
	return nil
}

func sameReceipt(left, right materialization.Receipt) bool {
	return left.TenantID == right.TenantID && left.JobUID == right.JobUID && left.Epoch == right.Epoch &&
		left.Role == right.Role && left.ConnectionUID == right.ConnectionUID &&
		left.Generation == right.Generation && left.DescriptorRevision == right.DescriptorRevision &&
		left.Provider.Kind == right.Provider.Kind && left.Provider.ObjectUID == right.Provider.ObjectUID &&
		left.Provider.VersionToken == right.Provider.VersionToken &&
		left.GeneratedSecretName == right.GeneratedSecretName &&
		left.GeneratedSecretUID == right.GeneratedSecretUID &&
		left.GeneratedResourceVersion == right.GeneratedResourceVersion
}

func materializationRequestID(record dispatch.Record) string {
	return fmt.Sprintf("scheduler/%s/%d/%s", record.Identity.JobUID, record.Identity.Epoch, record.OwnerID)
}
