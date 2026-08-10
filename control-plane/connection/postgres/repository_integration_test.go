package postgres_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"io.astrasync/control-plane/auth"
	authpostgres "io.astrasync/control-plane/auth/postgres"
	"io.astrasync/control-plane/connection"
	connectionpostgres "io.astrasync/control-plane/connection/postgres"
	"io.astrasync/control-plane/job"
	jobpostgres "io.astrasync/control-plane/job/postgres"
)

func TestRepositoryPersistsAtomicConnectionLifecycle(t *testing.T) {
	dataSourceName := os.Getenv("ASTRASYNC_TEST_POSTGRES_URL")
	if dataSourceName == "" {
		t.Skip("ASTRASYNC_TEST_POSTGRES_URL is not configured")
	}
	ctx := context.Background()
	jobRepository, err := jobpostgres.Open(ctx, dataSourceName)
	if err != nil {
		t.Fatalf("open Job repository: %v", err)
	}
	defer jobRepository.Close()
	if err := jobRepository.Migrate(ctx); err != nil {
		t.Fatalf("migrate Jobs: %v", err)
	}
	authRepository, err := authpostgres.Open(ctx, dataSourceName)
	if err != nil {
		t.Fatalf("open auth repository: %v", err)
	}
	defer authRepository.Close()
	if err := authRepository.Migrate(ctx); err != nil {
		t.Fatalf("migrate auth: %v", err)
	}
	tenantID := uuid.NewString()
	namespace := "tenant-" + tenantID[:8]
	if err := authRepository.BootstrapTenant(ctx, tenantID, namespace, "Connection integration", auth.ExternalIdentity{
		Issuer: "https://issuer.example/" + tenantID, Subject: "admin",
	}); err != nil {
		t.Fatalf("bootstrap tenant: %v", err)
	}
	repository, err := connectionpostgres.Open(ctx, dataSourceName)
	if err != nil {
		t.Fatalf("open Connection repository: %v", err)
	}
	defer repository.Close()
	if err := repository.Migrate(ctx); err != nil {
		t.Fatalf("migrate Connections: %v", err)
	}

	now := time.Now().UTC()
	created, err := connection.New(
		tenantID, "orders-db", uuid.NewString(), "mysql-cdc", "Orders", "integration",
		connection.Generation{
			Number: 1, DescriptorRevision: integrationDigest("descriptor-v1"),
			ConnectionSchemaRevision: integrationDigest("schema-v1"),
			Settings: []connection.Setting{
				{Key: "database", Value: "orders", Sensitivity: connection.SensitivityRestricted},
				{Key: "hostname", Value: "db.internal", Sensitivity: connection.SensitivityRestricted},
				{Key: "sslMode", Value: "required", Sensitivity: connection.SensitivityPublic},
			},
			SecretLocator: connection.SecretLocator{
				Provider: connection.ProviderKubernetesSecretV1, SecretName: "orders-v1", SecretUID: uuid.NewString(),
				Fields: []connection.SecretFieldMapping{
					{LogicalField: "password", SecretKey: "password"},
					{LogicalField: "username", SecretKey: "username"},
				},
			},
			CreatedAt: now,
		}, now,
	)
	if err != nil {
		t.Fatalf("construct Connection: %v", err)
	}
	createIdentity := integrationIdentity("create", now)
	createMutation := connection.Mutation{
		Kind: connection.MutationCreate, TenantID: tenantID, Name: created.Name,
		Candidate: &created, Identity: createIdentity,
		AuditAttributes: map[string]any{"uid": created.UID, "connector": created.Connector},
	}
	result, err := repository.Apply(ctx, createMutation)
	if err != nil || result.Outcome != connection.OutcomeChanged || result.Connection.UID != created.UID {
		t.Fatalf("create Connection: result=%+v err=%v", result, err)
	}
	replayed, err := repository.Apply(ctx, createMutation)
	if err != nil || replayed.Outcome != connection.OutcomeReplayed ||
		replayed.Connection.Current.SecretLocator.SecretName != "" ||
		len(replayed.Connection.Current.Settings) != 1 {
		t.Fatalf("safe idempotency replay: result=%+v err=%v", replayed, err)
	}
	mismatch := createMutation
	mismatch.Identity.RequestDigest = integrationDigest("another-request")
	if _, err := repository.Apply(ctx, mismatch); !errors.Is(err, connection.ErrIdempotencyReused) {
		t.Fatalf("expected idempotency mismatch, got %v", err)
	}

	active, _, err := created.SetState(connection.StateActive, connection.CompatibilityCompatible, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("enable domain transition: %v", err)
	}
	enableIdentity := integrationIdentity("enable", now.Add(time.Minute))
	if _, err := repository.Apply(ctx, connection.Mutation{
		Kind: connection.MutationEnable, TenantID: tenantID, Name: created.Name, ExpectedVersion: 1,
		Candidate: &active, Identity: enableIdentity,
		AuditAttributes: map[string]any{"uid": created.UID, "afterState": "ACTIVE"},
	}); err != nil {
		t.Fatalf("enable Connection: %v", err)
	}

	disabled, _, err := active.SetState(connection.StateDisabled, connection.CompatibilityCompatible, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("disable domain transition: %v", err)
	}
	failedAuditIdentity := integrationIdentity("failed-audit", now.Add(2*time.Minute))
	failedAuditIdentity.AuditEventID = enableIdentity.AuditEventID
	if _, err := repository.Apply(ctx, connection.Mutation{
		Kind: connection.MutationDisable, TenantID: tenantID, Name: created.Name, ExpectedVersion: 2,
		Candidate: &disabled, Identity: failedAuditIdentity,
		AuditAttributes: map[string]any{"uid": created.UID, "afterState": "DISABLED"},
	}); err == nil {
		t.Fatal("expected duplicate audit event to abort mutation")
	}
	stored, err := repository.Get(ctx, tenantID, created.Name)
	if err != nil || stored.Version != 2 || stored.State != connection.StateActive {
		t.Fatalf("audit failure did not roll back state: stored=%+v err=%v", stored, err)
	}
	if _, err := repository.Apply(ctx, connection.Mutation{
		Kind: connection.MutationDisable, TenantID: tenantID, Name: created.Name, ExpectedVersion: 2,
		Candidate: &disabled, Identity: integrationIdentity("disable", now.Add(3*time.Minute)),
		AuditAttributes: map[string]any{"uid": created.UID, "afterState": "DISABLED"},
	}); err != nil {
		t.Fatalf("disable Connection: %v", err)
	}

	jobRecord, err := job.New(
		job.Key{Namespace: namespace, Name: "orders"}, uuid.NewString(),
		job.Spec{
			Source:   job.ConnectorSpec{Connector: "mysql-cdc", ConnectionRef: created.Name},
			Sink:     job.ConnectorSpec{Connector: "jdbc", Options: map[string]string{"table": "orders"}},
			Delivery: job.DeliverySpec{Guarantee: job.DeliveryAtLeastOnce},
			Runtime:  job.RuntimeSpec{MaxBatchRecords: 128},
		}, now,
	)
	if err != nil {
		t.Fatalf("construct bound Job: %v", err)
	}
	if _, err := jobRepository.Create(ctx, jobRecord); err != nil {
		t.Fatalf("create bound Job: %v", err)
	}
	database, err := sql.Open("pgx", dataSourceName)
	if err != nil {
		t.Fatalf("open verification database: %v", err)
	}
	defer database.Close()
	if _, err := database.ExecContext(ctx,
		`INSERT INTO astrasync_job_connection_bindings
            (job_uid, tenant_id, role, connection_uid, connector, created_at)
         VALUES ($1::uuid, $2::uuid, 'SOURCE', $3::uuid, $4, $5)`,
		jobRecord.UID, tenantID, created.UID, created.Connector, now); err != nil {
		t.Fatalf("insert Job binding: %v", err)
	}
	deleteMutation := connection.Mutation{
		Kind: connection.MutationDelete, TenantID: tenantID, Name: created.Name, ExpectedVersion: 3,
		Identity:        integrationIdentity("delete-in-use", now.Add(4*time.Minute)),
		AuditAttributes: map[string]any{"uid": created.UID},
	}
	if _, err := repository.Apply(ctx, deleteMutation); !errors.Is(err, connection.ErrInUse) {
		t.Fatalf("expected reference-safe deletion rejection, got %v", err)
	}
	if err := jobRepository.Delete(ctx, jobRecord.Key, jobRecord.Version); err != nil {
		t.Fatalf("delete bound Job: %v", err)
	}
	deleteMutation.Identity = integrationIdentity("delete", now.Add(5*time.Minute))
	deleted, err := repository.Apply(ctx, deleteMutation)
	if err != nil || deleted.Tombstone == nil || deleted.Tombstone.UID != created.UID {
		t.Fatalf("delete Connection: result=%+v err=%v", deleted, err)
	}
	if _, err := repository.Get(ctx, tenantID, created.Name); !errors.Is(err, connection.ErrNotFound) {
		t.Fatalf("expected deleted Connection to be absent, got %v", err)
	}
}

func TestRepositoryFencesConcurrentConnectionTestExecutors(t *testing.T) {
	dataSourceName := os.Getenv("ASTRASYNC_TEST_POSTGRES_URL")
	if dataSourceName == "" {
		t.Skip("ASTRASYNC_TEST_POSTGRES_URL is not configured")
	}
	ctx := context.Background()
	authRepository, err := authpostgres.Open(ctx, dataSourceName)
	if err != nil {
		t.Fatalf("open auth repository: %v", err)
	}
	defer authRepository.Close()
	if err := authRepository.Migrate(ctx); err != nil {
		t.Fatalf("migrate auth: %v", err)
	}
	tenantID := uuid.NewString()
	namespace := "tenant-" + tenantID[:8]
	if err := authRepository.BootstrapTenant(ctx, tenantID, namespace, "Connection test executor", auth.ExternalIdentity{
		Issuer: "https://issuer.example/" + tenantID, Subject: "admin",
	}); err != nil {
		t.Fatalf("bootstrap tenant: %v", err)
	}
	repositoryA, err := connectionpostgres.Open(ctx, dataSourceName)
	if err != nil {
		t.Fatalf("open first Connection repository: %v", err)
	}
	defer repositoryA.Close()
	if err := repositoryA.Migrate(ctx); err != nil {
		t.Fatalf("migrate Connections: %v", err)
	}
	repositoryB, err := connectionpostgres.Open(ctx, dataSourceName)
	if err != nil {
		t.Fatalf("open second Connection repository: %v", err)
	}
	defer repositoryB.Close()

	now := time.Now().UTC().Truncate(time.Microsecond)
	created, err := connection.New(
		tenantID, "executor-test", uuid.NewString(), "postgres-cdc", "Executor test", "integration",
		connection.Generation{
			Number: 1, DescriptorRevision: integrationDigest("executor-descriptor-v1"),
			ConnectionSchemaRevision: integrationDigest("executor-schema-v1"),
			Settings: []connection.Setting{
				{Key: "database", Value: "executor_db", Sensitivity: connection.SensitivityRestricted},
				{Key: "hostname", Value: "captured.internal", Sensitivity: connection.SensitivityRestricted},
			},
			CreatedAt: now,
		}, now,
	)
	if err != nil {
		t.Fatalf("construct Connection: %v", err)
	}
	if _, err := repositoryA.Apply(ctx, connection.Mutation{
		Kind: connection.MutationCreate, TenantID: tenantID, Name: created.Name,
		Candidate: &created, Identity: integrationIdentity("executor-create", now),
		AuditAttributes: map[string]any{"uid": created.UID},
	}); err != nil {
		t.Fatalf("create Connection: %v", err)
	}
	operation := connection.TestOperation{
		TenantID: tenantID, OperationID: uuid.NewString(), ConnectionUID: created.UID,
		Generation: created.Current.Number, DescriptorRevision: created.Current.DescriptorRevision,
		ActorID: "principal-integration", EgressPolicy: connection.DefaultTestEgressPolicy(),
		State: connection.TestQueued, CreatedAt: now.Add(time.Second),
		DeadlineAt: now.Add(2 * time.Minute), ExpiresAt: now.Add(10 * time.Minute),
	}
	if _, err := repositoryA.Apply(ctx, connection.Mutation{
		Kind: connection.MutationTest, TenantID: tenantID, Name: created.Name,
		ExpectedVersion: created.Version, Test: &operation,
		Identity:        integrationIdentity("executor-queue", now.Add(time.Second)),
		AuditAttributes: map[string]any{"uid": created.UID, "operationId": operation.OperationID},
	}); err != nil {
		t.Fatalf("queue Connection test: %v", err)
	}
	updated, changed, err := created.Replace(
		created.DisplayName, created.Description,
		[]connection.Setting{
			{Key: "database", Value: "executor_db", Sensitivity: connection.SensitivityRestricted},
			{Key: "hostname", Value: "current.internal", Sensitivity: connection.SensitivityRestricted},
		},
		integrationDigest("executor-descriptor-v2"), integrationDigest("executor-schema-v2"),
		now.Add(2*time.Second),
	)
	if err != nil || !changed {
		t.Fatalf("replace Connection generation: changed=%v err=%v", changed, err)
	}
	if _, err := repositoryA.Apply(ctx, connection.Mutation{
		Kind: connection.MutationUpdate, TenantID: tenantID, Name: created.Name,
		ExpectedVersion: created.Version, Candidate: &updated,
		Identity:        integrationIdentity("executor-update", now.Add(2*time.Second)),
		AuditAttributes: map[string]any{"uid": created.UID},
	}); err != nil {
		t.Fatalf("persist replacement generation: %v", err)
	}

	type claimResult struct {
		work []connection.TestWork
		err  error
	}
	start := make(chan struct{})
	results := make(chan claimResult, 2)
	var group sync.WaitGroup
	for index, repository := range []*connectionpostgres.Repository{repositoryA, repositoryB} {
		group.Add(1)
		go func(executor int, candidate *connectionpostgres.Repository) {
			defer group.Done()
			<-start
			work, claimErr := candidate.ClaimTests(
				ctx, fmt.Sprintf("postgres-executor-%d", executor), 1, 10*time.Second, now.Add(3*time.Second),
			)
			results <- claimResult{work: work, err: claimErr}
		}(index, repository)
	}
	close(start)
	group.Wait()
	close(results)
	var claimed connection.TestWork
	claimCount := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent claim: %v", result.err)
		}
		claimCount += len(result.work)
		if len(result.work) == 1 {
			claimed = result.work[0]
		}
	}
	if claimCount != 1 {
		t.Fatalf("expected one PostgreSQL claim, got %d", claimCount)
	}
	if claimed.Generation.Generation.Number != 1 ||
		claimed.Generation.Generation.Settings[1].Value != "captured.internal" {
		t.Fatalf("claim did not load captured generation: %+v", claimed.Generation)
	}

	reclaimed, err := repositoryB.ClaimTests(
		ctx, "postgres-reclaimer", 1, 10*time.Second, now.Add(14*time.Second),
	)
	if err != nil || len(reclaimed) != 1 || reclaimed[0].Attempt != 2 {
		t.Fatalf("reclaim expired lease: work=%+v err=%v", reclaimed, err)
	}
	completion := connection.TestCompletion{
		State: connection.TestFailed, Phase: connection.TestPhaseAuthentication,
		ResultCode:     connection.TestResultAuthenticationFailed,
		RemediationKey: "connection.test.authentication",
	}
	if _, err := repositoryA.CompleteTest(
		ctx, operation.OperationID, claimed.ExecutorID, completion, now.Add(15*time.Second),
	); !errors.Is(err, connection.ErrTestLeaseLost) {
		t.Fatalf("stale PostgreSQL completion was accepted: %v", err)
	}
	completed, err := repositoryB.CompleteTest(
		ctx, operation.OperationID, "postgres-reclaimer", completion, now.Add(15*time.Second),
	)
	if err != nil || completed.State != connection.TestFailed || completed.Success {
		t.Fatalf("complete reclaimed PostgreSQL test: result=%+v err=%v", completed, err)
	}
	if _, err := repositoryA.GetTest(ctx, uuid.NewString(), operation.OperationID); !errors.Is(err, connection.ErrTestNotFound) {
		t.Fatalf("cross-tenant test read was not hidden: %v", err)
	}

	database, err := sql.Open("pgx", dataSourceName)
	if err != nil {
		t.Fatalf("open audit verification database: %v", err)
	}
	defer database.Close()
	var eventType, actorID, attributes string
	if err := database.QueryRowContext(ctx,
		`SELECT event_type, actor_id, attributes::text
		   FROM astrasync_security_audit_events
		  WHERE event_id = $1`, "connection-test-complete:"+operation.OperationID,
	).Scan(&eventType, &actorID, &attributes); err != nil {
		t.Fatalf("read completion audit: %v", err)
	}
	if eventType != "connection.test.complete" || actorID != "service:postgres-reclaimer" ||
		strings.Contains(attributes, "captured.internal") || strings.Contains(attributes, "executor_db") {
		t.Fatalf("completion audit is unsafe: type=%s actor=%s attributes=%s", eventType, actorID, attributes)
	}

	if count, err := repositoryA.ExpireTests(ctx, operation.ExpiresAt.Add(time.Second)); err != nil || count < 1 {
		t.Fatalf("expire retained PostgreSQL test: count=%d err=%v", count, err)
	}
	expired, err := repositoryA.GetTest(ctx, tenantID, operation.OperationID)
	if err != nil || expired.State != connection.TestExpired || expired.Phase != "" ||
		expired.ResultCode != "" || expired.RemediationKey != "" {
		t.Fatalf("PostgreSQL expiry did not scrub details: result=%+v err=%v", expired, err)
	}
}

func integrationIdentity(label string, occurredAt time.Time) connection.MutationIdentity {
	return connection.MutationIdentity{
		ActorID: "principal-integration", Method: "/astra.control.v1.ConnectionService/" + label,
		KeyFingerprint: integrationDigest("key-" + label), RequestDigest: integrationDigest("request-" + label),
		RequestID: "request-" + label, AuditEventID: uuid.NewString(), OccurredAt: occurredAt,
	}
}

func integrationDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("sha256:%x", sum)
}
