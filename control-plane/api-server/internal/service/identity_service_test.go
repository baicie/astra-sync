package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/api-server/internal/service"
	"io.astrasync/control-plane/auth"
)

const (
	identityPrincipalID = "11111111-1111-1111-8111-111111111111"
	identityTenantAID   = "22222222-2222-4222-8222-222222222222"
	identityTenantBID   = "33333333-3333-4333-8333-333333333333"
	identityTenantCID   = "44444444-4444-4444-8444-444444444444"
)

func TestIdentityServiceRequiresAuthenticatedPrincipal(t *testing.T) {
	repository := &fakeIdentityRepository{}
	principalFixture, err := service.NewIdentityService(repository, auth.DevelopmentAuthorizer{})
	if err != nil {
		t.Fatalf("new identity service: %v", err)
	}
	if _, err := principalFixture.GetCurrentPrincipal(context.Background(), &controlv1.GetCurrentPrincipalRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected unauthenticated denial, got %v", err)
	}
	if _, err := principalFixture.ListTenants(context.Background(), &controlv1.ListTenantsRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected unauthenticated denial, got %v", err)
	}
}

func TestIdentityServiceProjectsRolesAndPermissions(t *testing.T) {
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)
	principal := auth.Principal{
		ID: identityPrincipalID, Issuer: "https://issuer.example", Subject: "alice",
		Active: true, PolicyRevision: "1",
		Memberships: map[string]auth.Membership{
			identityTenantAID: {
				TenantID: identityTenantAID,
				Role:     auth.RoleTenantAuditor, Active: true,
				TenantNamespace: "alpha", TenantDisplayName: "Alpha",
				PolicyRevision: "3",
			},
			identityTenantBID: {
				TenantID: identityTenantBID,
				Role:     auth.RoleTenantAdmin, Active: false,
				TenantNamespace: "beta", TenantDisplayName: "Beta",
				PolicyRevision: "7",
			},
		},
	}
	repository := &fakeIdentityRepository{principals: map[string]auth.Principal{identityPrincipalID: principal}}
	serviceUnderTest, err := service.NewIdentityService(repository, auth.DevelopmentAuthorizer{},
		service.WithIdentityClock(func() time.Time { return now }),
		service.WithIdentityUIDSource(func() string { return "identity-uid" }),
	)
	if err != nil {
		t.Fatalf("new identity service: %v", err)
	}
	contextWithPrincipal, err := auth.WithPrincipal(context.Background(), principal)
	if err != nil {
		t.Fatalf("principal context: %v", err)
	}
	response, err := serviceUnderTest.GetCurrentPrincipal(contextWithPrincipal, &controlv1.GetCurrentPrincipalRequest{})
	if err != nil {
		t.Fatalf("get current principal: %v", err)
	}
	if response.GetPrincipalId() != identityPrincipalID {
		t.Fatalf("unexpected principal id: %s", response.GetPrincipalId())
	}
	if response.GetStatus() != auth.PrincipalStatusActive {
		t.Fatalf("unexpected status: %s", response.GetStatus())
	}
	if len(response.GetMemberships()) != 1 || response.GetMemberships()[0].GetTenantId() != identityTenantAID {
		t.Fatalf("inactive membership was not filtered: %+v", response.GetMemberships())
	}
	if response.GetMemberships()[0].GetAuthzRevision() != 3 {
		t.Fatalf("authz revision was not projected: %d", response.GetMemberships()[0].GetAuthzRevision())
	}
	if len(response.GetPlatformRoles()) != 0 {
		t.Fatalf("non-admin principal should not have platform roles: %+v", response.GetPlatformRoles())
	}

	platformPrincipal := principal
	platformPrincipal.PlatformAdmin = true
	platformPrincipal.Memberships = map[string]auth.Membership{}
	repository.principals[identityPrincipalID] = platformPrincipal
	platformResponse, err := serviceUnderTest.GetCurrentPrincipal(contextWithPrincipal, &controlv1.GetCurrentPrincipalRequest{})
	if err != nil {
		t.Fatalf("get current principal: %v", err)
	}
	if len(platformResponse.GetPlatformRoles()) != 1 || platformResponse.GetPlatformRoles()[0] != auth.PlatformRoleAdmin {
		t.Fatalf("platform admin role was not projected: %+v", platformResponse.GetPlatformRoles())
	}
}

func TestIdentityServiceListTenantsRespectsPaginationAndToken(t *testing.T) {
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)
	tenants := map[string]auth.Membership{
		identityTenantAID: {
			TenantID: identityTenantAID, Role: auth.RoleTenantAdmin,
			Active: true, TenantNamespace: "alpha", TenantDisplayName: "Alpha",
		},
		identityTenantBID: {
			TenantID: identityTenantBID, Role: auth.RoleTenantViewer,
			Active: true, TenantNamespace: "beta", TenantDisplayName: "Beta",
		},
		identityTenantCID: {
			TenantID: identityTenantCID, Role: auth.RoleTenantOperator,
			Active: true, TenantNamespace: "gamma", TenantDisplayName: "Gamma",
		},
	}
	repository := &fakeIdentityRepository{
		principals: map[string]auth.Principal{
			identityPrincipalID: {
				ID: identityPrincipalID, Issuer: "https://issuer.example", Subject: "alice",
				Active: true, PolicyRevision: "1",
				Memberships: tenants,
			},
		},
		tenants: map[string]auth.TenantView{
			identityTenantAID: {
				TenantID: identityTenantAID, Namespace: "alpha",
				DisplayName: "Alpha", Status: auth.TenantStatusActive, AuthzRevision: 1,
			},
			identityTenantBID: {
				TenantID: identityTenantBID, Namespace: "beta",
				DisplayName: "Beta", Status: auth.TenantStatusActive, AuthzRevision: 1,
			},
			identityTenantCID: {
				TenantID: identityTenantCID, Namespace: "gamma",
				DisplayName: "Gamma", Status: auth.TenantStatusActive, AuthzRevision: 1,
			},
		},
	}
	serviceUnderTest, err := service.NewIdentityService(repository, auth.DevelopmentAuthorizer{},
		service.WithIdentityClock(func() time.Time { return now }),
	)
	if err != nil {
		t.Fatalf("new identity service: %v", err)
	}
	principal := repository.principals[identityPrincipalID]
	contextWithPrincipal, err := auth.WithPrincipal(context.Background(), principal)
	if err != nil {
		t.Fatalf("principal context: %v", err)
	}

	first, err := serviceUnderTest.ListTenants(contextWithPrincipal, &controlv1.ListTenantsRequest{PageSize: 2})
	if err != nil {
		t.Fatalf("first tenants page: %v", err)
	}
	if len(first.GetTenants()) != 2 || first.GetNextPageToken() == "" {
		t.Fatalf("unexpected first page: %+v", first)
	}
	if first.GetTenants()[0].GetTenantId() != identityTenantAID {
		t.Fatalf("unexpected tenant order: %+v", first.GetTenants())
	}

	second, err := serviceUnderTest.ListTenants(contextWithPrincipal, &controlv1.ListTenantsRequest{
		PageSize: 2, PageToken: first.GetNextPageToken(),
	})
	if err != nil {
		t.Fatalf("second tenants page: %v", err)
	}
	if len(second.GetTenants()) != 1 || second.GetNextPageToken() != "" {
		t.Fatalf("unexpected second page: %+v", second)
	}

	tampered := first.GetNextPageToken() + "x"
	if _, err := serviceUnderTest.ListTenants(contextWithPrincipal, &controlv1.ListTenantsRequest{
		PageSize: 2, PageToken: tampered,
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected tampered token rejection, got %v", err)
	}

	// page token belonging to a different principal should fail.
	otherPrincipal := principal
	otherPrincipal.ID = "99999999-9999-4999-8999-999999999999"
	otherContext, err := auth.WithPrincipal(context.Background(), otherPrincipal)
	if err != nil {
		t.Fatalf("other principal context: %v", err)
	}
	if _, err := serviceUnderTest.ListTenants(otherContext, &controlv1.ListTenantsRequest{
		PageSize: 2, PageToken: first.GetNextPageToken(),
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected cross-principal token rejection, got %v", err)
	}
}

func TestIdentityServiceRejectsOversizedPageTokens(t *testing.T) {
	repository := &fakeIdentityRepository{}
	serviceUnderTest, err := service.NewIdentityService(repository, auth.DevelopmentAuthorizer{})
	if err != nil {
		t.Fatalf("new identity service: %v", err)
	}
	principal := auth.Principal{ID: identityPrincipalID, Subject: "alice", Active: true, PolicyRevision: "1"}
	ctx, err := auth.WithPrincipal(context.Background(), principal)
	if err != nil {
		t.Fatalf("principal context: %v", err)
	}
	if _, err := serviceUnderTest.ListTenants(ctx, &controlv1.ListTenantsRequest{PageSize: 1000}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected oversized page rejection, got %v", err)
	}
}

func TestIdentityServiceRepositoryErrorBubblesAsInternal(t *testing.T) {
	repository := &fakeIdentityRepository{resolveErr: errors.New("postgres detail must remain private")}
	serviceUnderTest, err := service.NewIdentityService(repository, auth.DevelopmentAuthorizer{})
	if err != nil {
		t.Fatalf("new identity service: %v", err)
	}
	principal := auth.Principal{ID: identityPrincipalID, Subject: "alice", Active: true, PolicyRevision: "1"}
	ctx, err := auth.WithPrincipal(context.Background(), principal)
	if err != nil {
		t.Fatalf("principal context: %v", err)
	}
	_, err = serviceUnderTest.GetCurrentPrincipal(ctx, &controlv1.GetCurrentPrincipalRequest{})
	if status.Code(err) != codes.Internal || strings.Contains(status.Convert(err).Message(), "postgres detail") {
		t.Fatalf("expected sanitized repository error, got %v", err)
	}
}

func TestIdentityServiceTenantUnavailableResolvesAsNotFound(t *testing.T) {
	repository := &fakeIdentityRepository{resolveErr: auth.ErrTenantUnavailable}
	serviceUnderTest, err := service.NewIdentityService(repository, auth.DevelopmentAuthorizer{})
	if err != nil {
		t.Fatalf("new identity service: %v", err)
	}
	principal := auth.Principal{ID: identityPrincipalID, Subject: "alice", Active: true, PolicyRevision: "1"}
	ctx, err := auth.WithPrincipal(context.Background(), principal)
	if err != nil {
		t.Fatalf("principal context: %v", err)
	}
	_, err = serviceUnderTest.GetCurrentPrincipal(ctx, &controlv1.GetCurrentPrincipalRequest{})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("expected not-found mapping, got %v", err)
	}
}

type fakeIdentityRepository struct {
	mu         sync.Mutex
	principals map[string]auth.Principal
	tenants    map[string]auth.TenantView
	resolveErr error
	tenantErr  error
	uidCounter int
}

func (r *fakeIdentityRepository) ResolvePrincipalByID(_ context.Context, principalID string) (auth.Principal, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.resolveErr != nil {
		return auth.Principal{}, r.resolveErr
	}
	principal, ok := r.principals[principalID]
	if !ok {
		return auth.Principal{}, auth.ErrUnauthenticated
	}
	return principal, nil
}

func (r *fakeIdentityRepository) ReadTenant(_ context.Context, tenantID string) (auth.TenantView, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tenantErr != nil {
		return auth.TenantView{}, r.tenantErr
	}
	view, ok := r.tenants[tenantID]
	if !ok {
		return auth.TenantView{}, auth.ErrTenantUnavailable
	}
	return view, nil
}

func (r *fakeIdentityRepository) nextUID(prefix string) string {
	r.uidCounter++
	return fmt.Sprintf("%s-%d", prefix, r.uidCounter)
}
