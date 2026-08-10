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
	"io.astrasync/control-plane/catalog"
	catalogpostgres "io.astrasync/control-plane/catalog/postgres"
	"io.astrasync/control-plane/connection"
	connectionpostgres "io.astrasync/control-plane/connection/postgres"
	"io.astrasync/control-plane/job"
	jobpostgres "io.astrasync/control-plane/job/postgres"
	dispatchpostgres "io.astrasync/control-plane/scheduler/internal/dispatch/postgres"
	"io.astrasync/control-plane/scheduler/internal/materialization"
	materializationpostgres "io.astrasync/control-plane/scheduler/internal/materialization/postgres"
)

func TestStorePersistsLeaseFencedMaterializationReceiptsAndCleanup(t *testing.T) {
	dataSourceName := os.Getenv("ASTRASYNC_TEST_POSTGRES_URL")
	if dataSourceName == "" {
		t.Skip("ASTRASYNC_TEST_POSTGRES_URL is not configured")
	}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	tenantID := uuid.NewString()
	namespace := "materialization-" + tenantID[:8]
	profile := "materialization-" + tenantID
	issuer := "https://issuer.example/" + tenantID
	descriptorRevision := digest("descriptor-" + tenantID)
	inventoryRevision := digest("inventory-" + tenantID)
	compilerRevision := digest("compiler-" + tenantID)
	schemaRevision := digest("schema-" + tenantID)
	catalogEventID := "materialization-catalog-" + tenantID

	database, err := sql.Open("pgx", dataSourceName)
	if err != nil {
		t.Fatalf("open materialization database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close materialization database: %v", err)
		}
	})

	authRepository, err := authpostgres.Open(ctx, dataSourceName)
	if err != nil {
		t.Fatalf("open auth repository: %v", err)
	}
	defer authRepository.Close()
	if err := authRepository.Migrate(ctx); err != nil {
		t.Fatalf("migrate auth: %v", err)
	}
	if err := authRepository.BootstrapTenant(ctx, tenantID, namespace, "Materialization integration", auth.ExternalIdentity{
		Issuer: issuer, Subject: "admin",
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

	catalogRepository, err := catalogpostgres.Open(ctx, dataSourceName)
	if err != nil {
		t.Fatalf("open Catalog repository: %v", err)
	}
	defer catalogRepository.Close()
	if err := catalogRepository.Migrate(ctx); err != nil {
		t.Fatalf("migrate Catalog: %v", err)
	}
	dispatchStore := dispatchpostgres.New(database)
	if err := dispatchStore.Migrate(ctx); err != nil {
		t.Fatalf("migrate dispatches: %v", err)
	}
	t.Cleanup(func() {
		cleanupMaterializationFixture(
			t, database, tenantID, namespace, profile, inventoryRevision, descriptorRevision,
			catalogEventID, issuer)
	})
	snapshot := catalog.Snapshot{
		InventoryRevision: inventoryRevision, CompilerRevision: compilerRevision,
		ExecutionProfile: profile, Payload: []byte("inventory"), ActivatedAt: now,
		Descriptors: []catalog.DescriptorSnapshot{{
			Revision: descriptorRevision, Name: "jdbc", ArtifactVersion: "1.0.0", Payload: []byte("descriptor"),
		}},
	}
	if changed, err := catalogRepository.Activate(ctx, snapshot, catalog.AuditEvent{
		EventID: catalogEventID, ActorID: "service:catalog-reconciler",
		RequestID: "materialization-catalog-" + tenantID, NewRevision: snapshot.InventoryRevision,
		DescriptorCount: 1, Outcome: "CHANGED", OccurredAt: now,
	}); err != nil || !changed {
		t.Fatalf("activate materialization Catalog: changed=%v err=%v", changed, err)
	}

	connectionUID := uuid.NewString()
	providerUID := uuid.NewString()
	createdConnection, err := connection.New(
		tenantID, "orders-db", connectionUID, "jdbc", "Orders", "integration",
		connection.Generation{
			Number: 1, DescriptorRevision: descriptorRevision, ConnectionSchemaRevision: schemaRevision,
			Settings: []connection.Setting{{
				Key: "url", Value: "jdbc:postgresql://db.internal/orders", Sensitivity: connection.SensitivityRestricted,
			}},
			SecretLocator: connection.SecretLocator{
				Provider: connection.ProviderKubernetesSecretV1, SecretName: "orders-v1", SecretUID: providerUID,
				Fields: []connection.SecretFieldMapping{{LogicalField: "password", SecretKey: "password"}},
			},
			CreatedAt: now,
		}, now)
	if err != nil {
		t.Fatalf("construct Connection: %v", err)
	}
	if _, err := connectionRepository.Apply(ctx, connection.Mutation{
		Kind: connection.MutationCreate, TenantID: tenantID, Name: createdConnection.Name,
		Candidate: &createdConnection, Identity: connectionIdentity("create", now),
		AuditAttributes: map[string]any{"uid": connectionUID, "connector": "jdbc"},
	}); err != nil {
		t.Fatalf("create Connection: %v", err)
	}

	jobRecord, err := job.New(job.Key{Namespace: namespace, Name: "orders"}, uuid.NewString(), job.Spec{
		Source: job.ConnectorSpec{Connector: "jdbc", ConnectionRef: createdConnection.Name,
			Options: map[string]string{"table": "source_table"}},
		Sink:     job.ConnectorSpec{Connector: "jdbc", Options: map[string]string{"table": "sink_table"}},
		Delivery: job.DeliverySpec{Guarantee: job.DeliveryAtMostOnce},
		Runtime:  job.RuntimeSpec{MaxBatchRecords: 128},
	}, now)
	if err != nil {
		t.Fatalf("construct Job: %v", err)
	}
	jobRecord, err = jobRepository.Create(ctx, jobRecord)
	if err != nil {
		t.Fatalf("create Job: %v", err)
	}
	jobRecord, _, err = jobRecord.RequestStart(now.Add(time.Second))
	if err != nil {
		t.Fatalf("start Job: %v", err)
	}
	jobRecord, err = jobRepository.Update(ctx, jobRecord, 1)
	if err != nil {
		t.Fatalf("persist Job start: %v", err)
	}

	if _, err := database.ExecContext(ctx,
		`INSERT INTO astrasync_execution_connection_bindings
		    (job_uid, epoch, tenant_id, role, connection_uid, generation,
		     descriptor_revision, compiler_revision, created_at)
		 VALUES ($1::uuid, 1, $2::uuid, 'SOURCE', $3::uuid, 1, $4, $5, $6)`,
		jobRecord.UID, tenantID, connectionUID, descriptorRevision, compilerRevision, now); err != nil {
		t.Fatalf("insert execution binding: %v", err)
	}
	claimed, err := dispatchStore.Claim(ctx, "scheduler-materialization", 1, 10*time.Minute, time.Minute, now.Add(2*time.Second))
	if err != nil || len(claimed) != 1 || claimed[0].Identity.JobUID != jobRecord.UID {
		t.Fatalf("claim materialization dispatch: records=%+v err=%v", claimed, err)
	}
	record := claimed[0]
	store, err := materializationpostgres.New(database, profile, uuid.NewString)
	if err != nil {
		t.Fatalf("create materialization store: %v", err)
	}
	bindings, err := store.Load(ctx, record, now.Add(3*time.Second))
	if err != nil || len(bindings) != 1 || bindings[0].ConnectionUID != connectionUID ||
		bindings[0].CompilerRevision != compilerRevision || bindings[0].Locator.SecretUID != providerUID {
		t.Fatalf("load captured materialization binding: bindings=%+v err=%v", bindings, err)
	}
	if err := store.ClaimCleanup(ctx, record, bindings, now.Add(3*time.Second)); err != nil {
		t.Fatalf("claim cleanup obligation: %v", err)
	}
	receipt := materialization.Receipt{
		TenantID: tenantID, JobUID: jobRecord.UID, Epoch: 1, Role: materialization.RoleSource,
		ConnectionUID: connectionUID, Generation: 1, DescriptorRevision: descriptorRevision,
		Provider: materialization.ProviderReceipt{
			Kind: connection.ProviderKubernetesSecretV1, ObjectUID: providerUID, VersionToken: "provider-rv-7",
		},
		GeneratedSecretName: "job-credential-integration", GeneratedSecretUID: uuid.NewString(),
		GeneratedResourceVersion: "generated-rv-1", CreatedAt: now.Add(3 * time.Second),
	}
	if err := store.CommitReceipts(ctx, record, []materialization.Receipt{receipt}, now.Add(3*time.Second)); err != nil {
		t.Fatalf("commit materialization receipt: %v", err)
	}
	if err := store.CommitReceipts(ctx, record, []materialization.Receipt{receipt}, now.Add(4*time.Second)); err != nil {
		t.Fatalf("idempotently replay materialization receipt: %v", err)
	}
	loadedReceipts, err := store.Receipts(ctx, record.Identity)
	if err != nil || len(loadedReceipts) != 1 || loadedReceipts[0].Provider.ObjectUID != providerUID {
		t.Fatalf("read materialization receipt: receipts=%+v err=%v", loadedReceipts, err)
	}
	conflict := receipt
	conflict.Provider.VersionToken = "provider-rv-other"
	if err := store.CommitReceipts(ctx, record, []materialization.Receipt{conflict}, now.Add(5*time.Second)); !errors.Is(err, materialization.ErrReceiptConflict) {
		t.Fatalf("expected immutable receipt conflict, got %v", err)
	}
	if err := store.CompleteCleanup(ctx, record.Identity, now.Add(6*time.Second)); err != nil {
		t.Fatalf("complete materialization cleanup: %v", err)
	}
	var obligationState string
	var auditCount int
	if err := database.QueryRowContext(ctx,
		`SELECT state FROM astrasync_connection_cleanup_obligations
		  WHERE connection_uid = $1::uuid AND job_uid = $2::uuid AND epoch = 1`,
		connectionUID, jobRecord.UID).Scan(&obligationState); err != nil || obligationState != "COMPLETE" {
		t.Fatalf("cleanup obligation state: state=%q err=%v", obligationState, err)
	}
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM astrasync_security_audit_events
		  WHERE event_type = 'connection.materialize' AND tenant_id = $1::uuid`, tenantID,
	).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("materialization audit count: count=%d err=%v", auditCount, err)
	}
	if _, err := store.Load(ctx, record, record.LeaseExpiresAt.Add(time.Microsecond)); !errors.Is(err, materialization.ErrLeaseLost) {
		t.Fatalf("expired dispatch lease retained materialization authority: %v", err)
	}
}

func cleanupMaterializationFixture(
	t *testing.T,
	database *sql.DB,
	tenantID string,
	namespace string,
	profile string,
	inventoryRevision string,
	descriptorRevision string,
	catalogEventID string,
	issuer string,
) {
	t.Helper()
	statements := []struct {
		query string
		args  []any
	}{
		{`DELETE FROM astrasync_connection_materialization_receipts WHERE tenant_id = $1::uuid`, []any{tenantID}},
		{`DELETE FROM astrasync_connection_cleanup_obligations WHERE tenant_id = $1::uuid`, []any{tenantID}},
		{`DELETE FROM astrasync_connection_tests WHERE tenant_id = $1::uuid`, []any{tenantID}},
		{`DELETE FROM astrasync_execution_connection_bindings WHERE tenant_id = $1::uuid`, []any{tenantID}},
		{`DELETE FROM astrasync_job_connection_bindings WHERE tenant_id = $1::uuid`, []any{tenantID}},
		{`DELETE FROM astrasync_scheduler_dispatches WHERE namespace = $1`, []any{namespace}},
		{`DELETE FROM astrasync_control_jobs WHERE namespace = $1`, []any{namespace}},
		{`DELETE FROM astrasync_job_idempotency WHERE tenant_id = $1::uuid`, []any{tenantID}},
		{`DELETE FROM astrasync_job_tombstones WHERE tenant_id = $1::uuid`, []any{tenantID}},
		{`DELETE FROM astrasync_connection_idempotency WHERE tenant_id = $1::uuid`, []any{tenantID}},
		{`DELETE FROM astrasync_connection_tombstones WHERE tenant_id = $1::uuid`, []any{tenantID}},
		{`DELETE FROM astrasync_connection_list_revisions WHERE tenant_id = $1::uuid`, []any{tenantID}},
		{`DELETE FROM astrasync_connections WHERE tenant_id = $1::uuid`, []any{tenantID}},
		{`DELETE FROM astrasync_connector_inventory_activation WHERE execution_profile = $1`, []any{profile}},
		{`DELETE FROM astrasync_connector_inventory_descriptors WHERE inventory_revision = $1`, []any{inventoryRevision}},
		{`DELETE FROM astrasync_connector_inventories WHERE inventory_revision = $1`, []any{inventoryRevision}},
		{`DELETE FROM astrasync_connector_descriptor_snapshots WHERE descriptor_revision = $1`, []any{descriptorRevision}},
		{`DELETE FROM astrasync_security_audit_events WHERE tenant_id = $1::uuid OR event_id = $2`, []any{tenantID, catalogEventID}},
		{`DELETE FROM astrasync_auth_memberships WHERE tenant_id = $1::uuid`, []any{tenantID}},
		{`DELETE FROM astrasync_auth_tenants WHERE tenant_id = $1::uuid`, []any{tenantID}},
		{`DELETE FROM astrasync_auth_principals WHERE issuer = $1`, []any{issuer}},
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(context.Background(), statement.query, statement.args...); err != nil {
			t.Errorf("clean materialization integration fixture: %v", err)
		}
	}
}

func connectionIdentity(label string, occurredAt time.Time) connection.MutationIdentity {
	return connection.MutationIdentity{
		ActorID: "principal-materialization", Method: "/astra.control.v1.ConnectionService/" + label,
		KeyFingerprint: digest("key-" + label + uuid.NewString()),
		RequestDigest:  digest("request-" + label + uuid.NewString()),
		RequestID:      "request-" + label + "-" + uuid.NewString(), AuditEventID: uuid.NewString(),
		OccurredAt: occurredAt,
	}
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("sha256:%x", sum)
}
