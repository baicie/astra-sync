package auth

import "context"

// AccessRepository captures the persistence operations the AccessService needs
// to manage tenant membership and platform role grants. It is intentionally
// separate from IdentityResolver so callers that only need session/identity
// resolution do not pull in the wider administrative surface.
//
// The grant and revoke methods combine the membership mutation with the audit
// event write in a single PostgreSQL transaction. A failure to commit either
// the data change or the audit row rolls back the entire unit so a denial,
// crash, or storage failure cannot leave the platform in an inconsistent state.
type AccessRepository interface {
	ResolvePrincipalByID(ctx context.Context, principalID string) (Principal, error)
	ReadTenant(ctx context.Context, tenantID string) (TenantView, error)
	LoadTenantMembers(ctx context.Context, tenantID string) ([]TenantMember, error)
	GrantTenantRole(
		ctx context.Context, tenantID, principalID string, role Role,
		actorID string, audit SecurityAuditEvent,
	) (TenantView, error)
	RevokeTenantRole(
		ctx context.Context, tenantID, principalID, actorID string,
		audit SecurityAuditEvent,
	) (TenantView, error)
	GrantPlatformRole(
		ctx context.Context, principalID string, role string,
		actorID string, audit SecurityAuditEvent,
	) (PlatformRoleGrant, error)
	RevokePlatformRole(
		ctx context.Context, principalID string, role string,
		actorID string, audit SecurityAuditEvent,
	) (PlatformRoleGrant, error)
	WriteSecurityAudit(ctx context.Context, event SecurityAuditEvent) error
}
