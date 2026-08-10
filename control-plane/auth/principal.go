package auth

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var tenantIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

var (
	ErrUnauthenticated   = errors.New("principal is not authenticated")
	ErrPermissionDenied  = errors.New("permission denied")
	ErrTenantUnavailable = errors.New("tenant is unavailable")
	ErrPolicyStale       = errors.New("authorization policy is stale")
)

type Principal struct {
	ID             string
	Issuer         string
	Subject        string
	Active         bool
	Service        bool
	PlatformAdmin  bool
	PolicyRevision string
	Memberships    map[string]Membership
}

type Membership struct {
	TenantID          string
	TenantNamespace   string
	TenantDisplayName string
	Role              Role
	Active            bool
	PolicyRevision    string
	Permissions       map[Permission]struct{}
}

func NewMembership(tenantID string, active bool, permissions ...Permission) (Membership, error) {
	if !tenantIDPattern.MatchString(tenantID) {
		return Membership{}, fmt.Errorf("tenant ID must be a canonical UUID")
	}
	allowed := make(map[Permission]struct{}, len(permissions))
	for _, permission := range permissions {
		if strings.TrimSpace(string(permission)) == "" {
			return Membership{}, fmt.Errorf("permission must not be blank")
		}
		allowed[permission] = struct{}{}
	}
	return Membership{TenantID: tenantID, Active: active, Permissions: allowed}, nil
}

func (m Membership) Has(permission Permission) bool {
	_, allowed := m.Permissions[permission]
	return allowed
}

func (m Membership) PermissionList() []Permission {
	result := make([]Permission, 0, len(m.Permissions))
	for permission := range m.Permissions {
		result = append(result, permission)
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result
}

type principalContextKey struct{}

func WithPrincipal(ctx context.Context, principal Principal) (context.Context, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context must not be nil")
	}
	if strings.TrimSpace(principal.Subject) == "" || strings.TrimSpace(principal.PolicyRevision) == "" {
		return nil, fmt.Errorf("principal subject and policy revision are required")
	}
	copyPrincipal := principal
	copyPrincipal.Memberships = make(map[string]Membership, len(principal.Memberships))
	for tenantID, membership := range principal.Memberships {
		if tenantID != membership.TenantID || !tenantIDPattern.MatchString(tenantID) {
			return nil, fmt.Errorf("principal contains an invalid tenant membership")
		}
		copyMembership := membership
		copyMembership.Permissions = make(map[Permission]struct{}, len(membership.Permissions))
		for permission := range membership.Permissions {
			copyMembership.Permissions[permission] = struct{}{}
		}
		copyPrincipal.Memberships[tenantID] = copyMembership
	}
	return context.WithValue(ctx, principalContextKey{}, copyPrincipal), nil
}

func (p Principal) MembershipForScope(scope string) (Membership, bool) {
	if membership, found := p.Memberships[scope]; found {
		return membership, true
	}
	for _, membership := range p.Memberships {
		if membership.TenantNamespace == scope {
			return membership, true
		}
	}
	return Membership{}, false
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	if ctx == nil {
		return Principal{}, false
	}
	principal, found := ctx.Value(principalContextKey{}).(Principal)
	return principal, found
}
