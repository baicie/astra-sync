package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"io.astrasync/control-plane/job"
)

const jobIdempotencyRetention = 24 * time.Hour

func (r *Repository) ReplayMutation(
	ctx context.Context, mutation job.Mutation,
) (job.MutationResult, bool, error) {
	if err := validateReplayMutation(mutation); err != nil {
		return job.MutationResult{}, false, err
	}
	return readMutationIdempotency(ctx, r.db, mutation, false)
}

func (r *Repository) ApplyMutation(
	ctx context.Context, mutation job.Mutation,
) (result job.MutationResult, resultErr error) {
	if err := mutation.Validate(); err != nil {
		return job.MutationResult{}, err
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return job.MutationResult{}, fmt.Errorf("begin Job mutation: %w", err)
	}
	defer func() {
		if resultErr != nil {
			_ = tx.Rollback()
		}
	}()

	replayed, found, err := readMutationIdempotency(ctx, tx, mutation, true)
	if err != nil {
		return job.MutationResult{}, err
	}
	if found {
		if err := tx.Commit(); err != nil {
			return job.MutationResult{}, fmt.Errorf("commit Job idempotency replay: %w", err)
		}
		return replayed, nil
	}
	if err := insertMutationIdempotency(ctx, tx, mutation); err != nil {
		return job.MutationResult{}, err
	}

	result, err = applyJobMutation(ctx, tx, mutation)
	if err != nil {
		return job.MutationResult{}, err
	}
	if err := writeJobMutationAudit(ctx, tx, mutation, result); err != nil {
		return job.MutationResult{}, err
	}
	if err := completeMutationIdempotency(ctx, tx, mutation, result); err != nil {
		return job.MutationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return job.MutationResult{}, classifyMutationPostgresError("commit Job mutation", err)
	}
	return cloneMutationResult(result), nil
}

func applyJobMutation(
	ctx context.Context, tx *sql.Tx, mutation job.Mutation,
) (job.MutationResult, error) {
	switch mutation.Kind {
	case job.MutationCreate:
		return createJobMutation(ctx, tx, mutation)
	case job.MutationUpdate:
		return updateJobMutation(ctx, tx, mutation)
	case job.MutationDelete:
		return deleteJobMutation(ctx, tx, mutation)
	case job.MutationStart:
		return startJobMutation(ctx, tx, mutation)
	case job.MutationStop:
		return stopJobMutation(ctx, tx, mutation)
	default:
		return job.MutationResult{}, fmt.Errorf("unsupported Job mutation %q", mutation.Kind)
	}
}

func createJobMutation(
	ctx context.Context, tx *sql.Tx, mutation job.Mutation,
) (job.MutationResult, error) {
	if err := verifyFenceSpec(*mutation.Spec, mutation.TenantID, *mutation.Validation); err != nil {
		return job.MutationResult{}, err
	}
	if err := lockValidatedConnections(ctx, tx, mutation.Validation.Bindings); err != nil {
		return job.MutationResult{}, err
	}
	created, err := job.New(mutation.Key, mutation.UID, *mutation.Spec, mutation.Identity.OccurredAt)
	if err != nil {
		return job.MutationResult{}, err
	}
	spec, statusDocument, err := documents(created)
	if err != nil {
		return job.MutationResult{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO astrasync_control_jobs
            (namespace, name, uid, version, spec, status, created_at, updated_at)
         VALUES ($1, $2, $3::uuid, 1, $4::jsonb, $5::jsonb, $6, $6)`,
		created.Key.Namespace, created.Key.Name, created.UID, spec, statusDocument,
		created.CreatedAt); err != nil {
		return job.MutationResult{}, classifyMutationPostgresError("create Job", err)
	}
	if err := replaceStableBindings(ctx, tx, created, mutation.TenantID, mutation.Validation.Bindings); err != nil {
		return job.MutationResult{}, err
	}
	return job.MutationResult{Job: &created, Outcome: job.MutationOutcomeChanged}, nil
}

func updateJobMutation(
	ctx context.Context, tx *sql.Tx, mutation job.Mutation,
) (job.MutationResult, error) {
	current, err := lockJob(ctx, tx, mutation.Key)
	if err != nil {
		return job.MutationResult{}, err
	}
	if current.Version != mutation.ExpectedVersion {
		return job.MutationResult{}, job.ErrConflict
	}
	if err := verifyFenceSpec(*mutation.Spec, mutation.TenantID, *mutation.Validation); err != nil {
		return job.MutationResult{}, err
	}
	if err := lockValidatedConnections(ctx, tx, mutation.Validation.Bindings); err != nil {
		return job.MutationResult{}, err
	}
	next, err := current.ReplaceSpec(*mutation.Spec, mutation.Identity.OccurredAt)
	if err != nil {
		return job.MutationResult{}, err
	}
	updated, err := updateLockedJob(ctx, tx, next, current.Version)
	if err != nil {
		return job.MutationResult{}, err
	}
	if err := replaceStableBindings(ctx, tx, updated, mutation.TenantID, mutation.Validation.Bindings); err != nil {
		return job.MutationResult{}, err
	}
	return job.MutationResult{Job: &updated, Outcome: job.MutationOutcomeChanged}, nil
}

func startJobMutation(
	ctx context.Context, tx *sql.Tx, mutation job.Mutation,
) (job.MutationResult, error) {
	current, err := lockJob(ctx, tx, mutation.Key)
	if err != nil {
		return job.MutationResult{}, err
	}
	next, changed, err := current.RequestStart(mutation.Identity.OccurredAt)
	if err != nil {
		return job.MutationResult{}, err
	}
	if !changed {
		return job.MutationResult{Job: &current, Outcome: job.MutationOutcomeNoChange}, nil
	}
	if current.Version != mutation.ExpectedVersion {
		return job.MutationResult{}, job.ErrConflict
	}
	if err := verifyFenceSpec(current.Spec, mutation.TenantID, *mutation.Validation); err != nil {
		return job.MutationResult{}, err
	}
	if err := verifyStableBindings(ctx, tx, current, mutation.TenantID, mutation.Validation.Bindings); err != nil {
		return job.MutationResult{}, err
	}
	if err := lockValidatedConnections(ctx, tx, mutation.Validation.Bindings); err != nil {
		return job.MutationResult{}, err
	}
	updated, err := updateLockedJob(ctx, tx, next, current.Version)
	if err != nil {
		return job.MutationResult{}, err
	}
	for _, binding := range sortedBindings(mutation.Validation.Bindings) {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO astrasync_execution_connection_bindings
                (job_uid, epoch, tenant_id, role, connection_uid, generation,
                 descriptor_revision, compiler_revision, created_at)
             VALUES ($1::uuid, $2, $3::uuid, $4, $5::uuid, $6, $7, $8, $9)`,
			updated.UID, updated.Status.Epoch, mutation.TenantID, binding.Role,
			binding.ConnectionUID, binding.Generation, binding.DescriptorRevision,
			mutation.Validation.CompilerRevision, mutation.Identity.OccurredAt); err != nil {
			return job.MutationResult{}, classifyMutationPostgresError("capture execution Connection binding", err)
		}
	}
	return job.MutationResult{Job: &updated, Outcome: job.MutationOutcomeChanged}, nil
}

func stopJobMutation(
	ctx context.Context, tx *sql.Tx, mutation job.Mutation,
) (job.MutationResult, error) {
	current, err := lockJob(ctx, tx, mutation.Key)
	if err != nil {
		return job.MutationResult{}, err
	}
	next, changed, err := current.RequestStop(mutation.Identity.OccurredAt)
	if err != nil {
		return job.MutationResult{}, err
	}
	if !changed {
		return job.MutationResult{Job: &current, Outcome: job.MutationOutcomeNoChange}, nil
	}
	if current.Version != mutation.ExpectedVersion {
		return job.MutationResult{}, job.ErrConflict
	}
	updated, err := updateLockedJob(ctx, tx, next, current.Version)
	if err != nil {
		return job.MutationResult{}, err
	}
	return job.MutationResult{Job: &updated, Outcome: job.MutationOutcomeChanged}, nil
}

func deleteJobMutation(
	ctx context.Context, tx *sql.Tx, mutation job.Mutation,
) (job.MutationResult, error) {
	current, err := lockJob(ctx, tx, mutation.Key)
	if err != nil {
		return job.MutationResult{}, err
	}
	if current.Version != mutation.ExpectedVersion {
		return job.MutationResult{}, job.ErrConflict
	}
	if err := current.Deletable(); err != nil {
		return job.MutationResult{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM astrasync_job_connection_bindings WHERE job_uid = $1::uuid`, current.UID); err != nil {
		return job.MutationResult{}, fmt.Errorf("delete stable Job Connection bindings: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO astrasync_job_tombstones
            (tenant_id, namespace, name, uid, final_version, deleted_at, expires_at)
         VALUES ($1::uuid, $2, $3, $4::uuid, $5, $6, $7)`,
		mutation.TenantID, current.Key.Namespace, current.Key.Name, current.UID,
		current.Version, mutation.Identity.OccurredAt,
		mutation.Identity.OccurredAt.Add(jobIdempotencyRetention)); err != nil {
		return job.MutationResult{}, fmt.Errorf("write Job tombstone: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM astrasync_control_jobs WHERE uid = $1::uuid AND version = $2`,
		current.UID, current.Version); err != nil {
		return job.MutationResult{}, fmt.Errorf("delete Job: %w", err)
	}
	tombstone := job.Tombstone{
		TenantID: mutation.TenantID, Key: current.Key, UID: current.UID,
		FinalVersion: current.Version, DeletedAt: mutation.Identity.OccurredAt.UTC(),
	}
	return job.MutationResult{Tombstone: &tombstone, Outcome: job.MutationOutcomeChanged}, nil
}

func lockJob(ctx context.Context, tx *sql.Tx, key job.Key) (job.Job, error) {
	stored, err := scanJob(tx.QueryRowContext(ctx,
		`SELECT namespace, name, uid::text, version, spec, status, created_at, updated_at
           FROM astrasync_control_jobs
          WHERE namespace = $1 AND name = $2
          FOR UPDATE`, key.Namespace, key.Name))
	if errors.Is(err, sql.ErrNoRows) {
		return job.Job{}, job.ErrNotFound
	}
	if err != nil {
		return job.Job{}, fmt.Errorf("lock Job: %w", err)
	}
	return stored, nil
}

func updateLockedJob(
	ctx context.Context, tx *sql.Tx, candidate job.Job, expectedVersion int64,
) (job.Job, error) {
	spec, statusDocument, err := documents(candidate)
	if err != nil {
		return job.Job{}, err
	}
	updated, err := scanJob(tx.QueryRowContext(ctx,
		`UPDATE astrasync_control_jobs
            SET spec = $1::jsonb, status = $2::jsonb, version = version + 1, updated_at = $3
          WHERE uid = $4::uuid AND version = $5
          RETURNING namespace, name, uid::text, version, spec, status, created_at, updated_at`,
		spec, statusDocument, candidate.UpdatedAt, candidate.UID, expectedVersion))
	if errors.Is(err, sql.ErrNoRows) {
		return job.Job{}, job.ErrConflict
	}
	if err != nil {
		return job.Job{}, fmt.Errorf("commit locked Job state: %w", err)
	}
	return updated, nil
}

func verifyFenceSpec(spec job.Spec, tenantID string, fence job.ValidationFence) error {
	expected := map[job.ConnectionRole]job.ConnectorSpec{
		job.ConnectionRoleSource: spec.Source,
		job.ConnectionRoleSink:   spec.Sink,
	}
	bindings := make(map[job.ConnectionRole]job.ConnectionBinding, len(fence.Bindings))
	for _, binding := range fence.Bindings {
		if binding.TenantID != tenantID {
			return job.ErrValidationStale
		}
		bindings[binding.Role] = binding
	}
	for role, connector := range expected {
		binding, found := bindings[role]
		if connector.ConnectionRef == "" {
			if found {
				return job.ErrValidationStale
			}
			continue
		}
		if !found || binding.ReferenceName != connector.ConnectionRef || binding.Connector != connector.Connector {
			return job.ErrValidationStale
		}
	}
	return nil
}

func lockValidatedConnections(
	ctx context.Context, tx *sql.Tx, bindings []job.ConnectionBinding,
) error {
	for _, binding := range sortedBindings(bindings) {
		var tenantID, name, uid, connector, state string
		var generation int64
		var schemaRevision string
		err := tx.QueryRowContext(ctx,
			`SELECT c.tenant_id::text, c.name, c.uid::text, c.connector, c.state,
                    c.current_generation, g.connection_schema_revision
               FROM astrasync_connections c
               JOIN astrasync_connection_generations g
                 ON g.connection_uid = c.uid AND g.generation = c.current_generation
              WHERE c.tenant_id = $1::uuid AND c.uid = $2::uuid
              FOR SHARE OF c`, binding.TenantID, binding.ConnectionUID).Scan(
			&tenantID, &name, &uid, &connector, &state, &generation, &schemaRevision)
		if errors.Is(err, sql.ErrNoRows) {
			return job.ErrValidationStale
		}
		if err != nil {
			return fmt.Errorf("lock validated Connection: %w", err)
		}
		if tenantID != binding.TenantID || name != binding.ReferenceName || uid != binding.ConnectionUID ||
			connector != binding.Connector || state != "ACTIVE" || generation != binding.Generation ||
			schemaRevision != binding.ConnectionSchemaRevision {
			return job.ErrValidationStale
		}
	}
	return nil
}

func replaceStableBindings(
	ctx context.Context, tx *sql.Tx, stored job.Job, tenantID string, bindings []job.ConnectionBinding,
) error {
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM astrasync_job_connection_bindings WHERE job_uid = $1::uuid`, stored.UID); err != nil {
		return fmt.Errorf("replace stable Job Connection bindings: %w", err)
	}
	for _, binding := range sortedBindings(bindings) {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO astrasync_job_connection_bindings
                (job_uid, tenant_id, role, connection_uid, connector, created_at)
             VALUES ($1::uuid, $2::uuid, $3, $4::uuid, $5, $6)`,
			stored.UID, tenantID, binding.Role, binding.ConnectionUID,
			binding.Connector, stored.UpdatedAt); err != nil {
			return classifyMutationPostgresError("write stable Job Connection binding", err)
		}
	}
	return nil
}

func verifyStableBindings(
	ctx context.Context, tx *sql.Tx, stored job.Job, tenantID string, expected []job.ConnectionBinding,
) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT tenant_id::text, role, connection_uid::text, connector
           FROM astrasync_job_connection_bindings
          WHERE job_uid = $1::uuid
          ORDER BY role
          FOR SHARE`, stored.UID)
	if err != nil {
		return fmt.Errorf("lock stable Job Connection bindings: %w", err)
	}
	defer rows.Close()
	actual := make(map[job.ConnectionRole]struct {
		tenantID, uid, connector string
	})
	for rows.Next() {
		var role job.ConnectionRole
		var value struct{ tenantID, uid, connector string }
		if err := rows.Scan(&value.tenantID, &role, &value.uid, &value.connector); err != nil {
			return fmt.Errorf("scan stable Job Connection binding: %w", err)
		}
		actual[role] = value
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate stable Job Connection bindings: %w", err)
	}
	if len(actual) != len(expected) {
		return job.ErrValidationStale
	}
	for _, binding := range expected {
		value, found := actual[binding.Role]
		if !found || value.tenantID != tenantID || value.uid != binding.ConnectionUID ||
			value.connector != binding.Connector {
			return job.ErrValidationStale
		}
	}
	return nil
}

func sortedBindings(source []job.ConnectionBinding) []job.ConnectionBinding {
	result := append([]job.ConnectionBinding(nil), source...)
	sort.Slice(result, func(left, right int) bool { return result[left].Role < result[right].Role })
	return result
}

type mutationQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readMutationIdempotency(
	ctx context.Context, queryer mutationQueryer, mutation job.Mutation, lock bool,
) (job.MutationResult, bool, error) {
	query := `SELECT request_digest, status, COALESCE(result_kind, ''),
                   COALESCE(result_job_uid::text, ''), COALESCE(result_name, ''),
                   COALESCE(result_version, 0), COALESCE(result_outcome, '')
              FROM astrasync_job_idempotency
             WHERE tenant_id = $1::uuid AND actor_id = $2 AND method = $3 AND key_fingerprint = $4`
	if lock {
		query += ` FOR UPDATE`
	}
	var digest, statusValue, kind, uid, name, outcome string
	var version int64
	err := queryer.QueryRowContext(ctx, query, mutation.TenantID, mutation.Identity.ActorID,
		mutation.Identity.Method, mutation.Identity.KeyFingerprint).Scan(
		&digest, &statusValue, &kind, &uid, &name, &version, &outcome)
	if errors.Is(err, sql.ErrNoRows) {
		return job.MutationResult{}, false, nil
	}
	if err != nil {
		return job.MutationResult{}, false, fmt.Errorf("read Job idempotency: %w", err)
	}
	if digest != mutation.Identity.RequestDigest {
		return job.MutationResult{}, false, job.ErrIdempotencyReused
	}
	if statusValue != "COMPLETE" {
		return job.MutationResult{}, false, job.ErrMutationInProgress
	}
	if kind != string(mutation.Kind) {
		return job.MutationResult{}, false, job.ErrIdempotencyReused
	}
	result := job.MutationResult{Outcome: job.MutationOutcomeReplayed}
	if mutation.Kind == job.MutationDelete {
		var tombstone job.Tombstone
		tombstone.TenantID = mutation.TenantID
		err := queryer.QueryRowContext(ctx,
			`SELECT namespace, name, uid::text, final_version, deleted_at
               FROM astrasync_job_tombstones
              WHERE tenant_id = $1::uuid AND uid = $2::uuid`, mutation.TenantID, uid).Scan(
			&tombstone.Key.Namespace, &tombstone.Key.Name, &tombstone.UID,
			&tombstone.FinalVersion, &tombstone.DeletedAt)
		if err != nil {
			return job.MutationResult{}, false, fmt.Errorf("read Job deletion replay tombstone: %w", err)
		}
		result.Tombstone = &tombstone
		return result, true, nil
	}
	stored, err := scanJob(queryer.QueryRowContext(ctx,
		`SELECT namespace, name, uid::text, version, spec, status, created_at, updated_at
           FROM astrasync_control_jobs WHERE uid = $1::uuid`, uid))
	if errors.Is(err, sql.ErrNoRows) {
		return job.MutationResult{}, false, job.ErrNotFound
	}
	if err != nil {
		return job.MutationResult{}, false, fmt.Errorf("read Job mutation replay result: %w", err)
	}
	if stored.Key.Name != name || stored.Version < version {
		return job.MutationResult{}, false, fmt.Errorf("Job idempotency result is inconsistent")
	}
	result.Job = &stored
	return result, true, nil
}

func insertMutationIdempotency(ctx context.Context, tx *sql.Tx, mutation job.Mutation) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO astrasync_job_idempotency
            (tenant_id, actor_id, method, key_fingerprint, request_digest, status, created_at, expires_at)
         VALUES ($1::uuid, $2, $3, $4, $5, 'IN_PROGRESS', $6, $7)`,
		mutation.TenantID, mutation.Identity.ActorID, mutation.Identity.Method,
		mutation.Identity.KeyFingerprint, mutation.Identity.RequestDigest,
		mutation.Identity.OccurredAt, mutation.Identity.OccurredAt.Add(jobIdempotencyRetention))
	if err != nil {
		return classifyMutationPostgresError("claim Job idempotency key", err)
	}
	return nil
}

func completeMutationIdempotency(
	ctx context.Context, tx *sql.Tx, mutation job.Mutation, result job.MutationResult,
) error {
	var uid, name any
	var version any
	if result.Job != nil {
		uid, name, version = result.Job.UID, result.Job.Key.Name, result.Job.Version
	} else if result.Tombstone != nil {
		uid, name, version = result.Tombstone.UID, result.Tombstone.Key.Name, result.Tombstone.FinalVersion
	}
	command, err := tx.ExecContext(ctx,
		`UPDATE astrasync_job_idempotency
            SET status = 'COMPLETE', result_kind = $1, result_job_uid = $2::uuid,
                result_name = $3, result_version = $4, result_outcome = $5,
                audit_event_id = $6, completed_at = $7
          WHERE tenant_id = $8::uuid AND actor_id = $9 AND method = $10 AND key_fingerprint = $11`,
		mutation.Kind, uid, name, version, result.Outcome, mutation.Identity.AuditEventID,
		mutation.Identity.OccurredAt, mutation.TenantID, mutation.Identity.ActorID,
		mutation.Identity.Method, mutation.Identity.KeyFingerprint)
	if err != nil {
		return fmt.Errorf("complete Job idempotency result: %w", err)
	}
	if changed, _ := command.RowsAffected(); changed != 1 {
		return fmt.Errorf("Job idempotency claim disappeared")
	}
	return nil
}

func writeJobMutationAudit(
	ctx context.Context, tx *sql.Tx, mutation job.Mutation, result job.MutationResult,
) error {
	attributes := make(map[string]any, len(mutation.AuditAttributes)+4)
	for key, value := range mutation.AuditAttributes {
		attributes[key] = value
	}
	attributes["namespace"] = mutation.Key.Namespace
	attributes["name"] = mutation.Key.Name
	attributes["beforeVersion"] = mutation.ExpectedVersion
	if result.Job != nil {
		attributes["afterVersion"] = result.Job.Version
		attributes["epoch"] = result.Job.Status.Epoch
	} else if result.Tombstone != nil {
		attributes["afterVersion"] = result.Tombstone.FinalVersion
	}
	document, err := json.Marshal(attributes)
	if err != nil {
		return fmt.Errorf("encode Job mutation audit attributes: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO astrasync_security_audit_events
            (event_id, event_type, actor_id, tenant_id, request_id, outcome, attributes, occurred_at)
         VALUES ($1, $2, $3, $4::uuid, $5, $6, $7::jsonb, $8)`,
		mutation.Identity.AuditEventID, jobAuditAction(mutation.Kind), mutation.Identity.ActorID,
		mutation.TenantID, mutation.Identity.RequestID, result.Outcome, document,
		mutation.Identity.OccurredAt); err != nil {
		return fmt.Errorf("write Job mutation audit: %w", err)
	}
	if mutation.Validation == nil || result.Outcome == job.MutationOutcomeNoChange {
		return nil
	}
	for _, binding := range sortedBindings(mutation.Validation.Bindings) {
		connectionAttributes, err := json.Marshal(map[string]any{
			"jobUid": result.Job.UID, "role": binding.Role,
			"connectionUid": binding.ConnectionUID, "generation": binding.Generation,
			"descriptorRevision": binding.DescriptorRevision, "epoch": result.Job.Status.Epoch,
		})
		if err != nil {
			return fmt.Errorf("encode Connection use audit attributes: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO astrasync_security_audit_events
                (event_id, event_type, actor_id, tenant_id, request_id, outcome, attributes, occurred_at)
             VALUES ($1, 'connection.use', $2, $3::uuid, $4, $5, $6::jsonb, $7)`,
			mutation.Identity.AuditEventID+"/"+string(binding.Role), mutation.Identity.ActorID,
			mutation.TenantID, mutation.Identity.RequestID, result.Outcome,
			connectionAttributes, mutation.Identity.OccurredAt); err != nil {
			return fmt.Errorf("write Connection use audit: %w", err)
		}
	}
	return nil
}

func jobAuditAction(kind job.MutationKind) string {
	return map[job.MutationKind]string{
		job.MutationCreate: "job.create", job.MutationUpdate: "job.update",
		job.MutationDelete: "job.delete", job.MutationStart: "job.start", job.MutationStop: "job.stop",
	}[kind]
}

func validateReplayMutation(mutation job.Mutation) error {
	if mutation.Kind != job.MutationCreate && mutation.Kind != job.MutationUpdate &&
		mutation.Kind != job.MutationDelete && mutation.Kind != job.MutationStart && mutation.Kind != job.MutationStop {
		return fmt.Errorf("unsupported Job mutation %q", mutation.Kind)
	}
	if err := mutation.Key.Validate(); err != nil {
		return err
	}
	if err := mutation.Identity.Validate(); err != nil {
		return err
	}
	return nil
}

func classifyMutationPostgresError(action string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			if postgresError.ConstraintName == "astrasync_control_jobs_pkey" ||
				postgresError.ConstraintName == "astrasync_control_jobs_uid_key" {
				return job.ErrAlreadyExists
			}
			if postgresError.ConstraintName == "astrasync_job_idempotency_pkey" {
				return job.ErrMutationInProgress
			}
		case "23503":
			return job.ErrValidationStale
		case "40001", "40P01":
			return job.ErrConflict
		}
	}
	return fmt.Errorf("%s: %w", action, err)
}

func cloneMutationResult(source job.MutationResult) job.MutationResult {
	result := source
	if source.Job != nil {
		copyJob := source.Job.Clone()
		result.Job = &copyJob
	}
	if source.Tombstone != nil {
		copyTombstone := *source.Tombstone
		result.Tombstone = &copyTombstone
	}
	return result
}
