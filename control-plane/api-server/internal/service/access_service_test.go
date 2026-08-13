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
	"google.golang.org/protobuf/types/known/emptypb"

	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/api-server/internal/service"
	"io.astrasync/control-plane/auth"
)

const (
	accessTenantID    = "11111111-1111-1111-8111-111111111111"
	accessPrincipalID = "22222222-2222-4222-8222-222222222222"
)

func TestAccessServiceListMembersAuthorizesAndPaginates(t *testing.T) {
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)
	repository := &fakeAccessRepository{
		members: map[string][]auth.TenantMember{
			accessTenantID: {
				{PrincipalID: "p1", Role: auth.RoleTenantAdmin, Status: auth.PrincipalStatusActive},
				{PrincipalID: "p2", Role: auth.RoleTenantViewer, Status: auth.PrincipalStatusActive},
				{PrincipalID: "p3", Role: auth.RoleTenantOperator, Status: auth.PrincipalStatusActive},
			},
		},
	}
	serviceUnderTest, err := service.NewAccessService(repository, auth.DevelopmentAuthorizer{},
		service.WithAccessClock(func() time.Time { return now }),
		service.WithAccessUIDSource(func() string { return "access-uid" }),
	)
	if err != nil {
		t.Fatalf("new access service: %v", err)
	}
	first, err := serviceUnderTest.ListMembers(context.Background(), &controlv1.ListMembersRequest{
		TenantId: accessTenantID, PageSize: 2,
	})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.GetMembers()) != 2 || first.GetNextPageToken() == "" {
		t.Fatalf("unexpected first page: %+v", first)
	}
	if first.GetMembers()[0].GetPrincipalId() != "p1" {
		t.Fatalf("unexpected tenant order: %+v", first.GetMembers())
	}

	second, err := serviceUnderTest.ListMembers(context.Background(), &controlv1.ListMembersRequest{
		TenantId: accessTenantID, PageSize: 2, PageToken: first.GetNextPageToken(),
	})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.GetMembers()) != 1 || second.GetNextPageToken() != "" {
		t.Fatalf("unexpected second page: %+v", second)
	}
	if len(repository.auditWrites) != 2 {
		t.Fatalf("expected two audit writes, got %d", len(repository.auditWrites))
	}
}

func TestAccessServiceGrantTenantRoleWritesMembershipAndAudit(t *testing.T) {
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)
	repository := &fakeAccessRepository{}
	serviceUnderTest, err := service.NewAccessService(repository, auth.DevelopmentAuthorizer{},
		service.WithAccessClock(func() time.Time { return now }),
		service.WithAccessUIDSource(func() string { return "uid-1" }),
	)
	if err != nil {
		t.Fatalf("new access service: %v", err)
	}

	member, err := serviceUnderTest.GrantTenantRole(context.Background(), &controlv1.GrantTenantRoleRequest{
		TenantId: accessTenantID, PrincipalId: accessPrincipalID,
		Role: string(auth.RoleTenantOperator), IdempotencyKey: "abcdefghijklmnop1234567890",
	})
	if err != nil {
		t.Fatalf("grant tenant role: %v", err)
	}
	if member.GetRole() != string(auth.RoleTenantOperator) {
		t.Fatalf("unexpected role: %s", member.GetRole())
	}
	if len(repository.auditWrites) != 1 {
		t.Fatalf("expected one audit write, got %d", len(repository.auditWrites))
	}
	if repository.auditWrites[0].EventType != "access.member.granted" {
		t.Fatalf("unexpected event type: %s", repository.auditWrites[0].EventType)
	}
}

func TestAccessServiceGrantTenantRoleRejectsInvalidIdempotencyKey(t *testing.T) {
	repository := &fakeAccessRepository{}
	serviceUnderTest, err := service.NewAccessService(repository, auth.DevelopmentAuthorizer{})
	if err != nil {
		t.Fatalf("new access service: %v", err)
	}
	if _, err := serviceUnderTest.GrantTenantRole(context.Background(), &controlv1.GrantTenantRoleRequest{
		TenantId: accessTenantID, PrincipalId: accessPrincipalID, Role: string(auth.RoleTenantOperator),
		IdempotencyKey: "short",
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected short idempotency key rejection, got %v", err)
	}
	if _, err := serviceUnderTest.GrantTenantRole(context.Background(), &controlv1.GrantTenantRoleRequest{
		TenantId: accessTenantID, PrincipalId: accessPrincipalID, Role: string(auth.RoleTenantOperator),
		IdempotencyKey: strings.Repeat("a", 129),
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected oversized idempotency key rejection, got %v", err)
	}
}

func TestAccessServiceGrantTenantRoleRejectsUnknownRole(t *testing.T) {
	repository := &fakeAccessRepository{}
	serviceUnderTest, err := service.NewAccessService(repository, auth.DevelopmentAuthorizer{})
	if err != nil {
		t.Fatalf("new access service: %v", err)
	}
	if _, err := serviceUnderTest.GrantTenantRole(context.Background(), &controlv1.GrantTenantRoleRequest{
		TenantId: accessTenantID, PrincipalId: accessPrincipalID,
		Role: "tenant_wizard", IdempotencyKey: "abcdefghijklmnop1234567890",
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected unknown role rejection, got %v", err)
	}
}

func TestAccessServiceRevokeTenantRoleRecordsAudit(t *testing.T) {
	repository := &fakeAccessRepository{}
	serviceUnderTest, err := service.NewAccessService(repository, auth.DevelopmentAuthorizer{},
		service.WithAccessUIDSource(func() string { return "uid-1" }),
	)
	if err != nil {
		t.Fatalf("new access service: %v", err)
	}
	if _, err := serviceUnderTest.RevokeTenantRole(context.Background(), &controlv1.RevokeTenantRoleRequest{
		TenantId: accessTenantID, PrincipalId: accessPrincipalID,
		IdempotencyKey: "abcdefghijklmnop1234567890",
	}); err != nil {
		t.Fatalf("revoke tenant role: %v", err)
	}
	if len(repository.auditWrites) != 1 || repository.auditWrites[0].EventType != "access.member.revoked" {
		t.Fatalf("expected revocation audit, got %+v", repository.auditWrites)
	}
}

func TestAccessServiceListRolesIncludesPlatformAndDescription(t *testing.T) {
	repository := &fakeAccessRepository{}
	serviceUnderTest, err := service.NewAccessService(repository, auth.DevelopmentAuthorizer{})
	if err != nil {
		t.Fatalf("new access service: %v", err)
	}
	principal := auth.Principal{
		ID: identityPrincipalID, Subject: "alice", Active: true, PolicyRevision: "1",
	}
	ctx, err := auth.WithPrincipal(context.Background(), principal)
	if err != nil {
		t.Fatalf("principal context: %v", err)
	}
	response, err := serviceUnderTest.ListRoles(ctx, &controlv1.ListRolesRequest{})
	if err != nil {
		t.Fatalf("list roles: %v", err)
	}
	if len(response.GetRoles()) != 5 {
		t.Fatalf("unexpected role count: %d", len(response.GetRoles()))
	}
	for _, role := range response.GetRoles() {
		if role.GetDescription() == "" {
			t.Fatalf("role %s is missing description", role.GetName())
		}
	}
	last := response.GetRoles()[len(response.GetRoles())-1]
	if last.GetName() != string(auth.RoleTenantViewer) {
		t.Fatalf("unexpected role order: %s", last.GetName())
	}
}

func TestAccessServicePlatformRoleRequiresPlatformAdmin(t *testing.T) {
	repository := &fakeAccessRepository{}
	serviceUnderTest, err := service.NewAccessService(repository, auth.DevelopmentAuthorizer{})
	if err != nil {
		t.Fatalf("new access service: %v", err)
	}
	principal := auth.Principal{
		ID: identityPrincipalID, Subject: "alice", Active: true, PolicyRevision: "1",
	}
	ctx, err := auth.WithPrincipal(context.Background(), principal)
	if err != nil {
		t.Fatalf("principal context: %v", err)
	}
	if _, err := serviceUnderTest.GrantPlatformRole(ctx, &controlv1.GrantPlatformRoleRequest{
		PrincipalId: accessPrincipalID, Role: auth.PlatformRoleAdmin,
		IdempotencyKey: "abcdefghijklmnop1234567890",
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected platform_admin denial, got %v", err)
	}

	platformPrincipal := principal
	platformPrincipal.PlatformAdmin = true
	platformCtx, err := auth.WithPrincipal(context.Background(), platformPrincipal)
	if err != nil {
		t.Fatalf("platform principal context: %v", err)
	}
	grant, err := serviceUnderTest.GrantPlatformRole(platformCtx, &controlv1.GrantPlatformRoleRequest{
		PrincipalId: accessPrincipalID, Role: auth.PlatformRoleAdmin,
		IdempotencyKey: "abcdefghijklmnop1234567890",
	})
	if err != nil {
		t.Fatalf("grant platform role: %v", err)
	}
	if !grant.GetActive() {
		t.Fatalf("grant should have active=true")
	}
	if grant.GetRole() != auth.PlatformRoleAdmin {
		t.Fatalf("unexpected role: %s", grant.GetRole())
	}
	if len(repository.auditWrites) != 1 || repository.auditWrites[0].EventType != "access.platform_role.granted" {
		t.Fatalf("expected platform grant audit, got %+v", repository.auditWrites)
	}
}

func TestAccessServicePlatformRoleRejectsUnknownRole(t *testing.T) {
	repository := &fakeAccessRepository{}
	serviceUnderTest, err := service.NewAccessService(repository, auth.DevelopmentAuthorizer{})
	if err != nil {
		t.Fatalf("new access service: %v", err)
	}
	principal := auth.Principal{
		ID: identityPrincipalID, Subject: "alice", Active: true, PolicyRevision: "1",
		PlatformAdmin: true,
	}
	ctx, err := auth.WithPrincipal(context.Background(), principal)
	if err != nil {
		t.Fatalf("principal context: %v", err)
	}
	if _, err := serviceUnderTest.GrantPlatformRole(ctx, &controlv1.GrantPlatformRoleRequest{
		PrincipalId: accessPrincipalID, Role: "tenant_admin",
		IdempotencyKey: "abcdefghijklmnop1234567890",
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected unknown role rejection, got %v", err)
	}
}

func TestAccessServiceRevokePlatformRoleRequiresPlatformAdmin(t *testing.T) {
	repository := &fakeAccessRepository{}
	serviceUnderTest, err := service.NewAccessService(repository, auth.DevelopmentAuthorizer{})
	if err != nil {
		t.Fatalf("new access service: %v", err)
	}
	principal := auth.Principal{
		ID: identityPrincipalID, Subject: "alice", Active: true, PolicyRevision: "1",
	}
	ctx, err := auth.WithPrincipal(context.Background(), principal)
	if err != nil {
		t.Fatalf("principal context: %v", err)
	}
	if _, err := serviceUnderTest.RevokePlatformRole(ctx, &controlv1.RevokePlatformRoleRequest{
		PrincipalId: accessPrincipalID, Role: auth.PlatformRoleAdmin,
		IdempotencyKey: "abcdefghijklmnop1234567890",
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected platform_admin denial, got %v", err)
	}
}

func TestAccessServiceRepositoryErrorsAreSanitized(t *testing.T) {
	repository := &fakeAccessRepository{grantErr: errors.New("postgres detail must remain private")}
	serviceUnderTest, err := service.NewAccessService(repository, auth.DevelopmentAuthorizer{})
	if err != nil {
		t.Fatalf("new access service: %v", err)
	}
	if _, err := serviceUnderTest.GrantTenantRole(context.Background(), &controlv1.GrantTenantRoleRequest{
		TenantId: accessTenantID, PrincipalId: accessPrincipalID,
		Role: string(auth.RoleTenantOperator), IdempotencyKey: "abcdefghijklmnop1234567890",
	}); status.Code(err) != codes.Internal || strings.Contains(status.Convert(err).Message(), "postgres detail") {
		t.Fatalf("expected sanitized repository error, got %v", err)
	}
}

func TestAccessServiceAuditFailureIsInternal(t *testing.T) {
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)
	repository := &fakeAccessRepository{auditErr: errors.New("audit unavailable")}
	serviceUnderTest, err := service.NewAccessService(repository, auth.DevelopmentAuthorizer{},
		service.WithAccessClock(func() time.Time { return now }),
		service.WithAccessUIDSource(func() string { return "uid-1" }),
	)
	if err != nil {
		t.Fatalf("new access service: %v", err)
	}
	if _, err := serviceUnderTest.GrantTenantRole(context.Background(), &controlv1.GrantTenantRoleRequest{
		TenantId: accessTenantID, PrincipalId: accessPrincipalID,
		Role: string(auth.RoleTenantOperator), IdempotencyKey: "abcdefghijklmnop1234567890",
	}); status.Code(err) != codes.Internal {
		t.Fatalf("expected audit failure to be internal, got %v", err)
	}
}

func TestAccessServiceRevokeTenantRoleEmptyResponse(t *testing.T) {
	repository := &fakeAccessRepository{}
	serviceUnderTest, err := service.NewAccessService(repository, auth.DevelopmentAuthorizer{})
	if err != nil {
		t.Fatalf("new access service: %v", err)
	}
	response, err := serviceUnderTest.RevokeTenantRole(context.Background(), &controlv1.RevokeTenantRoleRequest{
		TenantId: accessTenantID, PrincipalId: accessPrincipalID,
		IdempotencyKey: "abcdefghijklmnop1234567890",
	})
	if err != nil {
		t.Fatalf("revoke tenant role: %v", err)
	}
	if _, ok := interface{}(response).(*emptypb.Empty); !ok {
		t.Fatalf("expected empty response, got %T", response)
	}
}

func TestAccessServiceTransactionIsAtomicOnAuditFailure(t *testing.T) {
	// The fake repository reports a successful grant only if the audit write
	// succeeds. If the audit fails, both the membership mutation and the
	// audit row are dropped together so the platform never observes a
	// mutation that was not audited.
	repository := &fakeAccessRepository{auditErr: errors.New("audit database unavailable")}
	serviceUnderTest, err := service.NewAccessService(repository, auth.DevelopmentAuthorizer{},
		service.WithAccessUIDSource(func() string { return "uid-tx" }),
	)
	if err != nil {
		t.Fatalf("new access service: %v", err)
	}
	if _, err := serviceUnderTest.GrantTenantRole(context.Background(), &controlv1.GrantTenantRoleRequest{
		TenantId: accessTenantID, PrincipalId: accessPrincipalID,
		Role: string(auth.RoleTenantOperator), IdempotencyKey: "abcdefghijklmnop1234567890",
	}); status.Code(err) != codes.Internal {
		t.Fatalf("expected audit failure to be internal, got %v", err)
	}
	if len(repository.auditWrites) != 0 {
		t.Fatalf("audit failure should leave no audit rows: %+v", repository.auditWrites)
	}
}

func TestAccessServiceTransactionEmitsExactlyOneAuditPerMutation(t *testing.T) {
	repository := &fakeAccessRepository{}
	serviceUnderTest, err := service.NewAccessService(repository, auth.DevelopmentAuthorizer{},
		service.WithAccessUIDSource(func() string { return "uid-tx" }),
	)
	if err != nil {
		t.Fatalf("new access service: %v", err)
	}
	if _, err := serviceUnderTest.GrantTenantRole(context.Background(), &controlv1.GrantTenantRoleRequest{
		TenantId: accessTenantID, PrincipalId: accessPrincipalID,
		Role: string(auth.RoleTenantOperator), IdempotencyKey: "abcdefghijklmnop1234567890",
	}); err != nil {
		t.Fatalf("grant tenant role: %v", err)
	}
	if _, err := serviceUnderTest.RevokeTenantRole(context.Background(), &controlv1.RevokeTenantRoleRequest{
		TenantId: accessTenantID, PrincipalId: accessPrincipalID,
		IdempotencyKey: "qrstuvwxyz1234567890abcdef",
	}); err != nil {
		t.Fatalf("revoke tenant role: %v", err)
	}
	if _, err := serviceUnderTest.GrantPlatformRole(context.Background(), &controlv1.GrantPlatformRoleRequest{
		PrincipalId: accessPrincipalID, Role: auth.PlatformRoleAdmin,
		IdempotencyKey: "mnopqrstuv1234567890abcdefgh",
	}); err == nil {
		t.Fatalf("expected platform admin denial, got nil")
	}
	if _, err := serviceUnderTest.RevokePlatformRole(context.Background(), &controlv1.RevokePlatformRoleRequest{
		PrincipalId: accessPrincipalID, Role: auth.PlatformRoleAdmin,
		IdempotencyKey: "ijklmnopqr1234567890abcdefgh",
	}); err == nil {
		t.Fatalf("expected platform admin denial, got nil")
	}
	if len(repository.auditWrites) != 2 {
		t.Fatalf("expected exactly two transactional audit writes (grant + revoke), got %d", len(repository.auditWrites))
	}
	grantAudit := repository.auditWrites[0]
	if grantAudit.EventType != "access.member.granted" || grantAudit.Outcome != "CHANGED" {
		t.Fatalf("unexpected grant audit: %+v", grantAudit)
	}
	revokeAudit := repository.auditWrites[1]
	if revokeAudit.EventType != "access.member.revoked" || revokeAudit.Outcome != "CHANGED" {
		t.Fatalf("unexpected revoke audit: %+v", revokeAudit)
	}
}

type fakeAccessRepository struct {
	mu            sync.Mutex
	members       map[string][]auth.TenantMember
	auditWrites   []auth.SecurityAuditEvent
	grantErr      error
	revokeErr     error
	auditErr      error
	platformGrant auth.PlatformRoleGrant
	platformErr   error
}

func (r *fakeAccessRepository) ResolvePrincipalByID(_ context.Context, principalID string) (auth.Principal, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return auth.Principal{ID: principalID, Subject: "session", Active: true, PolicyRevision: "1"}, nil
}

func (r *fakeAccessRepository) ReadTenant(_ context.Context, tenantID string) (auth.TenantView, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return auth.TenantView{TenantID: tenantID, Status: auth.TenantStatusActive, AuthzRevision: 1}, nil
}

func (r *fakeAccessRepository) LoadTenantMembers(_ context.Context, tenantID string) ([]auth.TenantMember, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.grantErr != nil {
		return nil, r.grantErr
	}
	members := append([]auth.TenantMember(nil), r.members[tenantID]...)
	return members, nil
}

func (r *fakeAccessRepository) GrantTenantRole(
	_ context.Context, tenantID, principalID string, role auth.Role, actorID string,
	audit auth.SecurityAuditEvent,
) (auth.TenantView, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.grantErr != nil {
		return auth.TenantView{}, r.grantErr
	}
	if r.auditErr != nil {
		return auth.TenantView{}, r.auditErr
	}
	if audit.EventID != "" {
		r.auditWrites = append(r.auditWrites, audit)
	}
	return auth.TenantView{
		TenantID: tenantID, Status: auth.TenantStatusActive, AuthzRevision: 2,
		Members: []auth.TenantMember{{PrincipalID: principalID, Role: role, Status: auth.PrincipalStatusActive}},
	}, nil
}

func (r *fakeAccessRepository) RevokeTenantRole(
	_ context.Context, tenantID, principalID, actorID string,
	audit auth.SecurityAuditEvent,
) (auth.TenantView, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.revokeErr != nil {
		return auth.TenantView{}, r.revokeErr
	}
	if r.auditErr != nil {
		return auth.TenantView{}, r.auditErr
	}
	if audit.EventID != "" {
		r.auditWrites = append(r.auditWrites, audit)
	}
	return auth.TenantView{
		TenantID: tenantID, Status: auth.TenantStatusActive, AuthzRevision: 3,
		Members: []auth.TenantMember{{PrincipalID: principalID, Role: auth.RoleTenantAdmin, Status: auth.PrincipalStatusDisabled}},
	}, nil
}

func (r *fakeAccessRepository) GrantPlatformRole(
	_ context.Context, principalID string, role string, actorID string,
	audit auth.SecurityAuditEvent,
) (auth.PlatformRoleGrant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.platformErr != nil {
		return auth.PlatformRoleGrant{}, r.platformErr
	}
	if r.auditErr != nil {
		return auth.PlatformRoleGrant{}, r.auditErr
	}
	if audit.EventID != "" {
		r.auditWrites = append(r.auditWrites, audit)
	}
	r.platformGrant = auth.PlatformRoleGrant{
		PrincipalID: principalID, Role: role, Active: true,
		GrantedAt: time.Now().UTC(), GrantedBy: actorID,
	}
	return r.platformGrant, nil
}

func (r *fakeAccessRepository) RevokePlatformRole(
	_ context.Context, principalID string, role string, actorID string,
	audit auth.SecurityAuditEvent,
) (auth.PlatformRoleGrant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.platformErr != nil {
		return auth.PlatformRoleGrant{}, r.platformErr
	}
	if r.auditErr != nil {
		return auth.PlatformRoleGrant{}, r.auditErr
	}
	if audit.EventID != "" {
		r.auditWrites = append(r.auditWrites, audit)
	}
	r.platformGrant = auth.PlatformRoleGrant{
		PrincipalID: principalID, Role: role, Active: false,
		GrantedAt: time.Now().UTC(), GrantedBy: actorID,
	}
	return r.platformGrant, nil
}

func (r *fakeAccessRepository) WriteSecurityAudit(_ context.Context, event auth.SecurityAuditEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.auditErr != nil {
		return r.auditErr
	}
	r.auditWrites = append(r.auditWrites, event)
	return nil
}

func nextAuditUID(prefix string, counter int) string {
	return fmt.Sprintf("%s-%d", prefix, counter)
}
