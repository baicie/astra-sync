package auth_test

import (
	"context"
	"errors"
	"testing"

	"io.astrasync/control-plane/auth"
)

const tenantID = "8d58d674-7cc7-4b15-a46c-9e7768bbf103"

func TestContextAuthorizerRequiresActiveMembershipPermissionAndRevision(t *testing.T) {
	membership, err := auth.NewMembership(tenantID, true, auth.PermissionConnectorsRead)
	if err != nil {
		t.Fatalf("membership: %v", err)
	}
	ctx, err := auth.WithPrincipal(context.Background(), auth.Principal{
		Subject:        "user-1",
		Active:         true,
		PolicyRevision: "policy-7",
		Memberships:    map[string]auth.Membership{tenantID: membership},
	})
	if err != nil {
		t.Fatalf("principal context: %v", err)
	}
	authorizer := auth.ContextAuthorizer{CurrentPolicyRevision: func(context.Context, string) (string, error) {
		return "policy-7", nil
	}}

	decision, err := authorizer.Authorize(ctx, tenantID, auth.PermissionConnectorsRead)
	if err != nil || decision.Principal.Subject != "user-1" {
		t.Fatalf("authorize: decision=%+v err=%v", decision, err)
	}
	if _, err := authorizer.Authorize(ctx, tenantID, auth.PermissionConnectionsRead); !errors.Is(err, auth.ErrPermissionDenied) {
		t.Fatalf("expected permission denial, got %v", err)
	}
	authorizer.CurrentPolicyRevision = func(context.Context, string) (string, error) { return "policy-8", nil }
	if _, err := authorizer.Authorize(ctx, tenantID, auth.PermissionConnectorsRead); !errors.Is(err, auth.ErrPolicyStale) {
		t.Fatalf("expected stale policy denial, got %v", err)
	}
}

func TestBuiltInRoleAdditionsMatchSlice20Policy(t *testing.T) {
	operator := auth.PermissionsForRole(auth.RoleTenantOperator)
	if !contains(operator, auth.PermissionConnectorsRead) ||
		!contains(operator, auth.PermissionConnectionsRead) ||
		!contains(operator, auth.PermissionConnectionsUse) ||
		!contains(operator, auth.PermissionConnectionsTest) ||
		contains(operator, auth.PermissionConnectionsRotate) {
		t.Fatalf("unexpected tenant_operator permissions: %v", operator)
	}
	administrator := auth.PermissionsForRole(auth.RoleTenantAdmin)
	for _, permission := range auth.AllConnectionPermissions() {
		if !contains(administrator, permission) {
			t.Fatalf("tenant_admin is missing Slice 20 permission %q", permission)
		}
	}
}

func contains(values []auth.Permission, target auth.Permission) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
