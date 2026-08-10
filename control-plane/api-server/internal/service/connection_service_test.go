package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/api-server/internal/catalogproto"
	"io.astrasync/control-plane/api-server/internal/service"
	"io.astrasync/control-plane/auth"
	"io.astrasync/control-plane/catalog"
	catalogmemory "io.astrasync/control-plane/catalog/memory"
	"io.astrasync/control-plane/connection"
	connectionmemory "io.astrasync/control-plane/connection/memory"
)

func fixtureIdempotencyKey(label string) string {
	return "fixture-" + label + "-idempotency"
}

func TestConnectionServiceRunsRedactedLifecycleWithIdempotentReplay(t *testing.T) {
	ctx, connectionService, _, now := newConnectionService(t)
	create := validCreateConnectionRequest()
	created, err := connectionService.CreateConnection(ctx, create)
	if err != nil {
		t.Fatalf("create Connection: %v", err)
	}
	if created.GetState() != controlv1.ConnectionState_CONNECTION_STATE_DISABLED || created.GetVersion() != 1 ||
		created.GetGeneration() != 1 || !created.GetSecretConfigured() || len(created.GetPublicSettings()) != 1 {
		t.Fatalf("unexpected created Connection: %+v", created)
	}
	assertNoSecretLocator(t, created)
	replayedCreate, err := connectionService.CreateConnection(ctx, create)
	if err != nil || replayedCreate.GetUid() != created.GetUid() ||
		replayedCreate.GetOutcome() != controlv1.MutationOutcome_MUTATION_OUTCOME_REPLAYED {
		t.Fatalf("replay create: response=%+v err=%v", replayedCreate, err)
	}

	*now = now.Add(time.Minute)
	enabled, err := connectionService.EnableConnection(ctx, &controlv1.EnableConnectionRequest{
		TenantId: catalogTenantID, Name: create.GetName(), ExpectedVersion: 1,
		IdempotencyKey: fixtureIdempotencyKey("enable-orders-0001"),
	})
	if err != nil || enabled.GetState() != controlv1.ConnectionState_CONNECTION_STATE_ACTIVE || enabled.GetVersion() != 2 {
		t.Fatalf("enable: response=%+v err=%v", enabled, err)
	}
	activeSettings := []*controlv1.ConnectionSetting{
		{Key: "database", Value: "orders_active_change"},
		{Key: "hostname", Value: "db.internal"},
		{Key: "sslMode", Value: "required"},
	}
	if _, err := connectionService.UpdateConnection(ctx, &controlv1.UpdateConnectionRequest{
		TenantId: catalogTenantID, Name: create.GetName(), ExpectedVersion: 2,
		DisplayName: "Orders DB", Description: "active update", Settings: activeSettings,
		IdempotencyKey: fixtureIdempotencyKey("update-active-0001"),
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expected active effective update rejection, got %v", err)
	}

	*now = now.Add(time.Minute)
	rotated, err := connectionService.RotateConnection(ctx, &controlv1.RotateConnectionRequest{
		TenantId: catalogTenantID, Name: create.GetName(), ExpectedVersion: 2,
		IdempotencyKey: fixtureIdempotencyKey("rotate-orders-0001"), SecretBinding: kubernetesBinding(
			"mysql-orders-v2", "d1472583-3ed7-4db9-9d18-44bcfc5896a2",
		),
	})
	if err != nil || rotated.GetVersion() != 3 || rotated.GetGeneration() != 2 ||
		rotated.GetState() != controlv1.ConnectionState_CONNECTION_STATE_ACTIVE {
		t.Fatalf("rotate: response=%+v err=%v", rotated, err)
	}
	assertNoSecretLocator(t, rotated)

	*now = now.Add(time.Minute)
	disabled, err := connectionService.DisableConnection(ctx, &controlv1.DisableConnectionRequest{
		TenantId: catalogTenantID, Name: create.GetName(), ExpectedVersion: 3,
		IdempotencyKey: fixtureIdempotencyKey("disable-orders-001"),
	})
	if err != nil || disabled.GetVersion() != 4 || disabled.GetState() != controlv1.ConnectionState_CONNECTION_STATE_DISABLED {
		t.Fatalf("disable: response=%+v err=%v", disabled, err)
	}

	*now = now.Add(time.Minute)
	update := &controlv1.UpdateConnectionRequest{
		TenantId: catalogTenantID, Name: create.GetName(), ExpectedVersion: 4,
		DisplayName: "Orders DB", Description: "primary orders database",
		Settings: []*controlv1.ConnectionSetting{
			{Key: "database", Value: "orders_v2"}, {Key: "hostname", Value: "db.internal"},
			{Key: "sslMode", Value: "required"},
		},
		IdempotencyKey: fixtureIdempotencyKey("update-orders-0001"),
	}
	updated, err := connectionService.UpdateConnection(ctx, update)
	if err != nil || updated.GetVersion() != 5 || updated.GetGeneration() != 3 ||
		updated.GetPublicSettings()[0].GetValue() != "required" {
		t.Fatalf("update: response=%+v err=%v", updated, err)
	}
	replayedUpdate, err := connectionService.UpdateConnection(ctx, update)
	if err != nil || replayedUpdate.GetVersion() != 5 || replayedUpdate.GetGeneration() != 3 ||
		replayedUpdate.GetOutcome() != controlv1.MutationOutcome_MUTATION_OUTCOME_REPLAYED {
		t.Fatalf("replay update after version advancement: response=%+v err=%v", replayedUpdate, err)
	}
}

func TestConnectionServiceRejectsRawSecretsAndIdempotencyDigestMismatch(t *testing.T) {
	ctx, connectionService, _, _ := newConnectionService(t)
	request := validCreateConnectionRequest()
	request.Settings = append(request.Settings, &controlv1.ConnectionSetting{Key: "password", Value: "raw-secret-sentinel"})
	if _, err := connectionService.CreateConnection(ctx, request); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected raw secret rejection, got %v", err)
	}

	request = validCreateConnectionRequest()
	if _, err := connectionService.CreateConnection(ctx, request); err != nil {
		t.Fatalf("create: %v", err)
	}
	mismatch := validCreateConnectionRequest()
	mismatch.DisplayName = "different"
	if _, err := connectionService.CreateConnection(ctx, mismatch); status.Code(err) != codes.AlreadyExists ||
		status.Convert(err).Message() != "IDEMPOTENCY_KEY_REUSED" {
		t.Fatalf("expected idempotency digest mismatch, got %v", err)
	}
}

func TestConnectionServicePaginationIsTenantAndRevisionBound(t *testing.T) {
	ctx, connectionService, _, _ := newConnectionService(t)
	firstCreate := validCreateConnectionRequest()
	firstCreate.Name = "alpha"
	firstCreate.IdempotencyKey = "create-alpha-0001"
	secondCreate := validCreateConnectionRequest()
	secondCreate.Name = "beta"
	secondCreate.IdempotencyKey = "create-beta-00001"
	for _, request := range []*controlv1.CreateConnectionRequest{firstCreate, secondCreate} {
		if _, err := connectionService.CreateConnection(ctx, request); err != nil {
			t.Fatalf("create %s: %v", request.GetName(), err)
		}
	}
	page, err := connectionService.ListConnections(ctx, &controlv1.ListConnectionsRequest{
		TenantId: catalogTenantID, PageSize: 1,
	})
	if err != nil || len(page.GetConnections()) != 1 || page.GetNextPageToken() == "" {
		t.Fatalf("first page: response=%+v err=%v", page, err)
	}
	third := validCreateConnectionRequest()
	third.Name = "gamma"
	third.IdempotencyKey = "create-gamma-0001"
	if _, err := connectionService.CreateConnection(ctx, third); err != nil {
		t.Fatalf("create after first page: %v", err)
	}
	_, err = connectionService.ListConnections(ctx, &controlv1.ListConnectionsRequest{
		TenantId: catalogTenantID, PageSize: 1, PageToken: page.GetNextPageToken(),
	})
	if status.Code(err) != codes.Aborted {
		t.Fatalf("expected changed list snapshot rejection, got %v", err)
	}
}

func TestConnectionServiceQueuesGenerationBoundTestAndReferenceSafeDelete(t *testing.T) {
	ctx, connectionService, repository, _ := newConnectionService(t)
	created, err := connectionService.CreateConnection(ctx, validCreateConnectionRequest())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	testResult, err := connectionService.TestConnection(ctx, &controlv1.TestConnectionRequest{
		TenantId: catalogTenantID, Name: created.GetName(), ExpectedVersion: 1,
		IdempotencyKey: fixtureIdempotencyKey("test-orders-000001"),
	})
	if err != nil || testResult.GetState() != controlv1.ConnectionTestState_CONNECTION_TEST_STATE_QUEUED ||
		testResult.GetGeneration() != 1 {
		t.Fatalf("queue test: response=%+v err=%v", testResult, err)
	}
	loaded, err := connectionService.GetConnectionTest(ctx, &controlv1.GetConnectionTestRequest{
		TenantId: catalogTenantID, OperationId: testResult.GetOperationId(),
	})
	if err != nil || !loaded.GetCurrentGeneration() {
		t.Fatalf("get test: response=%+v err=%v", loaded, err)
	}
	if _, err := connectionService.DeleteConnection(ctx, &controlv1.DeleteConnectionRequest{
		TenantId: catalogTenantID, Name: created.GetName(), ExpectedVersion: 1,
		IdempotencyKey: fixtureIdempotencyKey("delete-orders-0001"),
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("queued test must block deletion, got %v", err)
	}
	repository.SetReferenceCounts(created.GetUid(), connection.ReferenceCounts{Jobs: 1})
	if got, err := connectionService.GetConnection(ctx, &controlv1.GetConnectionRequest{
		TenantId: catalogTenantID, Name: created.GetName(),
	}); err != nil || got.GetReferenceCounts().GetJobs() != 1 {
		t.Fatalf("reference projection: response=%+v err=%v", got, err)
	}
}

func TestConnectionServiceCapturesActorDeadlineAndTenantEgressPolicy(t *testing.T) {
	policy, err := connection.NewTestEgressPolicy([]string{"10.42.0.0/16"})
	if err != nil {
		t.Fatalf("construct test policy: %v", err)
	}
	ctx, connectionService, repository, now := newConnectionService(t,
		service.WithConnectionTestDeadline(15*time.Second),
		service.WithConnectionTestPolicyResolver(service.ConnectionTestPolicyResolverFunc(
			func(context.Context, string) (connection.TestEgressPolicy, error) {
				return policy.Clone(), nil
			},
		)),
	)
	created, err := connectionService.CreateConnection(ctx, validCreateConnectionRequest())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	queued, err := connectionService.TestConnection(ctx, &controlv1.TestConnectionRequest{
		TenantId: catalogTenantID, Name: created.GetName(), ExpectedVersion: 1,
		IdempotencyKey: fixtureIdempotencyKey("test-policy-00001"),
	})
	if err != nil {
		t.Fatalf("queue test: %v", err)
	}
	stored, err := repository.GetTest(ctx, catalogTenantID, queued.GetOperationId())
	if err != nil || stored.ActorID != "principal-admin" ||
		!stored.DeadlineAt.Equal(now.Add(15*time.Second)) ||
		stored.EgressPolicy.Revision != policy.Revision ||
		len(stored.EgressPolicy.AllowedCIDRs) != 1 || stored.EgressPolicy.AllowedCIDRs[0] != "10.42.0.0/16" {
		t.Fatalf("test admission metadata was not captured: result=%+v err=%v", stored, err)
	}
}

func TestConnectionServiceRolloutGatesFailClosed(t *testing.T) {
	ctx, mutationDisabled, _, _ := newConnectionService(
		t, service.WithConnectionMutationsEnabled(false),
	)
	if _, err := mutationDisabled.CreateConnection(ctx, validCreateConnectionRequest()); status.Code(err) != codes.FailedPrecondition || status.Convert(err).Message() != "CONNECTION_MUTATIONS_DISABLED" {
		t.Fatalf("expected mutation rollout gate, got %v", err)
	}

	ctx, testsDisabled, _, _ := newConnectionService(
		t, service.WithConnectionTestsEnabled(false),
	)
	created, err := testsDisabled.CreateConnection(ctx, validCreateConnectionRequest())
	if err != nil {
		t.Fatalf("create Connection for test gate: %v", err)
	}
	_, err = testsDisabled.TestConnection(ctx, &controlv1.TestConnectionRequest{
		TenantId: catalogTenantID, Name: created.GetName(), ExpectedVersion: created.GetVersion(),
		IdempotencyKey: fixtureIdempotencyKey("test-rollout-gate-0001"),
	})
	if status.Code(err) != codes.FailedPrecondition || status.Convert(err).Message() != "CONNECTION_TESTS_DISABLED" {
		t.Fatalf("expected test rollout gate, got %v", err)
	}
}

func newConnectionService(
	t *testing.T,
	options ...service.ConnectionServiceOption,
) (context.Context, *service.ConnectionService, *connectionmemory.Repository, *time.Time) {
	t.Helper()
	now := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	catalogRepository := catalogmemory.New()
	reconciler, err := catalog.NewReconciler(
		catalogRepository, catalogproto.Validator{}, func() time.Time { return now }, uuid.NewString,
	)
	if err != nil {
		t.Fatalf("create catalog reconciler: %v", err)
	}
	if _, _, err := reconciler.Reconcile(
		context.Background(), readCatalogFixture(t), "service:catalog-reconciler", uuid.NewString(),
	); err != nil {
		t.Fatalf("activate catalog: %v", err)
	}
	membership, err := auth.NewMembership(catalogTenantID, true, auth.PermissionsForRole(auth.RoleTenantAdmin)...)
	if err != nil {
		t.Fatalf("membership: %v", err)
	}
	ctx, err := auth.WithPrincipal(context.Background(), auth.Principal{
		ID: "principal-admin", Subject: "admin", Active: true, PolicyRevision: "policy-1",
		Memberships: map[string]auth.Membership{catalogTenantID: membership},
	})
	if err != nil {
		t.Fatalf("principal context: %v", err)
	}
	repository := connectionmemory.New()
	allOptions := []service.ConnectionServiceOption{
		service.WithConnectionMutationsEnabled(true),
		service.WithConnectionTestsEnabled(true),
	}
	allOptions = append(allOptions, options...)
	connectionService, err := service.NewConnectionService(
		repository, catalogRepository, auth.ContextAuthorizer{}, "standard",
		[]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return now }, uuid.NewString,
		allOptions...,
	)
	if err != nil {
		t.Fatalf("create Connection service: %v", err)
	}
	return ctx, connectionService, repository, &now
}

func validCreateConnectionRequest() *controlv1.CreateConnectionRequest {
	return &controlv1.CreateConnectionRequest{
		TenantId: catalogTenantID, Name: "orders-db", Connector: "mysql-cdc",
		DisplayName: "Orders DB", Description: "orders source",
		Settings: []*controlv1.ConnectionSetting{
			{Key: "hostname", Value: "db.internal"},
			{Key: "database", Value: "orders"},
			{Key: "sslMode", Value: "required"},
		},
		SecretBinding: kubernetesBinding(
			"mysql-orders-v1", "b61234d1-fe64-4546-8f61-8edce2ac9321",
		),
		IdempotencyKey: fixtureIdempotencyKey("create-orders-0001"),
	}
}

func kubernetesBinding(name, uid string) *controlv1.SecretBinding {
	return &controlv1.SecretBinding{
		Provider: controlv1.SecretProviderKind_SECRET_PROVIDER_KIND_KUBERNETES_SECRET_V1,
		Locator: &controlv1.SecretBinding_KubernetesSecretV1{
			KubernetesSecretV1: &controlv1.KubernetesSecretBinding{
				SecretName: name, SecretUid: uid,
				Fields: []*controlv1.SecretFieldMapping{
					{LogicalField: "username", SecretKey: "username"},
					{LogicalField: "password", SecretKey: "password"},
				},
			},
		},
	}
}

func assertNoSecretLocator(t *testing.T, value *controlv1.Connection) {
	t.Helper()
	payload, err := protojson.Marshal(value)
	if err != nil {
		t.Fatalf("marshal Connection projection: %v", err)
	}
	for _, prohibited := range []string{"mysql-orders-v", "b61234d1", "d1472583", "secretKey"} {
		if strings.Contains(string(payload), prohibited) {
			t.Fatalf("Connection projection leaked locator %q: %s", prohibited, payload)
		}
	}
}
