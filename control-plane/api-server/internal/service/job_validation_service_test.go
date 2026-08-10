package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	compilerv1 "io.astrasync/control-plane/api-server/gen/go/compiler/v1"
	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/api-server/internal/catalogproto"
	"io.astrasync/control-plane/api-server/internal/service"
	"io.astrasync/control-plane/auth"
	"io.astrasync/control-plane/catalog"
	catalogmemory "io.astrasync/control-plane/catalog/memory"
	"io.astrasync/control-plane/connection"
	connectionmemory "io.astrasync/control-plane/connection/memory"
	"io.astrasync/control-plane/job"
	jobmemory "io.astrasync/control-plane/job/memory"
)

type recordingCompiler struct {
	request *compilerv1.ValidateRequest
	result  *compilerv1.ValidateResponse
	err     error
}

func (c *recordingCompiler) Validate(
	_ context.Context, request *compilerv1.ValidateRequest,
) (*compilerv1.ValidateResponse, error) {
	c.request = proto.Clone(request).(*compilerv1.ValidateRequest)
	if c.err != nil {
		return nil, c.err
	}
	return proto.Clone(c.result).(*compilerv1.ValidateResponse), nil
}

type observingConnectionRepository struct {
	connection.Repository
	getCalls int
}

func (r *observingConnectionRepository) Get(
	ctx context.Context, tenantID, name string,
) (connection.Connection, error) {
	r.getCalls++
	return r.Repository.Get(ctx, tenantID, name)
}

func TestJobValidationPassesOnlyRedactedEffectiveConnectionMetadata(t *testing.T) {
	fixture := newJobValidationFixture(t, true)
	request := mysqlToCSVValidationRequest("orders-db")

	validation, err := fixture.service.ValidateForMutation(fixture.ctx, request)
	result := validation.Result
	if err != nil || !result.GetValid() {
		t.Fatalf("validate connected Job: result=%+v err=%v", result, err)
	}
	if len(validation.Fence.Bindings) != 1 ||
		validation.Fence.Bindings[0].Role != job.ConnectionRoleSource ||
		validation.Fence.Bindings[0].ConnectionUID == "" ||
		validation.Fence.Bindings[0].Generation != 1 ||
		validation.Fence.CompilerRevision != result.GetCompilerRevision() {
		t.Fatalf("unexpected durable validation fence: %+v", validation.Fence)
	}
	if fixture.compiler.request == nil {
		t.Fatal("compiler was not called")
	}
	source := fixture.compiler.request.GetSource()
	if !source.GetConnectionConfigured() || source.GetConnectionSchemaRevision() == "" ||
		source.GetConnectionSettings()["hostname"] != "db.internal" ||
		len(source.GetConfiguredSecretFields()) != 2 {
		t.Fatalf("unexpected effective source metadata: %+v", source)
	}
	payload := fixture.compiler.request.String()
	for _, prohibited := range []string{
		"orders-db", "mysql-orders-v1", "b61234d1-fe64-4546-8f61-8edce2ac9321",
	} {
		if strings.Contains(payload, prohibited) {
			t.Fatalf("compiler request leaked %q: %s", prohibited, payload)
		}
	}
	if strings.Contains(payload, "secret_name") || strings.Contains(payload, "secret_uid") ||
		strings.Contains(payload, "secret_key") {
		t.Fatalf("compiler request leaked locator shape: %s", payload)
	}
}

func TestJobValidationRejectsRawSensitiveOptionsBeforeCompiler(t *testing.T) {
	fixture := newJobValidationFixture(t, true)
	request := mysqlToCSVValidationRequest("orders-db")
	request.Spec.Source.Options["password"] = "raw-secret-sentinel"

	result, err := fixture.service.ValidateJobSpec(fixture.ctx, request)
	if err != nil || result.GetValid() || fixture.compiler.request != nil {
		t.Fatalf("raw sensitive option was not rejected locally: result=%+v err=%v", result, err)
	}
	if len(result.GetIssues()) == 0 ||
		result.GetIssues()[0].GetCode() != controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_OPTION_INVALID {
		t.Fatalf("unexpected issues: %+v", result.GetIssues())
	}
	if strings.Contains(result.String(), "raw-secret-sentinel") {
		t.Fatalf("validation result leaked sensitive value: %s", result)
	}
}

func TestJobValidationStartUsesStoredSpecAndRejectsCallerAlternate(t *testing.T) {
	fixture := newJobValidationFixture(t, false)
	storedSpec := job.Spec{
		Source:   job.ConnectorSpec{Connector: "csv", Options: map[string]string{"path": "stored-input.csv"}},
		Sink:     job.ConnectorSpec{Connector: "csv", Options: map[string]string{"path": "stored-output.csv"}},
		Delivery: job.DeliverySpec{Guarantee: job.DeliveryAtMostOnce},
		Runtime:  job.RuntimeSpec{MaxBatchRecords: 200},
	}
	stored, err := job.New(job.Key{Namespace: "tenant-a", Name: "stored-job"}, uuid.NewString(), storedSpec, fixture.now)
	if err != nil {
		t.Fatalf("new stored Job: %v", err)
	}
	if _, err := fixture.jobs.Create(context.Background(), stored); err != nil {
		t.Fatalf("store Job: %v", err)
	}
	result, err := fixture.service.ValidateJobSpec(fixture.ctx, &controlv1.ValidateJobSpecRequest{
		Namespace: "tenant-a", Name: "stored-job", ExpectedVersion: 1,
		Purpose: controlv1.JobValidationPurpose_JOB_VALIDATION_PURPOSE_START,
	})
	if err != nil || !result.GetValid() || fixture.compiler.request.GetSource().GetJobOptions()["path"] != "stored-input.csv" {
		t.Fatalf("START did not validate stored spec: result=%+v request=%+v err=%v", result, fixture.compiler.request, err)
	}
	_, err = fixture.service.ValidateJobSpec(fixture.ctx, &controlv1.ValidateJobSpecRequest{
		Namespace: "tenant-a", Name: "stored-job", ExpectedVersion: 1,
		Purpose: controlv1.JobValidationPurpose_JOB_VALIDATION_PURPOSE_START,
		Spec:    csvSpec("alternate.csv", "alternate-output.csv"),
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected caller START spec rejection, got %v", err)
	}
}

func TestJobValidationConnectionRuntimeGateBlocksOnlyStart(t *testing.T) {
	fixture := newJobValidationFixture(t, true)
	storedSpec := job.Spec{
		Source: job.ConnectorSpec{
			Connector: "mysql-cdc", ConnectionRef: "orders-db",
			Options: map[string]string{
				"tables": "orders", "topicPrefix": "orders-cdc", "serverId": "1001",
				"schemaHistoryFile": "history.dat",
			},
		},
		Sink:     job.ConnectorSpec{Connector: "csv", Options: map[string]string{"path": "output.csv"}},
		Delivery: job.DeliverySpec{Guarantee: job.DeliveryAtMostOnce},
		Runtime:  job.RuntimeSpec{MaxBatchRecords: 100},
	}
	stored, err := job.New(
		job.Key{Namespace: "tenant-a", Name: "connection-start"}, uuid.NewString(), storedSpec, fixture.now,
	)
	if err != nil {
		t.Fatalf("new Connection-backed Job: %v", err)
	}
	if _, err := fixture.jobs.Create(context.Background(), stored); err != nil {
		t.Fatalf("store Connection-backed Job: %v", err)
	}
	request := &controlv1.ValidateJobSpecRequest{
		Namespace: "tenant-a", Name: "connection-start", ExpectedVersion: 1,
		Purpose: controlv1.JobValidationPurpose_JOB_VALIDATION_PURPOSE_START,
	}
	if _, err := fixture.service.ValidateJobSpec(fixture.ctx, request); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expected default runtime rollout gate, got %v", err)
	}

	enabled, err := service.NewJobValidationService(
		fixture.jobs, fixture.connections, fixture.catalog, auth.ContextAuthorizer{}, fixture.compiler,
		"standard", uuid.NewString, service.WithConnectionRuntimeEnabled(true),
	)
	if err != nil {
		t.Fatalf("create runtime-enabled validation service: %v", err)
	}
	result, err := enabled.ValidateJobSpec(fixture.ctx, request)
	if err != nil || !result.GetValid() {
		t.Fatalf("validate explicitly enabled Connection-backed Start: result=%+v err=%v", result, err)
	}
}

func TestJobValidationAuthorizesConnectionUseBeforeLookup(t *testing.T) {
	fixture := newJobValidationFixture(t, true)
	observer := &observingConnectionRepository{Repository: fixture.connections}
	deniedService, err := service.NewJobValidationService(
		fixture.jobs, observer, fixture.catalog, auth.ContextAuthorizer{}, fixture.compiler,
		"standard", uuid.NewString,
	)
	if err != nil {
		t.Fatalf("create denied validation service: %v", err)
	}
	membership, err := auth.NewMembership(catalogTenantID, true, auth.PermissionJobsCreate)
	if err != nil {
		t.Fatalf("membership: %v", err)
	}
	membership.TenantNamespace = "tenant-a"
	ctx, err := auth.WithPrincipal(context.Background(), auth.Principal{
		ID: "without-use", Subject: "without-use", Active: true, PolicyRevision: "policy-1",
		Memberships: map[string]auth.Membership{catalogTenantID: membership},
	})
	if err != nil {
		t.Fatalf("principal context: %v", err)
	}
	_, err = deniedService.ValidateJobSpec(ctx, mysqlToCSVValidationRequest("orders-db"))
	if status.Code(err) != codes.PermissionDenied || observer.getCalls != 0 {
		t.Fatalf("expected pre-lookup permission denial, calls=%d err=%v", observer.getCalls, err)
	}
}

func TestJobValidationHidesMissingAndDisabledConnectionDistinction(t *testing.T) {
	fixture := newJobValidationFixture(t, false)
	missing, err := fixture.service.ValidateJobSpec(fixture.ctx, mysqlToCSVValidationRequest("missing-db"))
	if err != nil {
		t.Fatalf("validate missing Connection: %v", err)
	}
	disabled, err := fixture.service.ValidateJobSpec(fixture.ctx, mysqlToCSVValidationRequest("orders-db"))
	if err != nil {
		t.Fatalf("validate disabled Connection: %v", err)
	}
	if len(missing.GetIssues()) != 1 || len(disabled.GetIssues()) != 1 ||
		missing.GetIssues()[0].GetCode() != controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_CONNECTION_REF_UNAVAILABLE ||
		!proto.Equal(missing.GetIssues()[0], disabled.GetIssues()[0]) {
		t.Fatalf("Connection availability became an oracle: missing=%+v disabled=%+v", missing, disabled)
	}
}

func TestJobValidationFailsClosedOnCompilerCatalogSkew(t *testing.T) {
	fixture := newJobValidationFixture(t, false)
	fixture.compiler.result.CompilerRevision = "sha256:" + strings.Repeat("0", 64)

	_, err := fixture.service.ValidateJobSpec(fixture.ctx, &controlv1.ValidateJobSpecRequest{
		Namespace: "tenant-a", Name: "csv-copy",
		Purpose: controlv1.JobValidationPurpose_JOB_VALIDATION_PURPOSE_CREATE,
		Spec:    csvSpec("input.csv", "output.csv"),
	})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("expected compiler/catalog skew rejection, got %v", err)
	}
}

type jobValidationFixture struct {
	ctx         context.Context
	service     *service.JobValidationService
	compiler    *recordingCompiler
	jobs        *jobmemory.Repository
	connections *connectionmemory.Repository
	catalog     *catalogmemory.Repository
	now         time.Time
}

func newJobValidationFixture(t *testing.T, enableConnection bool) jobValidationFixture {
	t.Helper()
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	catalogRepository := catalogmemory.New()
	reconciler, err := catalog.NewReconciler(
		catalogRepository, catalogproto.Validator{}, func() time.Time { return now }, uuid.NewString,
	)
	if err != nil {
		t.Fatalf("create catalog reconciler: %v", err)
	}
	snapshot, _, err := reconciler.Reconcile(
		context.Background(), readCatalogFixture(t), "service:catalog-reconciler", uuid.NewString(),
	)
	if err != nil {
		t.Fatalf("activate catalog: %v", err)
	}
	membership, err := auth.NewMembership(
		catalogTenantID, true, auth.PermissionJobsCreate, auth.PermissionJobsStart,
		auth.PermissionConnectionsCreate, auth.PermissionConnectionsDisable, auth.PermissionConnectionsUse,
	)
	if err != nil {
		t.Fatalf("membership: %v", err)
	}
	membership.TenantNamespace = "tenant-a"
	ctx, err := auth.WithPrincipal(context.Background(), auth.Principal{
		ID: "job-editor", Subject: "job-editor", Active: true, PolicyRevision: "policy-1",
		Memberships: map[string]auth.Membership{catalogTenantID: membership},
	})
	if err != nil {
		t.Fatalf("principal context: %v", err)
	}
	connectionRepository := connectionmemory.New()
	connectionService, err := service.NewConnectionService(
		connectionRepository, catalogRepository, auth.ContextAuthorizer{}, "standard",
		[]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return now }, uuid.NewString,
		service.WithConnectionMutationsEnabled(true),
		service.WithConnectionTestsEnabled(true),
	)
	if err != nil {
		t.Fatalf("create Connection service: %v", err)
	}
	created, err := connectionService.CreateConnection(ctx, validCreateConnectionRequest())
	if err != nil {
		t.Fatalf("create fixture Connection: %v", err)
	}
	if enableConnection {
		_, err = connectionService.EnableConnection(ctx, &controlv1.EnableConnectionRequest{
			TenantId: catalogTenantID, Name: created.GetName(), ExpectedVersion: created.GetVersion(),
			IdempotencyKey: fixtureIdempotencyKey("enable-job-validation-0001"),
		})
		if err != nil {
			t.Fatalf("enable fixture Connection: %v", err)
		}
	}
	compiler := &recordingCompiler{result: &compilerv1.ValidateResponse{
		Valid: true, CompilerRevision: snapshot.CompilerRevision, InventoryRevision: snapshot.InventoryRevision,
		ExecutionMode: compilerv1.CompilerExecutionMode_COMPILER_EXECUTION_MODE_CDC,
	}}
	jobs := jobmemory.New()
	validationService, err := service.NewJobValidationService(
		jobs, connectionRepository, catalogRepository, auth.ContextAuthorizer{}, compiler,
		"standard", uuid.NewString,
	)
	if err != nil {
		t.Fatalf("create Job validation service: %v", err)
	}
	return jobValidationFixture{
		ctx: ctx, service: validationService, compiler: compiler, jobs: jobs,
		connections: connectionRepository, catalog: catalogRepository, now: now,
	}
}

func mysqlToCSVValidationRequest(connectionRef string) *controlv1.ValidateJobSpecRequest {
	return &controlv1.ValidateJobSpecRequest{
		Namespace: "tenant-a", Name: "orders-copy",
		Purpose: controlv1.JobValidationPurpose_JOB_VALIDATION_PURPOSE_CREATE,
		Spec: &controlv1.JobSpec{
			Source: &controlv1.ConnectorConfig{
				Connector: "mysql-cdc", ConnectionRef: connectionRef,
				Options: map[string]string{
					"tables": "orders", "topicPrefix": "orders-cdc", "serverId": "1001",
					"schemaHistoryFile": "history.dat",
				},
			},
			Sink: &controlv1.ConnectorConfig{Connector: "csv", Options: map[string]string{"path": "output.csv"}},
			Delivery: &controlv1.DeliveryConfig{
				Guarantee: controlv1.DeliveryGuarantee_DELIVERY_GUARANTEE_AT_MOST_ONCE,
			},
			Runtime: &controlv1.RuntimeConfig{MaxBatchRecords: 100},
		},
	}
}

func csvSpec(sourcePath, sinkPath string) *controlv1.JobSpec {
	return &controlv1.JobSpec{
		Source: &controlv1.ConnectorConfig{Connector: "csv", Options: map[string]string{"path": sourcePath}},
		Sink:   &controlv1.ConnectorConfig{Connector: "csv", Options: map[string]string{"path": sinkPath}},
		Delivery: &controlv1.DeliveryConfig{
			Guarantee: controlv1.DeliveryGuarantee_DELIVERY_GUARANTEE_AT_MOST_ONCE,
		},
		Runtime: &controlv1.RuntimeConfig{MaxBatchRecords: 100},
	}
}
