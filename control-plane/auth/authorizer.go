package auth

import (
	"context"
	"errors"
	"fmt"
)

type Decision struct {
	Principal      Principal
	Membership     Membership
	TenantID       string
	Permission     Permission
	PolicyRevision string
}

// DevelopmentAuthorizer is an explicit non-production compatibility boundary.
// Production startup must never select it.
type DevelopmentAuthorizer struct{}

func (DevelopmentAuthorizer) Authorize(
	_ context.Context, tenantID string, permission Permission,
) (Decision, error) {
	if !tenantIDPattern.MatchString(tenantID) {
		return Decision{}, ErrTenantUnavailable
	}
	if permission == "" {
		return Decision{}, fmt.Errorf("permission must not be blank")
	}
	membership, _ := NewMembership(tenantID, true, permission)
	return Decision{
		Principal: Principal{
			ID: "development", Subject: "development", Active: true, PolicyRevision: "development",
		},
		Membership: membership, TenantID: tenantID, Permission: permission, PolicyRevision: "development",
	}, nil
}

type Authorizer interface {
	Authorize(context.Context, string, Permission) (Decision, error)
}

type ContextAuthorizer struct {
	CurrentPolicyRevision func(context.Context, string) (string, error)
}

func (a ContextAuthorizer) Authorize(
	ctx context.Context, tenantID string, permission Permission,
) (Decision, error) {
	principal, found := PrincipalFromContext(ctx)
	if !found || !principal.Active {
		return Decision{}, ErrUnauthenticated
	}
	membership, found := principal.Memberships[tenantID]
	if !found || !membership.Active {
		return Decision{}, ErrTenantUnavailable
	}
	if !membership.Has(permission) {
		return Decision{}, ErrPermissionDenied
	}
	if a.CurrentPolicyRevision != nil {
		current, err := a.CurrentPolicyRevision(ctx, tenantID)
		if err != nil {
			return Decision{}, errors.Join(ErrPermissionDenied, err)
		}
		expectedRevision := membership.PolicyRevision
		if expectedRevision == "" {
			expectedRevision = principal.PolicyRevision
		}
		if current == "" || current != expectedRevision {
			return Decision{}, ErrPolicyStale
		}
	}
	return Decision{
		Principal:      principal,
		Membership:     membership,
		TenantID:       tenantID,
		Permission:     permission,
		PolicyRevision: policyRevision(principal, membership),
	}, nil
}

func policyRevision(principal Principal, membership Membership) string {
	if membership.PolicyRevision != "" {
		return membership.PolicyRevision
	}
	return principal.PolicyRevision
}
