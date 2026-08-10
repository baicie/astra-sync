package service_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/api-server/internal/catalogproto"
	"io.astrasync/control-plane/api-server/internal/service"
	"io.astrasync/control-plane/auth"
	"io.astrasync/control-plane/catalog"
	"io.astrasync/control-plane/catalog/memory"
)

const catalogTenantID = "8d58d674-7cc7-4b15-a46c-9e7768bbf103"

func TestCatalogServiceListsFiltersAndReadsDescriptors(t *testing.T) {
	ctx, catalogService, _ := newCatalogService(t)
	first, err := catalogService.ListConnectorDescriptors(ctx, &controlv1.ListConnectorDescriptorsRequest{
		TenantId: catalogTenantID,
		PageSize: 2,
	})
	if err != nil {
		t.Fatalf("list first page: %v", err)
	}
	if len(first.GetDescriptors()) != 2 || first.GetDescriptors()[0].GetName() != "csv" ||
		first.GetDescriptors()[1].GetName() != "jdbc" || first.GetNextPageToken() == "" {
		t.Fatalf("unexpected first page: %+v", first)
	}
	second, err := catalogService.ListConnectorDescriptors(ctx, &controlv1.ListConnectorDescriptorsRequest{
		TenantId:  catalogTenantID,
		PageSize:  2,
		PageToken: first.GetNextPageToken(),
	})
	if err != nil {
		t.Fatalf("list second page: %v", err)
	}
	if len(second.GetDescriptors()) != 2 || second.GetDescriptors()[0].GetName() != "mysql-cdc" ||
		second.GetDescriptors()[1].GetName() != "postgres-cdc" || second.GetNextPageToken() != "" {
		t.Fatalf("unexpected second page: %+v", second)
	}

	cdc, err := catalogService.ListConnectorDescriptors(ctx, &controlv1.ListConnectorDescriptorsRequest{
		TenantId:     catalogTenantID,
		Capabilities: []controlv1.ConnectorCapability{controlv1.ConnectorCapability_CONNECTOR_CAPABILITY_CHANGE_DATA_CAPTURE},
	})
	if err != nil || len(cdc.GetDescriptors()) != 2 {
		t.Fatalf("filter CDC catalog: response=%+v err=%v", cdc, err)
	}
	descriptor, err := catalogService.GetConnectorDescriptor(ctx, &controlv1.GetConnectorDescriptorRequest{
		TenantId: catalogTenantID,
		Name:     "jdbc",
	})
	if err != nil || descriptor.GetConnectorDescriptor().GetDisplayName() != "JDBC" {
		t.Fatalf("get JDBC descriptor: response=%+v err=%v", descriptor, err)
	}
}

func TestCatalogPageTokenRejectsTamperingScopeAndInventoryChanges(t *testing.T) {
	ctx, catalogService, repository := newCatalogService(t)
	page, err := catalogService.ListConnectorDescriptors(ctx, &controlv1.ListConnectorDescriptorsRequest{
		TenantId: catalogTenantID,
		PageSize: 1,
	})
	if err != nil {
		t.Fatalf("list catalog: %v", err)
	}
	token := page.GetNextPageToken()
	_, err = catalogService.ListConnectorDescriptors(ctx, &controlv1.ListConnectorDescriptorsRequest{
		TenantId:  catalogTenantID,
		PageSize:  1,
		PageToken: token + "x",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected tampered token rejection, got %v", err)
	}

	activateReducedInventory(t, repository)
	_, err = catalogService.ListConnectorDescriptors(ctx, &controlv1.ListConnectorDescriptorsRequest{
		TenantId:  catalogTenantID,
		PageSize:  1,
		PageToken: token,
	})
	if status.Code(err) != codes.FailedPrecondition || status.Convert(err).Message() != "CATALOG_REVISION_CHANGED" {
		t.Fatalf("expected catalog revision change, got %v", err)
	}
}

func TestCatalogServiceAuthorizesBeforeReads(t *testing.T) {
	_, catalogService, _ := newCatalogService(t)
	_, err := catalogService.ListConnectorDescriptors(context.Background(), &controlv1.ListConnectorDescriptorsRequest{
		TenantId: catalogTenantID,
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected unauthenticated denial, got %v", err)
	}

	membership, membershipErr := auth.NewMembership(catalogTenantID, true, auth.PermissionConnectionsRead)
	if membershipErr != nil {
		t.Fatalf("membership: %v", membershipErr)
	}
	denied, contextErr := auth.WithPrincipal(context.Background(), auth.Principal{
		Subject:        "operator",
		Active:         true,
		PolicyRevision: "policy-1",
		Memberships:    map[string]auth.Membership{catalogTenantID: membership},
	})
	if contextErr != nil {
		t.Fatalf("principal context: %v", contextErr)
	}
	_, err = catalogService.GetConnectorDescriptor(denied, &controlv1.GetConnectorDescriptorRequest{
		TenantId: catalogTenantID,
		Name:     "jdbc",
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected permission denial, got %v", err)
	}
}

func newCatalogService(t *testing.T) (context.Context, *service.ConnectorCatalogService, *memory.Repository) {
	t.Helper()
	payload := readCatalogFixture(t)
	repository := memory.New()
	now := time.Date(2026, 8, 8, 2, 0, 0, 0, time.UTC)
	eventSequence := 0
	reconciler, err := catalog.NewReconciler(
		repository,
		catalogproto.Validator{},
		func() time.Time { return now },
		func() string {
			eventSequence++
			return fmt.Sprintf("event-%d", eventSequence)
		},
	)
	if err != nil {
		t.Fatalf("create catalog reconciler: %v", err)
	}
	if _, _, err := reconciler.Reconcile(context.Background(), payload, "catalog-publisher", "request-1"); err != nil {
		t.Fatalf("reconcile deployment catalog: %v", err)
	}
	membership, err := auth.NewMembership(catalogTenantID, true, auth.PermissionConnectorsRead)
	if err != nil {
		t.Fatalf("membership: %v", err)
	}
	ctx, err := auth.WithPrincipal(context.Background(), auth.Principal{
		Subject:        "viewer",
		Active:         true,
		PolicyRevision: "policy-1",
		Memberships:    map[string]auth.Membership{catalogTenantID: membership},
	})
	if err != nil {
		t.Fatalf("principal context: %v", err)
	}
	catalogService, err := service.NewConnectorCatalogService(
		repository,
		auth.ContextAuthorizer{},
		"standard",
		[]byte("0123456789abcdef0123456789abcdef"),
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("create catalog service: %v", err)
	}
	return ctx, catalogService, repository
}

func activateReducedInventory(t *testing.T, repository *memory.Repository) {
	t.Helper()
	inventory := &controlv1.ConnectorInventory{}
	if err := proto.Unmarshal(readCatalogFixture(t), inventory); err != nil {
		t.Fatalf("decode catalog fixture: %v", err)
	}
	inventory.Descriptors = inventory.Descriptors[:3]
	identity := &controlv1.ConnectorInventoryIdentity{}
	for _, descriptor := range inventory.GetDescriptors() {
		identity.Entries = append(identity.Entries, &controlv1.ConnectorInventoryEntry{
			Name:               descriptor.GetName(),
			ArtifactVersion:    descriptor.GetArtifactVersion(),
			DescriptorRevision: descriptor.GetDescriptorRevision(),
		})
	}
	inventory.InventoryRevision = protoDigest(t, identity)
	inventory.CompilerRevision = protoDigest(t, &controlv1.ConnectorCompilerIdentity{
		InventoryRevision:     inventory.GetInventoryRevision(),
		JobSpecSchemaRevision: inventory.GetJobSpecSchemaRevision(),
		CompilerBuild:         inventory.GetCompilerBuild(),
		ExecutionProfile:      inventory.GetExecutionProfile(),
	})
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(inventory)
	if err != nil {
		t.Fatalf("encode reduced inventory: %v", err)
	}
	snapshot, err := (catalogproto.Validator{}).Validate(payload, time.Date(2026, 8, 8, 3, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("validate reduced inventory: %v", err)
	}
	if _, err := repository.Activate(snapshotContext(), snapshot, catalog.AuditEvent{
		EventID:         "event-reduced",
		ActorID:         "catalog-publisher",
		RequestID:       "request-reduced",
		OldRevision:     "sha256:1d0247c24e0c30e78bdbd135e43f72a6c58a4d2680c0d59337ae69668b771778",
		NewRevision:     snapshot.InventoryRevision,
		DescriptorCount: len(snapshot.Descriptors),
		Outcome:         "CHANGED",
		OccurredAt:      snapshot.ActivatedAt,
	}); err != nil {
		t.Fatalf("activate reduced inventory: %v", err)
	}
}

func protoDigest(t *testing.T, message proto.Message) string {
	t.Helper()
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(message)
	if err != nil {
		t.Fatalf("marshal digest input: %v", err)
	}
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("sha256:%x", sum)
}

func readCatalogFixture(t *testing.T) []byte {
	t.Helper()
	payload, err := os.ReadFile("../../../../deployment/catalog/connector-inventory.pb")
	if err != nil {
		t.Fatalf("read catalog fixture: %v", err)
	}
	return payload
}

func snapshotContext() context.Context {
	return context.Background()
}
