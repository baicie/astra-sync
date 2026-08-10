package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"io.astrasync/control-plane/connection"
)

const idempotencyRetention = 24 * time.Hour

//go:embed migrations/001_connections.sql
var migration string

//go:embed migrations/002_connection_test_executor.sql
var testExecutorMigration string

type Repository struct {
	db *sql.DB
}

func Open(ctx context.Context, dataSourceName string) (*Repository, error) {
	if dataSourceName == "" {
		return nil, fmt.Errorf("database URL must not be blank")
	}
	database, err := sql.Open("pgx", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("open Connection PostgreSQL: %w", err)
	}
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, fmt.Errorf("connect Connection PostgreSQL: %w", err)
	}
	return New(database), nil
}

func New(database *sql.DB) *Repository { return &Repository{db: database} }

func (r *Repository) Migrate(ctx context.Context) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Connection migration: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, migration); err != nil {
		return fmt.Errorf("migrate Connections: %w", err)
	}
	if _, err := tx.ExecContext(ctx, testExecutorMigration); err != nil {
		return fmt.Errorf("migrate Connection test executor: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Connection migration: %w", err)
	}
	return nil
}

func (r *Repository) Close() error { return r.db.Close() }

func (r *Repository) Ping(ctx context.Context) error { return r.db.PingContext(ctx) }

func (r *Repository) Get(
	ctx context.Context, tenantID, name string,
) (connection.Connection, error) {
	stored, err := scanConnection(r.db.QueryRowContext(ctx, connectionSelect+`
          WHERE c.tenant_id = $1::uuid AND c.name = $2`, tenantID, name))
	return classifyRead(stored, err)
}

func (r *Repository) GetByUID(
	ctx context.Context, tenantID, uid string,
) (connection.Connection, error) {
	stored, err := scanConnection(r.db.QueryRowContext(ctx, connectionSelect+`
          WHERE c.tenant_id = $1::uuid AND c.uid = $2::uuid`, tenantID, uid))
	return classifyRead(stored, err)
}

func (r *Repository) List(
	ctx context.Context, tenantID string, filter connection.ListFilter,
) (result connection.ListResult, resultErr error) {
	if filter.Limit <= 0 || filter.Limit > 101 {
		return connection.ListResult{}, fmt.Errorf("Connection list limit is invalid")
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return connection.ListResult{}, fmt.Errorf("begin Connection list snapshot: %w", err)
	}
	defer func() {
		if resultErr != nil {
			_ = tx.Rollback()
		}
	}()
	var revision int64
	if err := tx.QueryRowContext(ctx,
		`SELECT revision FROM astrasync_connection_list_revisions WHERE tenant_id = $1::uuid`, tenantID,
	).Scan(&revision); errors.Is(err, sql.ErrNoRows) {
		revision = 1
	} else if err != nil {
		return connection.ListResult{}, fmt.Errorf("read Connection list revision: %w", err)
	}
	rows, err := tx.QueryContext(ctx, connectionSelect+`
          WHERE c.tenant_id = $1::uuid
            AND ($2 = '' OR c.connector = $2)
            AND ($3 = '' OR c.state = $3)
            AND (c.name > $4 OR (c.name = $4 AND c.uid::text > $5))
          ORDER BY c.name, c.uid
          LIMIT $6`,
		tenantID, filter.Connector, string(filter.State), filter.AfterName, filter.AfterUID, filter.Limit+1)
	if err != nil {
		return connection.ListResult{}, fmt.Errorf("list Connections: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		stored, err := scanConnection(rows)
		if err != nil {
			return connection.ListResult{}, fmt.Errorf("scan listed Connection: %w", err)
		}
		result.Connections = append(result.Connections, stored)
	}
	if err := rows.Err(); err != nil {
		return connection.ListResult{}, fmt.Errorf("iterate listed Connections: %w", err)
	}
	result.HasMore = len(result.Connections) > filter.Limit
	if result.HasMore {
		result.Connections = result.Connections[:filter.Limit]
	}
	result.Revision = listRevision(tenantID, revision)
	if err := tx.Commit(); err != nil {
		return connection.ListResult{}, fmt.Errorf("commit Connection list snapshot: %w", err)
	}
	return result, nil
}

func (r *Repository) ReferenceCounts(
	ctx context.Context, uid string,
) (connection.ReferenceCounts, error) {
	var result connection.ReferenceCounts
	err := r.db.QueryRowContext(ctx,
		`SELECT
            (SELECT COUNT(*) FROM astrasync_job_connection_bindings WHERE connection_uid = $1::uuid),
            (SELECT COUNT(*) FROM astrasync_execution_connection_bindings WHERE connection_uid = $1::uuid),
            (SELECT COUNT(*) FROM astrasync_connection_tests
              WHERE connection_uid = $1::uuid AND state IN ('QUEUED', 'RUNNING')),
            (SELECT COUNT(*) FROM astrasync_connection_cleanup_obligations
              WHERE connection_uid = $1::uuid AND state = 'PENDING')`, uid,
	).Scan(&result.Jobs, &result.Executions, &result.Tests, &result.CleanupObligations)
	if err != nil {
		return connection.ReferenceCounts{}, fmt.Errorf("read Connection references: %w", err)
	}
	return result, nil
}

func (r *Repository) Apply(
	ctx context.Context, mutation connection.Mutation,
) (result connection.MutationResult, resultErr error) {
	if err := mutation.Identity.Validate(); err != nil {
		return connection.MutationResult{}, err
	}
	if len(mutation.AuditAttributes) > 32 {
		return connection.MutationResult{}, fmt.Errorf("Connection audit attributes exceed supported bounds")
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return connection.MutationResult{}, fmt.Errorf("begin Connection mutation: %w", err)
	}
	defer func() {
		if resultErr != nil {
			_ = tx.Rollback()
		}
	}()

	replayed, found, err := readIdempotency(ctx, tx, mutation)
	if err != nil {
		return connection.MutationResult{}, err
	}
	if found {
		if err := tx.Commit(); err != nil {
			return connection.MutationResult{}, fmt.Errorf("commit Connection idempotency replay: %w", err)
		}
		return replayed, nil
	}
	inserted, err := insertIdempotency(ctx, tx, mutation)
	if err != nil {
		return connection.MutationResult{}, err
	}
	if !inserted {
		replayed, found, err = readIdempotency(ctx, tx, mutation)
		if err != nil || !found {
			return connection.MutationResult{}, fmt.Errorf("resolve concurrent Connection idempotency: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return connection.MutationResult{}, err
		}
		return replayed, nil
	}

	result, err = applyMutation(ctx, tx, mutation)
	if err != nil {
		return connection.MutationResult{}, err
	}
	attributes, err := json.Marshal(mutation.AuditAttributes)
	if err != nil {
		return connection.MutationResult{}, fmt.Errorf("encode Connection audit attributes: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO astrasync_security_audit_events
            (event_id, event_type, actor_id, tenant_id, request_id, outcome, attributes, occurred_at)
         VALUES ($1, $2, $3, $4::uuid, $5, $6, $7::jsonb, $8)`,
		mutation.Identity.AuditEventID, auditAction(mutation.Kind), mutation.Identity.ActorID,
		mutation.TenantID, mutation.Identity.RequestID, result.Outcome, attributes,
		mutation.Identity.OccurredAt); err != nil {
		return connection.MutationResult{}, fmt.Errorf("write Connection mutation audit: %w", err)
	}
	safeProjection, err := json.Marshal(safeResult(result))
	if err != nil {
		return connection.MutationResult{}, fmt.Errorf("encode safe idempotency result: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE astrasync_connection_idempotency
            SET status = 'COMPLETE', result_kind = $1, result_projection = $2::jsonb,
                audit_event_id = $3, completed_at = $4
          WHERE tenant_id = $5::uuid AND actor_id = $6 AND method = $7 AND key_fingerprint = $8`,
		mutation.Kind, safeProjection, mutation.Identity.AuditEventID, mutation.Identity.OccurredAt,
		mutation.TenantID, mutation.Identity.ActorID, mutation.Identity.Method,
		mutation.Identity.KeyFingerprint); err != nil {
		return connection.MutationResult{}, fmt.Errorf("complete Connection idempotency result: %w", err)
	}
	if result.Outcome == connection.OutcomeChanged {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO astrasync_connection_list_revisions (tenant_id, revision)
             VALUES ($1::uuid, 2)
             ON CONFLICT (tenant_id) DO UPDATE SET revision = astrasync_connection_list_revisions.revision + 1`,
			mutation.TenantID); err != nil {
			return connection.MutationResult{}, fmt.Errorf("advance Connection list revision: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return connection.MutationResult{}, classifyPostgresError("commit Connection mutation", err)
	}
	return result, nil
}

func (r *Repository) GetTest(
	ctx context.Context, tenantID, operationID string,
) (connection.TestOperation, error) {
	result, err := scanTest(r.db.QueryRowContext(ctx, testSelect+`
          WHERE tenant_id = $1::uuid AND operation_id = $2::uuid`, tenantID, operationID))
	if errors.Is(err, sql.ErrNoRows) {
		return connection.TestOperation{}, connection.ErrTestNotFound
	}
	if err != nil {
		return connection.TestOperation{}, fmt.Errorf("get Connection test: %w", err)
	}
	return result, nil
}

func (r *Repository) LatestTest(
	ctx context.Context, connectionUID string,
) (connection.TestOperation, error) {
	result, err := scanTest(r.db.QueryRowContext(ctx, testSelect+`
          WHERE connection_uid = $1::uuid
          ORDER BY created_at DESC, operation_id DESC
          LIMIT 1`, connectionUID))
	if errors.Is(err, sql.ErrNoRows) {
		return connection.TestOperation{}, connection.ErrTestNotFound
	}
	if err != nil {
		return connection.TestOperation{}, fmt.Errorf("read latest Connection test: %w", err)
	}
	return result, nil
}

func (r *Repository) ClaimTests(
	ctx context.Context, executorID string, limit int, leaseDuration time.Duration, now time.Time,
) (_ []connection.TestWork, resultErr error) {
	if strings.TrimSpace(executorID) == "" || len(executorID) > connection.MaximumTestExecutorID ||
		limit <= 0 || limit > connection.MaximumTestClaimBatch || leaseDuration <= 0 ||
		leaseDuration > connection.MaximumTestLease {
		return nil, fmt.Errorf("Connection test claim is invalid")
	}
	now = now.UTC()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin Connection test claim: %w", err)
	}
	defer func() {
		if resultErr != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err := expireTestsTx(ctx, tx, now); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx,
		`WITH candidates AS (
		    SELECT operation_id
		      FROM astrasync_connection_tests
		     WHERE expires_at > $1 AND deadline_at > $1
		       AND (state = 'QUEUED'
		            OR (state = 'RUNNING' AND (lease_expires_at IS NULL OR lease_expires_at <= $1)))
		     ORDER BY created_at, operation_id
		     FOR UPDATE SKIP LOCKED
		     LIMIT $2
		)
		UPDATE astrasync_connection_tests test
		   SET state = 'RUNNING', phase = 'POLICY', result_code = NULL,
		       success = FALSE, remediation_key = '', started_at = COALESCE(started_at, $1),
		       completed_at = NULL, executor_id = $3, lease_expires_at = $4,
		       attempt = attempt + 1
		  FROM candidates
		 WHERE test.operation_id = candidates.operation_id
		RETURNING test.tenant_id::text, test.operation_id::text, test.connection_uid::text,
		          test.generation, test.descriptor_revision, test.actor_id,
		          test.egress_policy_revision, test.allowed_cidrs, test.state, COALESCE(test.phase, ''),
		          COALESCE(test.result_code, ''), test.success, test.remediation_key,
		          test.created_at, test.deadline_at, test.started_at, test.completed_at, test.expires_at,
		          test.executor_id, test.attempt, test.lease_expires_at`,
		now, limit, executorID, now.Add(leaseDuration))
	if err != nil {
		return nil, fmt.Errorf("claim Connection tests: %w", err)
	}
	defer rows.Close()
	type claimed struct {
		operation      connection.TestOperation
		executorID     string
		attempt        int32
		leaseExpiresAt time.Time
	}
	claimedTests := make([]claimed, 0, limit)
	for rows.Next() {
		var value claimed
		var allowedCIDRs []byte
		if err := rows.Scan(
			&value.operation.TenantID, &value.operation.OperationID, &value.operation.ConnectionUID,
			&value.operation.Generation, &value.operation.DescriptorRevision, &value.operation.ActorID,
			&value.operation.EgressPolicy.Revision, &allowedCIDRs, &value.operation.State,
			&value.operation.Phase, &value.operation.ResultCode, &value.operation.Success,
			&value.operation.RemediationKey, &value.operation.CreatedAt, &value.operation.DeadlineAt,
			&value.operation.StartedAt,
			&value.operation.CompletedAt, &value.operation.ExpiresAt, &value.executorID,
			&value.attempt, &value.leaseExpiresAt,
		); err != nil {
			return nil, fmt.Errorf("scan claimed Connection test: %w", err)
		}
		if err := json.Unmarshal(allowedCIDRs, &value.operation.EgressPolicy.AllowedCIDRs); err != nil {
			return nil, fmt.Errorf("decode claimed Connection test egress policy: %w", err)
		}
		if err := value.operation.Validate(); err != nil {
			return nil, fmt.Errorf("validate claimed Connection test: %w", err)
		}
		claimedTests = append(claimedTests, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claimed Connection tests: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close claimed Connection tests: %w", err)
	}
	result := make([]connection.TestWork, 0, len(claimedTests))
	for _, claimedTest := range claimedTests {
		generation, err := scanGenerationSnapshot(tx.QueryRowContext(ctx,
			`SELECT connection.tenant_id::text, tenant.namespace, connection.uid::text,
			        connection.connector, generation.generation, generation.descriptor_revision,
			        generation.connection_schema_revision, generation.settings,
			        COALESCE(generation.provider_kind, ''), generation.restricted_locator,
			        generation.created_at
			   FROM astrasync_connections connection
			   JOIN astrasync_auth_tenants tenant ON tenant.tenant_id = connection.tenant_id
			   JOIN astrasync_connection_generations generation
			     ON generation.connection_uid = connection.uid
			  WHERE connection.tenant_id = $1::uuid AND connection.uid = $2::uuid
			    AND generation.generation = $3`,
			claimedTest.operation.TenantID, claimedTest.operation.ConnectionUID,
			claimedTest.operation.Generation))
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("captured Connection test generation is unavailable")
		}
		if err != nil {
			return nil, fmt.Errorf("load captured Connection test generation: %w", err)
		}
		work := connection.TestWork{
			Operation: claimedTest.operation, Generation: generation,
			ExecutorID: claimedTest.executorID, Attempt: claimedTest.attempt,
			LeaseExpiresAt: claimedTest.leaseExpiresAt,
		}
		if err := work.Validate(); err != nil {
			return nil, err
		}
		result = append(result, work)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit Connection test claim: %w", err)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Operation.CreatedAt.Equal(result[right].Operation.CreatedAt) {
			return result[left].Operation.OperationID < result[right].Operation.OperationID
		}
		return result[left].Operation.CreatedAt.Before(result[right].Operation.CreatedAt)
	})
	return result, nil
}

func (r *Repository) CompleteTest(
	ctx context.Context,
	operationID, executorID string,
	completion connection.TestCompletion,
	now time.Time,
) (_ connection.TestOperation, resultErr error) {
	if _, err := uuid.Parse(operationID); err != nil || strings.TrimSpace(executorID) == "" ||
		len(executorID) > connection.MaximumTestExecutorID {
		return connection.TestOperation{}, fmt.Errorf("Connection test completion identity is invalid")
	}
	if err := completion.Validate(); err != nil {
		return connection.TestOperation{}, err
	}
	now = now.UTC()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return connection.TestOperation{}, fmt.Errorf("begin Connection test completion: %w", err)
	}
	defer func() {
		if resultErr != nil {
			_ = tx.Rollback()
		}
	}()
	completed, err := scanTest(tx.QueryRowContext(ctx,
		`UPDATE astrasync_connection_tests
		    SET state = $3, phase = $4, result_code = $5, success = $6,
		        remediation_key = $7, completed_at = $8, executor_id = NULL,
		        lease_expires_at = NULL
		  WHERE operation_id = $1::uuid AND executor_id = $2 AND state = 'RUNNING'
		    AND lease_expires_at > $8 AND deadline_at > $8
		RETURNING tenant_id::text, operation_id::text, connection_uid::text, generation,
		          descriptor_revision, actor_id, egress_policy_revision, allowed_cidrs,
		          state, COALESCE(phase, ''), COALESCE(result_code, ''), success,
		          remediation_key, created_at, deadline_at, started_at, completed_at, expires_at`,
		operationID, executorID, completion.State, completion.Phase, completion.ResultCode,
		completion.Success, completion.RemediationKey, now))
	if errors.Is(err, sql.ErrNoRows) {
		return connection.TestOperation{}, connection.ErrTestLeaseLost
	}
	if err != nil {
		return connection.TestOperation{}, fmt.Errorf("complete Connection test: %w", err)
	}
	attributes, err := json.Marshal(map[string]any{
		"operationId": completed.OperationID, "connectionUid": completed.ConnectionUID,
		"generation": completed.Generation, "resultCode": completed.ResultCode,
	})
	if err != nil {
		return connection.TestOperation{}, fmt.Errorf("encode Connection test completion audit: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO astrasync_security_audit_events
		    (event_id, event_type, actor_id, tenant_id, request_id, outcome, attributes, occurred_at)
		 VALUES ($1, 'connection.test.complete', $2, $3::uuid, $4, 'CHANGED', $5::jsonb, $6)`,
		"connection-test-complete:"+operationID, "service:"+executorID,
		completed.TenantID, "connection-test/"+operationID, attributes, now); err != nil {
		return connection.TestOperation{}, fmt.Errorf("write Connection test completion audit: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return connection.TestOperation{}, fmt.Errorf("commit Connection test completion: %w", err)
	}
	return completed, nil
}

func (r *Repository) ExpireTests(ctx context.Context, now time.Time) (int64, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return 0, fmt.Errorf("begin Connection test deadline reconciliation: %w", err)
	}
	defer tx.Rollback()
	count, err := expireTestsTx(ctx, tx, now.UTC())
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit Connection test deadline reconciliation: %w", err)
	}
	return count, nil
}

type testTransitionQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type testTransition struct {
	operationID   string
	tenantID      string
	connectionUID string
	generation    int64
}

func expireTestsTx(ctx context.Context, executor testTransitionQuerier, now time.Time) (int64, error) {
	timedOut, err := transitionTests(ctx, executor,
		`UPDATE astrasync_connection_tests
		    SET state = 'TIMED_OUT', phase = COALESCE(phase, 'POLICY'),
		        result_code = 'DEADLINE_EXCEEDED', success = FALSE,
		        remediation_key = 'connection.test.deadline',
		        started_at = COALESCE(started_at, created_at), completed_at = $1,
		        executor_id = NULL, lease_expires_at = NULL
		  WHERE deadline_at <= $1 AND expires_at > $1 AND state IN ('QUEUED', 'RUNNING')
		RETURNING operation_id::text, tenant_id::text, connection_uid::text, generation`, now)
	if err != nil {
		return 0, fmt.Errorf("time out Connection tests: %w", err)
	}
	expired, err := transitionTests(ctx, executor,
		`UPDATE astrasync_connection_tests
		    SET state = 'EXPIRED', phase = NULL, result_code = NULL, success = FALSE,
		        remediation_key = '', started_at = COALESCE(started_at, created_at),
		        completed_at = COALESCE(completed_at, $1),
		        executor_id = NULL, lease_expires_at = NULL
		  WHERE expires_at <= $1 AND state <> 'EXPIRED'
		RETURNING operation_id::text, tenant_id::text, connection_uid::text, generation`, now)
	if err != nil {
		return 0, fmt.Errorf("expire Connection tests: %w", err)
	}
	if err := writeTestTransitionAudits(ctx, executor, timedOut, "timeout", now); err != nil {
		return 0, err
	}
	if err := writeTestTransitionAudits(ctx, executor, expired, "expire", now); err != nil {
		return 0, err
	}
	return int64(len(timedOut) + len(expired)), nil
}

func transitionTests(
	ctx context.Context, executor testTransitionQuerier, query string, now time.Time,
) ([]testTransition, error) {
	rows, err := executor.QueryContext(ctx, query, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]testTransition, 0)
	for rows.Next() {
		var transition testTransition
		if err := rows.Scan(
			&transition.operationID, &transition.tenantID,
			&transition.connectionUID, &transition.generation,
		); err != nil {
			return nil, err
		}
		result = append(result, transition)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func writeTestTransitionAudits(
	ctx context.Context,
	executor testTransitionQuerier,
	transitions []testTransition,
	action string,
	now time.Time,
) error {
	for _, transition := range transitions {
		attributes, err := json.Marshal(map[string]any{
			"operationId": transition.operationID, "connectionUid": transition.connectionUID,
			"generation": transition.generation,
		})
		if err != nil {
			return fmt.Errorf("encode Connection test %s audit: %w", action, err)
		}
		if _, err := executor.ExecContext(ctx,
			`INSERT INTO astrasync_security_audit_events
			    (event_id, event_type, actor_id, tenant_id, request_id, outcome, attributes, occurred_at)
			 VALUES ($1, $2, 'service:connection-test-reconciler', $3::uuid, $4, 'CHANGED', $5::jsonb, $6)`,
			"connection-test-"+action+":"+transition.operationID,
			"connection.test."+action, transition.tenantID,
			"connection-test/"+transition.operationID, attributes, now,
		); err != nil {
			return fmt.Errorf("write Connection test %s audit: %w", action, err)
		}
	}
	return nil
}

func applyMutation(
	ctx context.Context, tx *sql.Tx, mutation connection.Mutation,
) (connection.MutationResult, error) {
	switch mutation.Kind {
	case connection.MutationCreate:
		return createConnection(ctx, tx, mutation)
	case connection.MutationUpdate, connection.MutationRotate, connection.MutationEnable, connection.MutationDisable:
		return updateConnection(ctx, tx, mutation)
	case connection.MutationDelete:
		return deleteConnection(ctx, tx, mutation)
	case connection.MutationTest:
		return createConnectionTest(ctx, tx, mutation)
	default:
		return connection.MutationResult{}, fmt.Errorf("unsupported Connection mutation %q", mutation.Kind)
	}
}

func createConnection(
	ctx context.Context, tx *sql.Tx, mutation connection.Mutation,
) (connection.MutationResult, error) {
	if mutation.Candidate == nil || mutation.Candidate.TenantID != mutation.TenantID ||
		mutation.Candidate.Name != mutation.Name {
		return connection.MutationResult{}, fmt.Errorf("create candidate identity is invalid")
	}
	candidate := mutation.Candidate.Clone()
	if err := candidate.Validate(); err != nil {
		return connection.MutationResult{}, err
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO astrasync_connections
            (tenant_id, name, uid, connector, version, current_generation, state,
             display_name, description, created_at, updated_at)
         VALUES ($1::uuid, $2, $3::uuid, $4, $5, $6, $7, $8, $9, $10, $11)`,
		candidate.TenantID, candidate.Name, candidate.UID, candidate.Connector, candidate.Version,
		candidate.Current.Number, candidate.State, candidate.DisplayName, candidate.Description,
		candidate.CreatedAt, candidate.UpdatedAt)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return connection.MutationResult{}, connection.ErrAlreadyExists
		}
		return connection.MutationResult{}, fmt.Errorf("create Connection: %w", err)
	}
	if err := insertGeneration(ctx, tx, candidate.UID, candidate.Current); err != nil {
		return connection.MutationResult{}, err
	}
	return connection.MutationResult{Connection: &candidate, Outcome: connection.OutcomeChanged}, nil
}

func updateConnection(
	ctx context.Context, tx *sql.Tx, mutation connection.Mutation,
) (connection.MutationResult, error) {
	current, err := scanConnection(tx.QueryRowContext(ctx, connectionSelect+`
          WHERE c.tenant_id = $1::uuid AND c.name = $2
          FOR UPDATE OF c`, mutation.TenantID, mutation.Name))
	if errors.Is(err, sql.ErrNoRows) {
		return connection.MutationResult{}, connection.ErrNotFound
	}
	if err != nil {
		return connection.MutationResult{}, fmt.Errorf("lock Connection: %w", err)
	}
	if mutation.ExpectedVersion <= 0 || current.Version != mutation.ExpectedVersion {
		return connection.MutationResult{}, connection.ErrConflict
	}
	if mutation.Candidate == nil {
		return connection.MutationResult{}, fmt.Errorf("Connection mutation candidate is required")
	}
	candidate := mutation.Candidate.Clone()
	if candidate.UID != current.UID || candidate.TenantID != current.TenantID ||
		candidate.Name != current.Name || candidate.Connector != current.Connector ||
		(candidate.Version != current.Version && candidate.Version != current.Version+1) ||
		(candidate.Current.Number != current.Current.Number && candidate.Current.Number != current.Current.Number+1) {
		return connection.MutationResult{}, connection.ErrConflict
	}
	if err := candidate.Validate(); err != nil {
		return connection.MutationResult{}, err
	}
	if candidate.Version == current.Version {
		return connection.MutationResult{Connection: &candidate, Outcome: connection.OutcomeNoChange}, nil
	}
	if candidate.Current.Number == current.Current.Number+1 {
		if err := insertGeneration(ctx, tx, candidate.UID, candidate.Current); err != nil {
			return connection.MutationResult{}, err
		}
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE astrasync_connections
            SET version = $1, current_generation = $2, state = $3,
                display_name = $4, description = $5, updated_at = $6
          WHERE tenant_id = $7::uuid AND name = $8 AND version = $9 AND uid = $10::uuid`,
		candidate.Version, candidate.Current.Number, candidate.State, candidate.DisplayName,
		candidate.Description, candidate.UpdatedAt, candidate.TenantID, candidate.Name,
		current.Version, candidate.UID)
	if err != nil {
		return connection.MutationResult{}, fmt.Errorf("update Connection: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return connection.MutationResult{}, connection.ErrConflict
	}
	return connection.MutationResult{Connection: &candidate, Outcome: connection.OutcomeChanged}, nil
}

func deleteConnection(
	ctx context.Context, tx *sql.Tx, mutation connection.Mutation,
) (connection.MutationResult, error) {
	current, err := scanConnection(tx.QueryRowContext(ctx, connectionSelect+`
          WHERE c.tenant_id = $1::uuid AND c.name = $2
          FOR UPDATE OF c`, mutation.TenantID, mutation.Name))
	if errors.Is(err, sql.ErrNoRows) {
		return connection.MutationResult{}, connection.ErrNotFound
	}
	if err != nil {
		return connection.MutationResult{}, fmt.Errorf("lock Connection for deletion: %w", err)
	}
	if current.Version != mutation.ExpectedVersion || mutation.ExpectedVersion <= 0 {
		return connection.MutationResult{}, connection.ErrConflict
	}
	if current.State != connection.StateDisabled {
		return connection.MutationResult{}, connection.ErrInUse
	}
	counts, err := referenceCountsTx(ctx, tx, current.UID)
	if err != nil {
		return connection.MutationResult{}, err
	}
	if counts.Jobs != 0 || counts.Executions != 0 || counts.Tests != 0 || counts.CleanupObligations != 0 {
		return connection.MutationResult{}, connection.ErrInUse
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO astrasync_connection_tombstones
            (tenant_id, name, uid, final_version, final_generation, deleted_at, expires_at)
         VALUES ($1::uuid, $2, $3::uuid, $4, $5, $6, $7)`,
		current.TenantID, current.Name, current.UID, current.Version, current.Current.Number,
		mutation.Identity.OccurredAt, mutation.Identity.OccurredAt.Add(idempotencyRetention)); err != nil {
		return connection.MutationResult{}, fmt.Errorf("write Connection tombstone: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM astrasync_connections WHERE tenant_id = $1::uuid AND name = $2 AND version = $3`,
		current.TenantID, current.Name, current.Version); err != nil {
		return connection.MutationResult{}, fmt.Errorf("delete Connection: %w", err)
	}
	tombstone := connection.Tombstone{
		TenantID: current.TenantID, Name: current.Name, UID: current.UID,
		DeletedAt: mutation.Identity.OccurredAt.UTC(),
	}
	return connection.MutationResult{Tombstone: &tombstone, Outcome: connection.OutcomeChanged}, nil
}

func createConnectionTest(
	ctx context.Context, tx *sql.Tx, mutation connection.Mutation,
) (connection.MutationResult, error) {
	current, err := scanConnection(tx.QueryRowContext(ctx, connectionSelect+`
          WHERE c.tenant_id = $1::uuid AND c.name = $2
          FOR UPDATE OF c`, mutation.TenantID, mutation.Name))
	if errors.Is(err, sql.ErrNoRows) {
		return connection.MutationResult{}, connection.ErrNotFound
	}
	if err != nil {
		return connection.MutationResult{}, fmt.Errorf("lock Connection for test: %w", err)
	}
	if current.Version != mutation.ExpectedVersion || mutation.ExpectedVersion <= 0 {
		return connection.MutationResult{}, connection.ErrConflict
	}
	if mutation.Test == nil {
		return connection.MutationResult{}, fmt.Errorf("Connection test operation is required")
	}
	test := *mutation.Test
	if err := test.Validate(); err != nil || test.ConnectionUID != current.UID ||
		test.Generation != current.Current.Number || test.ActorID != mutation.Identity.ActorID {
		return connection.MutationResult{}, fmt.Errorf("Connection test does not match current generation")
	}
	var tenantActive, actorActive, connectionActive, tenantDaily int
	if err := tx.QueryRowContext(ctx,
		`SELECT
		    COUNT(*) FILTER (WHERE state IN ('QUEUED', 'RUNNING')),
		    COUNT(*) FILTER (WHERE state IN ('QUEUED', 'RUNNING') AND actor_id = $2),
		    COUNT(*) FILTER (WHERE state IN ('QUEUED', 'RUNNING') AND connection_uid = $3::uuid),
		    COUNT(*) FILTER (WHERE created_at >= $4)
		   FROM astrasync_connection_tests
		  WHERE tenant_id = $1::uuid`,
		test.TenantID, test.ActorID, test.ConnectionUID, test.CreatedAt.Add(-24*time.Hour),
	).Scan(&tenantActive, &actorActive, &connectionActive, &tenantDaily); err != nil {
		return connection.MutationResult{}, fmt.Errorf("read Connection test admission counters: %w", err)
	}
	if tenantActive >= connection.MaximumTenantActiveTests ||
		actorActive >= connection.MaximumActorActiveTests ||
		connectionActive >= connection.MaximumConnectionActiveTest ||
		tenantDaily >= connection.MaximumTenantDailyTests {
		return connection.MutationResult{}, connection.ErrTestLimitExceeded
	}
	allowedCIDRs, err := json.Marshal(test.EgressPolicy.AllowedCIDRs)
	if err != nil {
		return connection.MutationResult{}, fmt.Errorf("encode Connection test egress policy: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO astrasync_connection_tests
		    (operation_id, tenant_id, connection_uid, generation, descriptor_revision, actor_id,
		     egress_policy_revision, allowed_cidrs,
		     state, phase, result_code, success, remediation_key, created_at,
		     deadline_at, started_at, completed_at, expires_at)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8::jsonb,
		         $9, NULL, NULL, FALSE, '', $10, $11, NULL, NULL, $12)`,
		test.OperationID, test.TenantID, test.ConnectionUID, test.Generation,
		test.DescriptorRevision, test.ActorID, test.EgressPolicy.Revision, allowedCIDRs,
		test.State, test.CreatedAt, test.DeadlineAt, test.ExpiresAt); err != nil {
		return connection.MutationResult{}, fmt.Errorf("queue Connection test: %w", err)
	}
	return connection.MutationResult{Test: &test, Outcome: connection.OutcomeChanged}, nil
}

func insertGeneration(ctx context.Context, tx *sql.Tx, uid string, generation connection.Generation) error {
	settings, err := json.Marshal(generation.Settings)
	if err != nil {
		return fmt.Errorf("encode Connection settings: %w", err)
	}
	var provider any
	var locator any
	if generation.SecretLocator.Provider != "" {
		provider = generation.SecretLocator.Provider
		locator, err = json.Marshal(generation.SecretLocator)
		if err != nil {
			return fmt.Errorf("encode restricted Secret locator: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO astrasync_connection_generations
            (connection_uid, generation, descriptor_revision, connection_schema_revision,
             settings, provider_kind, restricted_locator, created_at)
         VALUES ($1::uuid, $2, $3, $4, $5::jsonb, $6, $7::jsonb, $8)`,
		uid, generation.Number, generation.DescriptorRevision, generation.ConnectionSchemaRevision,
		settings, provider, locator, generation.CreatedAt); err != nil {
		return fmt.Errorf("insert immutable Connection generation: %w", err)
	}
	return nil
}

func readIdempotency(
	ctx context.Context, tx *sql.Tx, mutation connection.Mutation,
) (connection.MutationResult, bool, error) {
	var digest, statusValue string
	var projection []byte
	err := tx.QueryRowContext(ctx,
		`SELECT request_digest, status, COALESCE(result_projection, '{}'::jsonb)
           FROM astrasync_connection_idempotency
          WHERE tenant_id = $1::uuid AND actor_id = $2 AND method = $3 AND key_fingerprint = $4
          FOR UPDATE`, mutation.TenantID, mutation.Identity.ActorID, mutation.Identity.Method,
		mutation.Identity.KeyFingerprint).Scan(&digest, &statusValue, &projection)
	if errors.Is(err, sql.ErrNoRows) {
		return connection.MutationResult{}, false, nil
	}
	if err != nil {
		return connection.MutationResult{}, false, fmt.Errorf("read Connection idempotency: %w", err)
	}
	if digest != mutation.Identity.RequestDigest {
		return connection.MutationResult{}, false, connection.ErrIdempotencyReused
	}
	if statusValue != "COMPLETE" {
		return connection.MutationResult{}, false, fmt.Errorf("Connection idempotency operation is still in progress")
	}
	var result connection.MutationResult
	if err := json.Unmarshal(projection, &result); err != nil {
		return connection.MutationResult{}, false, fmt.Errorf("decode Connection idempotency result: %w", err)
	}
	result.Outcome = connection.OutcomeReplayed
	return result, true, nil
}

func insertIdempotency(ctx context.Context, tx *sql.Tx, mutation connection.Mutation) (bool, error) {
	result, err := tx.ExecContext(ctx,
		`INSERT INTO astrasync_connection_idempotency
            (tenant_id, actor_id, method, key_fingerprint, request_digest, status, created_at, expires_at)
         VALUES ($1::uuid, $2, $3, $4, $5, 'IN_PROGRESS', $6, $7)
         ON CONFLICT (tenant_id, actor_id, method, key_fingerprint) DO NOTHING`,
		mutation.TenantID, mutation.Identity.ActorID, mutation.Identity.Method,
		mutation.Identity.KeyFingerprint, mutation.Identity.RequestDigest,
		mutation.Identity.OccurredAt, mutation.Identity.OccurredAt.Add(idempotencyRetention))
	if err != nil {
		return false, fmt.Errorf("claim Connection idempotency key: %w", err)
	}
	rows, _ := result.RowsAffected()
	return rows == 1, nil
}

func referenceCountsTx(
	ctx context.Context, tx *sql.Tx, uid string,
) (connection.ReferenceCounts, error) {
	var result connection.ReferenceCounts
	if err := tx.QueryRowContext(ctx,
		`SELECT
            (SELECT COUNT(*) FROM astrasync_job_connection_bindings WHERE connection_uid = $1::uuid),
            (SELECT COUNT(*) FROM astrasync_execution_connection_bindings WHERE connection_uid = $1::uuid),
            (SELECT COUNT(*) FROM astrasync_connection_tests
              WHERE connection_uid = $1::uuid AND state IN ('QUEUED', 'RUNNING')),
            (SELECT COUNT(*) FROM astrasync_connection_cleanup_obligations
              WHERE connection_uid = $1::uuid AND state = 'PENDING')`, uid,
	).Scan(&result.Jobs, &result.Executions, &result.Tests, &result.CleanupObligations); err != nil {
		return connection.ReferenceCounts{}, fmt.Errorf("read Connection deletion references: %w", err)
	}
	return result, nil
}

const connectionSelect = `SELECT
        c.tenant_id::text, c.name, c.uid::text, c.connector, c.version, c.state,
        c.display_name, c.description, c.created_at, c.updated_at,
        g.generation, g.descriptor_revision, g.connection_schema_revision, g.settings,
        COALESCE(g.provider_kind, ''), g.restricted_locator, g.created_at
      FROM astrasync_connections c
      JOIN astrasync_connection_generations g
        ON g.connection_uid = c.uid AND g.generation = c.current_generation`

type scanner interface{ Scan(...any) error }

func scanConnection(source scanner) (connection.Connection, error) {
	var result connection.Connection
	var settings []byte
	var locator []byte
	var provider string
	err := source.Scan(
		&result.TenantID, &result.Name, &result.UID, &result.Connector, &result.Version, &result.State,
		&result.DisplayName, &result.Description, &result.CreatedAt, &result.UpdatedAt,
		&result.Current.Number, &result.Current.DescriptorRevision,
		&result.Current.ConnectionSchemaRevision, &settings, &provider, &locator,
		&result.Current.CreatedAt,
	)
	if err != nil {
		return connection.Connection{}, err
	}
	if err := json.Unmarshal(settings, &result.Current.Settings); err != nil {
		return connection.Connection{}, fmt.Errorf("decode Connection settings: %w", err)
	}
	if provider != "" {
		if len(locator) == 0 || string(locator) == "null" {
			return connection.Connection{}, fmt.Errorf("stored Connection locator is missing")
		}
		if err := json.Unmarshal(locator, &result.Current.SecretLocator); err != nil {
			return connection.Connection{}, fmt.Errorf("decode restricted Secret locator: %w", err)
		}
		if string(result.Current.SecretLocator.Provider) != provider {
			return connection.Connection{}, fmt.Errorf("stored Connection provider metadata mismatch")
		}
	}
	if err := result.Validate(); err != nil {
		return connection.Connection{}, fmt.Errorf("validate stored Connection: %w", err)
	}
	return result, nil
}

func scanGenerationSnapshot(source scanner) (connection.GenerationSnapshot, error) {
	var result connection.GenerationSnapshot
	var settings, locator []byte
	var provider string
	if err := source.Scan(
		&result.TenantID, &result.TenantNamespace, &result.ConnectionUID, &result.Connector,
		&result.Generation.Number, &result.Generation.DescriptorRevision,
		&result.Generation.ConnectionSchemaRevision, &settings, &provider, &locator,
		&result.Generation.CreatedAt,
	); err != nil {
		return connection.GenerationSnapshot{}, err
	}
	if err := json.Unmarshal(settings, &result.Generation.Settings); err != nil {
		return connection.GenerationSnapshot{}, fmt.Errorf("decode captured Connection test settings: %w", err)
	}
	if provider != "" {
		if len(locator) == 0 || string(locator) == "null" {
			return connection.GenerationSnapshot{}, fmt.Errorf("captured Connection test locator is missing")
		}
		if err := json.Unmarshal(locator, &result.Generation.SecretLocator); err != nil {
			return connection.GenerationSnapshot{}, fmt.Errorf("decode captured Connection test locator: %w", err)
		}
		if string(result.Generation.SecretLocator.Provider) != provider {
			return connection.GenerationSnapshot{}, fmt.Errorf("captured Connection test provider metadata mismatch")
		}
	}
	if err := result.Validate(); err != nil {
		return connection.GenerationSnapshot{}, fmt.Errorf("validate captured Connection test generation: %w", err)
	}
	return result, nil
}

const testSelect = `SELECT tenant_id::text, operation_id::text, connection_uid::text, generation,
		descriptor_revision, actor_id, egress_policy_revision, allowed_cidrs,
		state, COALESCE(phase, ''), COALESCE(result_code, ''), success,
		remediation_key, created_at, deadline_at, started_at, completed_at, expires_at
      FROM astrasync_connection_tests`

func scanTest(source scanner) (connection.TestOperation, error) {
	var result connection.TestOperation
	var allowedCIDRs []byte
	if err := source.Scan(
		&result.TenantID, &result.OperationID, &result.ConnectionUID, &result.Generation,
		&result.DescriptorRevision, &result.ActorID, &result.EgressPolicy.Revision, &allowedCIDRs,
		&result.State, &result.Phase, &result.ResultCode,
		&result.Success, &result.RemediationKey, &result.CreatedAt, &result.DeadlineAt, &result.StartedAt,
		&result.CompletedAt, &result.ExpiresAt,
	); err != nil {
		return connection.TestOperation{}, err
	}
	if err := json.Unmarshal(allowedCIDRs, &result.EgressPolicy.AllowedCIDRs); err != nil {
		return connection.TestOperation{}, fmt.Errorf("decode stored Connection test egress policy: %w", err)
	}
	if err := result.Validate(); err != nil {
		return connection.TestOperation{}, fmt.Errorf("validate stored Connection test: %w", err)
	}
	return result, nil
}

func classifyRead(stored connection.Connection, err error) (connection.Connection, error) {
	if errors.Is(err, sql.ErrNoRows) {
		return connection.Connection{}, connection.ErrNotFound
	}
	if err != nil {
		return connection.Connection{}, fmt.Errorf("read Connection: %w", err)
	}
	return stored, nil
}

func classifyPostgresError(action string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "40001" {
		return connection.ErrConflict
	}
	return fmt.Errorf("%s: %w", action, err)
}

func listRevision(tenantID string, revision int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", tenantID, revision)))
	return fmt.Sprintf("sha256:%x", sum)
}

func auditAction(kind connection.MutationKind) string {
	return map[connection.MutationKind]string{
		connection.MutationCreate: "connection.create", connection.MutationUpdate: "connection.update",
		connection.MutationRotate: "connection.rotate", connection.MutationEnable: "connection.enable",
		connection.MutationDisable: "connection.disable", connection.MutationDelete: "connection.delete",
		connection.MutationTest: "connection.test.request",
	}[kind]
}

func safeResult(source connection.MutationResult) connection.MutationResult {
	result := source
	if source.Connection != nil {
		value := source.Connection.Clone()
		settings := make([]connection.Setting, 0, len(value.Current.Settings))
		for _, setting := range value.Current.Settings {
			if setting.Sensitivity == connection.SensitivityPublic {
				settings = append(settings, setting)
			}
		}
		provider := value.Current.SecretLocator.Provider
		value.Current.Settings = settings
		value.Current.SecretLocator = connection.SecretLocator{Provider: provider}
		result.Connection = &value
	}
	return result
}

var _ connection.Repository = (*Repository)(nil)
