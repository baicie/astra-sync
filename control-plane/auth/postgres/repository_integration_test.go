package postgres_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"io.astrasync/control-plane/auth"
	authpostgres "io.astrasync/control-plane/auth/postgres"
)

func TestRepositoryMaterializesOIDCIdentityAndTenantMembership(t *testing.T) {
	dataSourceName := os.Getenv("ASTRASYNC_TEST_POSTGRES_URL")
	if dataSourceName == "" {
		t.Skip("ASTRASYNC_TEST_POSTGRES_URL is not configured")
	}
	ctx := context.Background()
	repository, err := authpostgres.Open(ctx, dataSourceName)
	if err != nil {
		t.Fatalf("open auth repository: %v", err)
	}
	defer repository.Close()
	if err := repository.Migrate(ctx); err != nil {
		t.Fatalf("migrate auth repository: %v", err)
	}
	tenantID := uuid.NewString()
	identity := auth.ExternalIdentity{Issuer: "https://issuer.example/" + uuid.NewString(), Subject: "operator-1"}
	if err := repository.BootstrapTenant(ctx, tenantID, "tenant-"+tenantID[:8], "Integration tenant", identity); err != nil {
		t.Fatalf("bootstrap tenant: %v", err)
	}
	principal, err := repository.ResolveOrCreatePrincipal(ctx, identity)
	if err != nil || !principal.Active || len(principal.Memberships) != 1 {
		t.Fatalf("resolve principal: principal=%+v err=%v", principal, err)
	}
	membership := principal.Memberships[tenantID]
	if !membership.Active || !membership.Has(auth.PermissionConnectionsCreate) || membership.PolicyRevision != "1" {
		t.Fatalf("unexpected tenant administrator membership: %+v", membership)
	}
	if revision, err := repository.CurrentPolicyRevision(ctx, tenantID); err != nil || revision != "1" {
		t.Fatalf("read policy revision: revision=%q err=%v", revision, err)
	}
	auditOccurredAt := time.Now().UTC()
	if err := repository.WriteSecurityAudit(ctx, auth.SecurityAuditEvent{
		EventID: uuid.NewString(), EventType: "authorization.denied", ActorID: principal.ID,
		TenantID: tenantID, RequestID: uuid.NewString(), Outcome: "PERMISSION_DENIED",
		Attributes: map[string]any{"method": "/test.Service/Denied"}, OccurredAt: auditOccurredAt,
	}); err != nil {
		t.Fatalf("write security audit: %v", err)
	}
	if err := repository.WriteSecurityAudit(ctx, auth.SecurityAuditEvent{
		EventID: uuid.NewString(), EventType: "job.start", ActorID: principal.ID,
		TenantID: tenantID, RequestID: uuid.NewString(), Outcome: "CHANGED",
		Attributes: map[string]any{"name": "orders", "afterVersion": 2}, OccurredAt: auditOccurredAt.Add(-time.Second),
	}); err != nil {
		t.Fatalf("write older security audit: %v", err)
	}
	if err := repository.WriteSecurityAudit(ctx, auth.SecurityAuditEvent{
		EventID: uuid.NewString(), EventType: "job.start", ActorID: principal.ID,
		TenantID: uuid.NewString(), RequestID: uuid.NewString(), Outcome: "CHANGED",
		OccurredAt: auditOccurredAt.Add(time.Second),
	}); err != nil {
		t.Fatalf("write cross-tenant security audit: %v", err)
	}
	auditQuery := auth.SecurityAuditQuery{
		TenantID: tenantID, OccurredAfter: auditOccurredAt.Add(-time.Minute),
		OccurredBefore: auditOccurredAt.Add(time.Minute), Limit: 1,
	}
	firstAuditPage, err := repository.ListSecurityAudit(ctx, auditQuery)
	if err != nil || len(firstAuditPage) != 1 || firstAuditPage[0].EventType != "authorization.denied" {
		t.Fatalf("read first tenant audit page: events=%+v err=%v", firstAuditPage, err)
	}
	auditQuery.Cursor = &auth.SecurityAuditCursor{
		OccurredAt: firstAuditPage[0].OccurredAt, EventID: firstAuditPage[0].EventID,
	}
	secondAuditPage, err := repository.ListSecurityAudit(ctx, auditQuery)
	if err != nil || len(secondAuditPage) != 1 || secondAuditPage[0].EventType != "job.start" ||
		secondAuditPage[0].TenantID != tenantID {
		t.Fatalf("read second tenant audit page: events=%+v err=%v", secondAuditPage, err)
	}
	filteredAudit, err := repository.ListSecurityAudit(ctx, auth.SecurityAuditQuery{
		TenantID: tenantID, OccurredAfter: auditOccurredAt.Add(-time.Minute),
		OccurredBefore: auditOccurredAt.Add(time.Minute), EventTypes: []string{"job.start"},
		Outcomes: []string{"CHANGED"}, Limit: 2,
	})
	if err != nil || len(filteredAudit) != 1 || filteredAudit[0].Attributes["name"] != "orders" {
		t.Fatalf("read filtered tenant audit: events=%+v err=%v", filteredAudit, err)
	}
	tiePrefix := uuid.NewString()
	for _, eventID := range []string{tiePrefix + "-a", tiePrefix + "-z"} {
		if err := repository.WriteSecurityAudit(ctx, auth.SecurityAuditEvent{
			EventID: eventID, EventType: "audit.keyset", ActorID: principal.ID,
			TenantID: tenantID, RequestID: uuid.NewString(), Outcome: "CHANGED",
			OccurredAt: auditOccurredAt.Add(-30 * time.Second),
		}); err != nil {
			t.Fatalf("write same-time security audit: %v", err)
		}
	}
	tieQuery := auth.SecurityAuditQuery{
		TenantID: tenantID, OccurredAfter: auditOccurredAt.Add(-time.Minute),
		OccurredBefore: auditOccurredAt.Add(time.Minute), EventTypes: []string{"audit.keyset"}, Limit: 1,
	}
	tieFirst, err := repository.ListSecurityAudit(ctx, tieQuery)
	if err != nil || len(tieFirst) != 1 || tieFirst[0].EventID != tiePrefix+"-z" {
		t.Fatalf("read same-time first audit page: events=%+v err=%v", tieFirst, err)
	}
	tieQuery.Cursor = &auth.SecurityAuditCursor{OccurredAt: tieFirst[0].OccurredAt, EventID: tieFirst[0].EventID}
	tieSecond, err := repository.ListSecurityAudit(ctx, tieQuery)
	if err != nil || len(tieSecond) != 1 || tieSecond[0].EventID != tiePrefix+"-a" {
		t.Fatalf("read same-time second audit page: events=%+v err=%v", tieSecond, err)
	}

	unassigned, err := repository.ResolveOrCreatePrincipal(ctx, auth.ExternalIdentity{
		Issuer: identity.Issuer, Subject: "unassigned-operator",
	})
	if err != nil || !unassigned.Active || len(unassigned.Memberships) != 0 {
		t.Fatalf("new identity must remain unassigned: principal=%+v err=%v", unassigned, err)
	}

	store, err := authpostgres.NewSessionStore(repository, bytes.Repeat([]byte{0x4a}, 32))
	if err != nil {
		t.Fatalf("create Console session store: %v", err)
	}
	login, err := store.CreateLoginTransaction(ctx, auth.LoginTransaction{
		Nonce: uuid.NewString(), CodeVerifier: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ",
		ReturnTo: "/connections",
	}, 5*time.Minute)
	if err != nil {
		t.Fatalf("create login transaction: %v", err)
	}
	transaction, err := store.ConsumeLoginTransaction(ctx, login.State, login.BrowserBinding)
	if err != nil || transaction.ReturnTo != "/connections" {
		t.Fatalf("consume login transaction: transaction=%+v err=%v", transaction, err)
	}
	if _, err := store.ConsumeLoginTransaction(ctx, login.State, login.BrowserBinding); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("expected one-time login transaction, got %v", err)
	}

	accessSentinel := "access-token-" + uuid.NewString()
	refreshSentinel := "refresh-token-" + uuid.NewString()
	credentials, err := store.CreateConsoleSession(ctx, principal.ID, auth.ConsoleTokens{
		AccessToken: accessSentinel, RefreshToken: refreshSentinel, TokenType: "Bearer",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}, 15*time.Minute, 2*time.Hour)
	if err != nil {
		t.Fatalf("create Console session: %v", err)
	}
	session, err := store.ResolveConsoleSession(ctx, credentials.SessionID, 15*time.Minute)
	if err != nil || session.PrincipalID != principal.ID || session.Tokens.AccessToken != accessSentinel ||
		session.CSRFToken != credentials.CSRFToken {
		t.Fatalf("resolve Console session: session=%+v err=%v", session, err)
	}
	database, err := sql.Open("pgx", dataSourceName)
	if err != nil {
		t.Fatalf("open session inspection database: %v", err)
	}
	defer database.Close()
	var encrypted []byte
	if err := database.QueryRowContext(ctx,
		`SELECT encrypted_tokens FROM astrasync_auth_sessions WHERE principal_id = $1::uuid`, principal.ID,
	).Scan(&encrypted); err != nil {
		t.Fatalf("read encrypted session envelope: %v", err)
	}
	if bytes.Contains(encrypted, []byte(accessSentinel)) || bytes.Contains(encrypted, []byte(refreshSentinel)) {
		t.Fatal("session envelope persisted plaintext token material")
	}
	updatedAccess := "updated-access-token-" + uuid.NewString()
	revision, err := store.UpdateConsoleSessionTokens(ctx, credentials.SessionID, principal.ID, session.Revision, auth.ConsoleTokens{
		AccessToken: updatedAccess, RefreshToken: refreshSentinel, TokenType: "Bearer",
		ExpiresAt: time.Now().UTC().Add(90 * time.Minute),
	})
	if err != nil || revision != session.Revision+1 {
		t.Fatalf("refresh Console session: revision=%d err=%v", revision, err)
	}
	if _, err := store.UpdateConsoleSessionTokens(ctx, credentials.SessionID, principal.ID, session.Revision, session.Tokens); !errors.Is(err, auth.ErrSessionConflict) {
		t.Fatalf("expected stale session refresh fencing, got %v", err)
	}
	if err := store.DeleteConsoleSession(ctx, credentials.SessionID); err != nil {
		t.Fatalf("delete Console session: %v", err)
	}
	if _, err := store.ResolveConsoleSession(ctx, credentials.SessionID, 15*time.Minute); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("expected deleted session rejection, got %v", err)
	}
}

func TestRepositoryAccessMutationsAreTransactional(t *testing.T) {
	dataSourceName := os.Getenv("ASTRASYNC_TEST_POSTGRES_URL")
	if dataSourceName == "" {
		t.Skip("ASTRASYNC_TEST_POSTGRES_URL is not configured")
	}
	ctx := context.Background()
	repository, err := authpostgres.Open(ctx, dataSourceName)
	if err != nil {
		t.Fatalf("open auth repository: %v", err)
	}
	defer repository.Close()
	if err := repository.Migrate(ctx); err != nil {
		t.Fatalf("migrate auth repository: %v", err)
	}
	tenantID := uuid.NewString()
	adminIdentity := auth.ExternalIdentity{Issuer: "https://issuer.example/" + uuid.NewString(), Subject: "admin"}
	if err := repository.BootstrapTenant(ctx, tenantID, "tenant-"+tenantID[:8], "Access mutation tenant", adminIdentity); err != nil {
		t.Fatalf("bootstrap tenant: %v", err)
	}
	adminPrincipal, err := repository.ResolveOrCreatePrincipal(ctx, adminIdentity)
	if err != nil {
		t.Fatalf("resolve admin principal: %v", err)
	}
	memberIdentity := auth.ExternalIdentity{Issuer: "https://issuer.example/" + uuid.NewString(), Subject: "member"}
	memberPrincipal, err := repository.ResolveOrCreatePrincipal(ctx, memberIdentity)
	if err != nil {
		t.Fatalf("resolve member principal: %v", err)
	}

	auditEventFor := func(eventType, principalID, role string, revision int64) auth.SecurityAuditEvent {
		return auth.SecurityAuditEvent{
			EventID:    uuid.NewString(),
			EventType:  eventType,
			ActorID:    adminPrincipal.ID,
			TenantID:   tenantID,
			RequestID:  uuid.NewString(),
			Outcome:    "CHANGED",
			Attributes: map[string]any{"principalId": principalID, "role": role, "authzRevision": revision},
			OccurredAt: time.Now().UTC(),
		}
	}

	// Grant a tenant role and check that the membership is materialised.
	grantedView, err := repository.GrantTenantRole(
		ctx, tenantID, memberPrincipal.ID, auth.RoleTenantOperator, adminPrincipal.ID,
		auditEventFor("access.member.granted", memberPrincipal.ID, string(auth.RoleTenantOperator), 0),
	)
	if err != nil {
		t.Fatalf("grant tenant role: %v", err)
	}
	if grantedView.AuthzRevision <= 1 {
		t.Fatalf("expected authz_revision to bump after grant, got %d", grantedView.AuthzRevision)
	}
	members, err := repository.LoadTenantMembers(ctx, tenantID)
	if err != nil {
		t.Fatalf("load tenant members: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("expected two members (admin + member), got %d", len(members))
	}

	// Grant the same role again — idempotent and revision should bump.
	secondView, err := repository.GrantTenantRole(
		ctx, tenantID, memberPrincipal.ID, auth.RoleTenantOperator, adminPrincipal.ID,
		auditEventFor("access.member.granted", memberPrincipal.ID, string(auth.RoleTenantOperator), 0),
	)
	if err != nil {
		t.Fatalf("grant tenant role again: %v", err)
	}
	if secondView.AuthzRevision <= grantedView.AuthzRevision {
		t.Fatalf("expected authz_revision to bump on idempotent grant, got %d -> %d", grantedView.AuthzRevision, secondView.AuthzRevision)
	}

	// Promote the same principal to tenant_admin and verify role change.
	adminView, err := repository.GrantTenantRole(
		ctx, tenantID, memberPrincipal.ID, auth.RoleTenantAdmin, adminPrincipal.ID,
		auditEventFor("access.member.granted", memberPrincipal.ID, string(auth.RoleTenantAdmin), 0),
	)
	if err != nil {
		t.Fatalf("grant tenant admin role: %v", err)
	}
	var foundRole string
	for _, member := range adminView.Members {
		if member.PrincipalID == memberPrincipal.ID {
			foundRole = string(member.Role)
		}
	}
	if foundRole != string(auth.RoleTenantAdmin) {
		t.Fatalf("expected role to be %s, got %s", auth.RoleTenantAdmin, foundRole)
	}

	// Revoke the membership and verify it is disabled.
	revokedView, err := repository.RevokeTenantRole(
		ctx, tenantID, memberPrincipal.ID, adminPrincipal.ID,
		auditEventFor("access.member.revoked", memberPrincipal.ID, "", 0),
	)
	if err != nil {
		t.Fatalf("revoke tenant role: %v", err)
	}
	if revokedView.AuthzRevision <= adminView.AuthzRevision {
		t.Fatalf("expected authz_revision to bump on revoke, got %d -> %d", adminView.AuthzRevision, revokedView.AuthzRevision)
	}
	if len(revokedView.Members) != 2 {
		t.Fatalf("expected both memberships to remain observable after revoke, got %d", len(revokedView.Members))
	}

	// Grant and revoke platform_admin role for the member principal.
	grant, err := repository.GrantPlatformRole(ctx, memberPrincipal.ID, auth.PlatformRoleAdmin, adminPrincipal.ID,
		auditEventFor("access.platform_role.granted", memberPrincipal.ID, auth.PlatformRoleAdmin, 0))
	if err != nil {
		t.Fatalf("grant platform role: %v", err)
	}
	if !grant.Active {
		t.Fatalf("platform role grant should be active")
	}
	revoke, err := repository.RevokePlatformRole(ctx, memberPrincipal.ID, auth.PlatformRoleAdmin, adminPrincipal.ID,
		auditEventFor("access.platform_role.revoked", memberPrincipal.ID, auth.PlatformRoleAdmin, 0))
	if err != nil {
		t.Fatalf("revoke platform role: %v", err)
	}
	if revoke.Active {
		t.Fatalf("platform role revoke should be inactive")
	}

	// Audit events written by the transactional grant/revoke paths must be
	// queryable through the audit reader; this verifies the transaction
	// committed both the data change and the audit row atomically.
	now := time.Now().UTC()
	auditWindow, err := repository.ListSecurityAudit(ctx, auth.SecurityAuditQuery{
		TenantID:       tenantID,
		OccurredAfter:  now.Add(-5 * time.Minute),
		OccurredBefore: now.Add(5 * time.Minute),
		Limit:          20,
	})
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	if len(auditWindow) < 4 {
		t.Fatalf("expected audit events from the transactional grant/revoke, got %d", len(auditWindow))
	}

	// Invalid input rejection paths.
	badAudit := auth.SecurityAuditEvent{} // empty event is invalid
	if _, err := repository.GrantTenantRole(ctx, "not-a-uuid", memberPrincipal.ID, auth.RoleTenantOperator, adminPrincipal.ID, badAudit); !errors.Is(err, auth.ErrTenantUnavailable) {
		t.Fatalf("expected tenant uuid rejection, got %v", err)
	}
	if _, err := repository.GrantTenantRole(ctx, tenantID, "not-a-uuid", auth.RoleTenantOperator, adminPrincipal.ID, badAudit); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("expected principal uuid rejection, got %v", err)
	}
	if _, err := repository.GrantTenantRole(ctx, tenantID, memberPrincipal.ID, auth.Role("tenant_wizard"), adminPrincipal.ID, badAudit); err == nil {
		t.Fatalf("expected unsupported tenant role rejection")
	}
	if _, err := repository.GrantPlatformRole(ctx, memberPrincipal.ID, "tenant_admin", adminPrincipal.ID, badAudit); err == nil {
		t.Fatalf("expected unsupported platform role rejection")
	}
}
