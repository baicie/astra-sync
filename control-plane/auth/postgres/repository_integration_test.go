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
	if err := repository.WriteSecurityAudit(ctx, auth.SecurityAuditEvent{
		EventID: uuid.NewString(), EventType: "authorization.denied", ActorID: principal.ID,
		TenantID: tenantID, RequestID: uuid.NewString(), Outcome: "PERMISSION_DENIED",
		Attributes: map[string]any{"method": "/test.Service/Denied"}, OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("write security audit: %v", err)
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
