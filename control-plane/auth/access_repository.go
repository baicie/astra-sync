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
//
// RevokeConsoleSessionsForPrincipal extends the same transactional contract
// to Console session revocation via the API Server surface (ADR-068): the
// session DELETE and the audit row are committed atomically. The returned
// slice is the unique active tenant IDs the principal held at the moment of
// the revoke, used by the caller to emit apiserver_session_revoke_total once
// per tenant.
//
// RevokeSessionsForPrincipal is the admin-CLI counterpart and intentionally
// does not require an audit event: the admin command has no authenticated
// principal. Operators that need an audit trail for offline revocations must
// drive the API Server RPC instead (ADR-068 §"Alternatives Considered").
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
	RevokeConsoleSessionsForPrincipal(
		ctx context.Context, principalID, actorID string,
		audit SecurityAuditEvent,
	) (int64, []string, error)
	WriteSecurityAudit(ctx context.Context, event SecurityAuditEvent) error
}
