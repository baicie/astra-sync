package postgres_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
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

func TestAtomicJobMutationsFenceConnectionGenerations(t *testing.T) {
	dataSourceName := os.Getenv("ASTRASYNC_TEST_POSTGRES_URL")
	if dataSourceName == "" {
		t.Skip("ASTRASYNC_TEST_POSTGRES_URL is not configured")
	}
	ctx := context.Background()
	now := time.Now().UTC()
	tenantID := uuid.NewString()
	namespace := "tenant-" + tenantID[:8]

	authRepository, err := authpostgres.Open(ctx, dataSourceName)
	if err != nil {
		t.Fatalf("open auth repository: %v", err)
	}
	defer authRepository.Close()
	if err := authRepository.Migrate(ctx); err != nil {
		t.Fatalf("migrate auth: %v", err)
	}
	if err := authRepository.BootstrapTenant(ctx, tenantID, namespace, "Job mutation integration", auth.ExternalIdentity{
		Issuer: "https://issuer.example/" + tenantID, Subject: "admin",
	}); err != nil {
		t.Fatalf("bootstrap tenant: %v", err)
	}

	jobRepository, err := jobpostgres.Open(ctx, dataSourceName)
	if err != nil {
		t.Fatalf("open Job repository: %v", err)
	}
	defer jobRepository.Close()
	if err := jobRepository.Migrate(ctx); err != nil {
		t.Fatalf("migrate Jobs: %v", err)
	}
	connectionRepository, err := connectionpostgres.Open(ctx, dataSourceName)
	if err != nil {
		t.Fatalf("open Connection repository: %v", err)
	}
	defer connectionRepository.Close()
	if err := connectionRepository.Migrate(ctx); err != nil {
		t.Fatalf("migrate Connections: %v", err)
	}
	if err := jobRepository.MigrateMutations(ctx); err != nil {
		t.Fatalf("migrate Job mutations: %v", err)
	}

	storedConnection := createActiveIntegrationConnection(t, ctx, connectionRepository, tenantID, now)
	spec := job.Spec{
		Source: job.ConnectorSpec{
			Connector: "mysql-cdc", ConnectionRef: storedConnection.Name,
			Options: map[string]string{"tables": "orders"},
		},
		Sink:     job.ConnectorSpec{Connector: "csv", Options: map[string]string{"path": "output.csv"}},
		Delivery: job.DeliverySpec{Guarantee: job.DeliveryAtMostOnce},
		Runtime:  job.RuntimeSpec{MaxBatchRecords: 128},
	}
	binding := mutationBinding(tenantID, storedConnection)
	create := job.Mutation{
		Kind: job.MutationCreate, TenantID: tenantID,
		Key: job.Key{Namespace: namespace, Name: "orders"}, UID: uuid.NewString(), Spec: &spec,
		Validation: mutationFence(binding), Identity: jobMutationIdentity("create", now),
		AuditAttributes: map[string]any{"validationId": "validation-create"},
	}
	created, err := jobRepository.ApplyMutation(ctx, create)
	if err != nil || created.Job == nil || created.Job.Version != 1 {
		t.Fatalf("create atomic Job: result=%+v err=%v", created, err)
	}
	replayed, found, err := jobRepository.ReplayMutation(ctx, create)
	if err != nil || !found || replayed.Job == nil || replayed.Outcome != job.MutationOutcomeReplayed {
		t.Fatalf("replay atomic Job create: result=%+v found=%v err=%v", replayed, found, err)
	}

	rotated, err := storedConnection.Rotate(
		connection.SecretLocator{
			Provider: connection.ProviderKubernetesSecretV1, SecretName: "orders-v2", SecretUID: uuid.NewString(),
			Fields: []connection.SecretFieldMapping{
				{LogicalField: "password", SecretKey: "password"},
				{LogicalField: "username", SecretKey: "username"},
			},
		}, mutationDigest("descriptor-v1"), mutationDigest("schema-v1"), now.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("rotate Connection domain state: %v", err)
	}
	if _, err := connectionRepository.Apply(ctx, connection.Mutation{
		Kind: connection.MutationRotate, TenantID: tenantID, Name: storedConnection.Name,
		ExpectedVersion: storedConnection.Version, Candidate: &rotated,
		Identity:        connectionMutationIdentity("rotate-v2", now.Add(time.Minute)),
		AuditAttributes: map[string]any{"uid": storedConnection.UID},
	}); err != nil {
		t.Fatalf("rotate Connection to generation 2: %v", err)
	}

	start := job.Mutation{
		Kind: job.MutationStart, TenantID: tenantID, Key: create.Key, ExpectedVersion: 1,
		Validation: mutationFence(binding), Identity: jobMutationIdentity("start", now.Add(2*time.Minute)),
		AuditAttributes: map[string]any{"validationId": "validation-start"},
	}
	if _, err := jobRepository.ApplyMutation(ctx, start); !errors.Is(err, job.ErrValidationStale) {
		t.Fatalf("expected stale generation fence, got %v", err)
	}
	binding = mutationBinding(tenantID, rotated)
	start.Validation = mutationFence(binding)
	started, err := jobRepository.ApplyMutation(ctx, start)
	if err != nil || started.Job == nil || started.Job.Status.Epoch != 1 || started.Job.Version != 2 {
		t.Fatalf("start with generation 2: result=%+v err=%v", started, err)
	}

	rotatedAgain, err := rotated.Rotate(
		connection.SecretLocator{
			Provider: connection.ProviderKubernetesSecretV1, SecretName: "orders-v3", SecretUID: uuid.NewString(),
			Fields: []connection.SecretFieldMapping{
				{LogicalField: "password", SecretKey: "password"},
				{LogicalField: "username", SecretKey: "username"},
			},
		}, mutationDigest("descriptor-v1"), mutationDigest("schema-v1"), now.Add(3*time.Minute),
	)
	if err != nil {
		t.Fatalf("rotate Connection domain state again: %v", err)
	}
	if _, err := connectionRepository.Apply(ctx, connection.Mutation{
		Kind: connection.MutationRotate, TenantID: tenantID, Name: rotated.Name,
		ExpectedVersion: rotated.Version, Candidate: &rotatedAgain,
		Identity:        connectionMutationIdentity("rotate-v3", now.Add(3*time.Minute)),
		AuditAttributes: map[string]any{"uid": rotated.UID},
	}); err != nil {
		t.Fatalf("rotate Connection to generation 3: %v", err)
	}

	startNoOp := start
	startNoOp.ExpectedVersion = 1
	startNoOp.Validation = mutationFence(mutationBinding(tenantID, rotatedAgain))
	startNoOp.Identity = jobMutationIdentity("start-no-op", now.Add(4*time.Minute))
	noOp, err := jobRepository.ApplyMutation(ctx, startNoOp)
	if err != nil || noOp.Job == nil || noOp.Job.Status.Epoch != 1 || noOp.Job.Version != 2 ||
		noOp.Outcome != job.MutationOutcomeNoChange {
		t.Fatalf("repeated Start allocated a new epoch: result=%+v err=%v", noOp, err)
	}

	database, err := sql.Open("pgx", dataSourceName)
	if err != nil {
		t.Fatalf("open verification database: %v", err)
	}
	defer database.Close()
	var capturedGeneration, bindingCount int64
	if err := database.QueryRowContext(ctx,
		`SELECT generation FROM astrasync_execution_connection_bindings
          WHERE job_uid = $1::uuid AND epoch = 1 AND role = 'SOURCE'`, created.Job.UID,
	).Scan(&capturedGeneration); err != nil || capturedGeneration != 2 {
		t.Fatalf("execution did not retain generation 2: generation=%d err=%v", capturedGeneration, err)
	}
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM astrasync_execution_connection_bindings WHERE job_uid = $1::uuid`, created.Job.UID,
	).Scan(&bindingCount); err != nil || bindingCount != 1 {
		t.Fatalf("unexpected execution binding count: count=%d err=%v", bindingCount, err)
	}

	stop := job.Mutation{
		Kind: job.MutationStop, TenantID: tenantID, Key: create.Key, ExpectedVersion: 2,
		Identity: jobMutationIdentity("stop", now.Add(5*time.Minute)),
	}
	stopping, err := jobRepository.ApplyMutation(ctx, stop)
	if err != nil || stopping.Job == nil || stopping.Job.Status.State != job.StateCanceling {
		t.Fatalf("stop Job: result=%+v err=%v", stopping, err)
	}
	canceled, _, err := stopping.Job.Advance(1, job.StateCanceled, nil, now.Add(6*time.Minute))
	if err != nil {
		t.Fatalf("advance canceled: %v", err)
	}
	canceled, err = jobRepository.Update(ctx, canceled, stopping.Job.Version)
	if err != nil {
		t.Fatalf("persist canceled state: %v", err)
	}
	deleteMutation := job.Mutation{
		Kind: job.MutationDelete, TenantID: tenantID, Key: create.Key, ExpectedVersion: canceled.Version,
		Identity: jobMutationIdentity("delete", now.Add(7*time.Minute)),
	}
	deleted, err := jobRepository.ApplyMutation(ctx, deleteMutation)
	if err != nil || deleted.Tombstone == nil || deleted.Tombstone.UID != created.Job.UID {
		t.Fatalf("delete Job with retained execution evidence: result=%+v err=%v", deleted, err)
	}
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM astrasync_execution_connection_bindings WHERE job_uid = $1::uuid`, created.Job.UID,
	).Scan(&bindingCount); err != nil || bindingCount != 1 {
		t.Fatalf("delete removed execution evidence: count=%d err=%v", bindingCount, err)
	}
}

func createActiveIntegrationConnection(
	t *testing.T, ctx context.Context, repository *connectionpostgres.Repository, tenantID string, now time.Time,
) connection.Connection {
	t.Helper()
	created, err := connection.New(
		tenantID, "orders-db", uuid.NewString(), "mysql-cdc", "Orders", "integration",
		connection.Generation{
			Number: 1, DescriptorRevision: mutationDigest("descriptor-v1"),
			ConnectionSchemaRevision: mutationDigest("schema-v1"),
			Settings: []connection.Setting{
				{Key: "hostname", Value: "db.internal", Sensitivity: connection.SensitivityRestricted},
			},
			SecretLocator: connection.SecretLocator{
				Provider: connection.ProviderKubernetesSecretV1, SecretName: "orders-v1", SecretUID: uuid.NewString(),
				Fields: []connection.SecretFieldMapping{
					{LogicalField: "password", SecretKey: "password"},
					{LogicalField: "username", SecretKey: "username"},
				},
			}, CreatedAt: now,
		}, now,
	)
	if err != nil {
		t.Fatalf("construct Connection: %v", err)
	}
	if _, err := repository.Apply(ctx, connection.Mutation{
		Kind: connection.MutationCreate, TenantID: tenantID, Name: created.Name, Candidate: &created,
		Identity: connectionMutationIdentity("create", now), AuditAttributes: map[string]any{"uid": created.UID},
	}); err != nil {
		t.Fatalf("create Connection: %v", err)
	}
	active, _, err := created.SetState(connection.StateActive, connection.CompatibilityCompatible, now.Add(time.Second))
	if err != nil {
		t.Fatalf("enable Connection domain state: %v", err)
	}
	if _, err := repository.Apply(ctx, connection.Mutation{
		Kind: connection.MutationEnable, TenantID: tenantID, Name: created.Name,
		ExpectedVersion: created.Version, Candidate: &active,
		Identity:        connectionMutationIdentity("enable", now.Add(time.Second)),
		AuditAttributes: map[string]any{"uid": created.UID},
	}); err != nil {
		t.Fatalf("enable Connection: %v", err)
	}
	return active
}

func mutationBinding(tenantID string, value connection.Connection) job.ConnectionBinding {
	return job.ConnectionBinding{
		Role: job.ConnectionRoleSource, TenantID: tenantID, ReferenceName: value.Name,
		ConnectionUID: value.UID, Connector: value.Connector, Generation: value.Current.Number,
		DescriptorRevision:       mutationDigest("active-descriptor"),
		ConnectionSchemaRevision: value.Current.ConnectionSchemaRevision,
	}
}

func mutationFence(binding job.ConnectionBinding) *job.ValidationFence {
	return &job.ValidationFence{
		ValidationID: "validation-integration", SpecDigest: mutationDigest("spec"),
		CompilerRevision: mutationDigest("compiler"), Bindings: []job.ConnectionBinding{binding},
	}
}

func jobMutationIdentity(label string, occurredAt time.Time) job.MutationIdentity {
	return job.MutationIdentity{
		ActorID: "principal-integration", Method: "/astra.control.v1.JobService/" + label,
		KeyFingerprint: mutationDigest("key-" + label), RequestDigest: mutationDigest("request-" + label),
		RequestID: "request-" + label, AuditEventID: uuid.NewString(), OccurredAt: occurredAt,
	}
}

func connectionMutationIdentity(label string, occurredAt time.Time) connection.MutationIdentity {
	return connection.MutationIdentity{
		ActorID: "principal-integration", Method: "/astra.control.v1.ConnectionService/" + label,
		KeyFingerprint: mutationDigest("connection-key-" + label),
		RequestDigest:  mutationDigest("connection-request-" + label),
		RequestID:      "request-" + label, AuditEventID: uuid.NewString(), OccurredAt: occurredAt,
	}
}

func mutationDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("sha256:%x", digest)
}
