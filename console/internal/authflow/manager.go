package authflow

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"io.astrasync/console/internal/oidc"
	"io.astrasync/control-plane/auth"
)

type PrincipalResolver interface {
	ResolveOrCreatePrincipal(context.Context, auth.ExternalIdentity) (auth.Principal, error)
	ResolvePrincipalByID(context.Context, string) (auth.Principal, error)
}

type Config struct {
	IdleTTL       time.Duration
	AbsoluteTTL   time.Duration
	LoginTTL      time.Duration
	RefreshWindow time.Duration
}

type Session struct {
	Principal auth.Principal
	Record    auth.ConsoleSession
	SessionID string
}

type LoginStart struct {
	AuthorizationURL string
	BrowserBinding   string
	ExpiresAt        time.Time
}

type Manager struct {
	provider *oidc.Client
	store    auth.ConsoleSessionStore
	resolver PrincipalResolver
	audit    auth.AuditWriter
	config   Config
	clock    func() time.Time
	eventID  func() string
}

func New(
	provider *oidc.Client, store auth.ConsoleSessionStore, resolver PrincipalResolver,
	audit auth.AuditWriter, configuration Config,
) (*Manager, error) {
	if provider == nil || store == nil || resolver == nil || audit == nil {
		return nil, fmt.Errorf("Console authentication dependencies must not be nil")
	}
	if configuration.IdleTTL == 0 {
		configuration.IdleTTL = 30 * time.Minute
	}
	if configuration.AbsoluteTTL == 0 {
		configuration.AbsoluteTTL = 8 * time.Hour
	}
	if configuration.LoginTTL == 0 {
		configuration.LoginTTL = 10 * time.Minute
	}
	if configuration.RefreshWindow == 0 {
		configuration.RefreshWindow = time.Minute
	}
	if configuration.IdleTTL < time.Minute || configuration.AbsoluteTTL < configuration.IdleTTL ||
		configuration.AbsoluteTTL > 7*24*time.Hour || configuration.LoginTTL < time.Minute ||
		configuration.LoginTTL > 30*time.Minute || configuration.RefreshWindow < 10*time.Second ||
		configuration.RefreshWindow > time.Hour {
		return nil, fmt.Errorf("Console authentication time bounds are invalid")
	}
	return &Manager{provider: provider, store: store, resolver: resolver, audit: audit,
		config: configuration, clock: time.Now, eventID: uuid.NewString}, nil
}

func (m *Manager) BeginLogin(ctx context.Context, returnTo string) (LoginStart, error) {
	returnTo = SafeReturnPath(returnTo)
	nonce, err := randomURLToken()
	if err != nil {
		return LoginStart{}, err
	}
	verifier, err := randomURLToken()
	if err != nil {
		return LoginStart{}, err
	}
	challengeBytes := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeBytes[:])
	credentials, err := m.store.CreateLoginTransaction(ctx, auth.LoginTransaction{
		Nonce: nonce, CodeVerifier: verifier, ReturnTo: returnTo,
	}, m.config.LoginTTL)
	if err != nil {
		return LoginStart{}, err
	}
	authorizationURL, err := m.provider.AuthorizationURL(credentials.State, nonce, challenge)
	if err != nil {
		return LoginStart{}, err
	}
	return LoginStart{AuthorizationURL: authorizationURL, BrowserBinding: credentials.BrowserBinding, ExpiresAt: credentials.ExpiresAt}, nil
}

func (m *Manager) CompleteLogin(
	ctx context.Context, state, browserBinding, code string,
) (Session, string, error) {
	transaction, err := m.store.ConsumeLoginTransaction(ctx, state, browserBinding)
	if err != nil || strings.TrimSpace(code) == "" || len(code) > 16*1024 {
		m.auditEvent(ctx, "authentication.login", "anonymous", "", "DENIED")
		return Session{}, "", auth.ErrUnauthenticated
	}
	tokens, err := m.provider.Exchange(ctx, code, transaction.CodeVerifier)
	if err != nil || tokens.IDToken == "" {
		m.auditEvent(ctx, "authentication.login", "anonymous", "", "DENIED")
		return Session{}, "", auth.ErrUnauthenticated
	}
	idToken, err := m.provider.ValidateIDToken(ctx, tokens.IDToken, transaction.Nonce)
	if err != nil {
		m.auditEvent(ctx, "authentication.login", "anonymous", "", "DENIED")
		return Session{}, "", auth.ErrUnauthenticated
	}
	accessToken, err := m.provider.ValidateAccessToken(ctx, tokens.AccessToken)
	if err != nil || accessToken.Identity != idToken.Identity {
		m.auditEvent(ctx, "authentication.login", "anonymous", "", "DENIED")
		return Session{}, "", auth.ErrUnauthenticated
	}
	if accessToken.ExpiresAt.Before(tokens.ExpiresAt) {
		tokens.ExpiresAt = accessToken.ExpiresAt
	}
	principal, err := m.resolver.ResolveOrCreatePrincipal(ctx, idToken.Identity)
	if err != nil || !principal.Active {
		m.auditEvent(ctx, "authentication.login", "anonymous", "", "DENIED")
		return Session{}, "", auth.ErrUnauthenticated
	}
	credentials, err := m.store.CreateConsoleSession(ctx, principal.ID, auth.ConsoleTokens{
		AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken,
		TokenType: tokens.TokenType, ExpiresAt: tokens.ExpiresAt,
	}, m.config.IdleTTL, m.config.AbsoluteTTL)
	if err != nil {
		m.auditEvent(ctx, "authentication.login", principal.ID, "", "DENIED")
		return Session{}, "", fmt.Errorf("create Console session: %w", err)
	}
	if err := m.audit.WriteSecurityAudit(ctx, auth.SecurityAuditEvent{
		EventID: m.eventID(), EventType: "authentication.login", ActorID: principal.ID,
		RequestID: requestID(ctx), Outcome: "ALLOWED", Attributes: map[string]any{
			"issuer": idToken.Identity.Issuer,
		}, OccurredAt: m.clock().UTC(),
	}); err != nil {
		_ = m.store.DeleteConsoleSession(ctx, credentials.SessionID)
		return Session{}, "", fmt.Errorf("record Console login audit: %w", err)
	}
	record, err := m.store.ResolveConsoleSession(ctx, credentials.SessionID, m.config.IdleTTL)
	if err != nil {
		return Session{}, "", err
	}
	return Session{Principal: principal, Record: record, SessionID: credentials.SessionID}, transaction.ReturnTo, nil
}

func (m *Manager) Resolve(ctx context.Context, sessionID string) (Session, error) {
	record, err := m.store.ResolveConsoleSession(ctx, sessionID, m.config.IdleTTL)
	if err != nil {
		return Session{}, err
	}
	principal, err := m.resolver.ResolvePrincipalByID(ctx, record.PrincipalID)
	if err != nil || !principal.Active {
		return Session{}, auth.ErrUnauthenticated
	}
	if !record.Tokens.ExpiresAt.After(m.clock().UTC()) {
		return m.refresh(ctx, principal, record, sessionID)
	}
	if record.Tokens.RefreshToken != "" && record.Tokens.ExpiresAt.Sub(m.clock().UTC()) <= m.config.RefreshWindow {
		if refreshed, refreshErr := m.refresh(ctx, principal, record, sessionID); refreshErr == nil {
			return refreshed, nil
		}
	}
	return Session{Principal: principal, Record: record, SessionID: sessionID}, nil
}

func (m *Manager) ValidateCSRF(session Session, token string) bool {
	if token == "" || len(token) != len(session.Record.CSRFToken) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(session.Record.CSRFToken)) == 1
}

func (m *Manager) Logout(ctx context.Context, session Session) error {
	deleteErr := m.store.DeleteConsoleSession(ctx, session.SessionID)
	auditErr := m.audit.WriteSecurityAudit(ctx, auth.SecurityAuditEvent{
		EventID: m.eventID(), EventType: "authentication.logout", ActorID: session.Principal.ID,
		RequestID: requestID(ctx), Outcome: "ALLOWED", OccurredAt: m.clock().UTC(),
	})
	if deleteErr != nil {
		return deleteErr
	}
	return auditErr
}

func (m *Manager) refresh(ctx context.Context, principal auth.Principal, record auth.ConsoleSession, sessionID string) (Session, error) {
	if record.Tokens.RefreshToken == "" {
		return Session{}, auth.ErrUnauthenticated
	}
	refreshed, err := m.provider.Refresh(ctx, record.Tokens.RefreshToken)
	if err != nil {
		return Session{}, auth.ErrUnauthenticated
	}
	access, err := m.provider.ValidateAccessToken(ctx, refreshed.AccessToken)
	if err != nil || access.Identity.Issuer != principal.Issuer || access.Identity.Subject != principal.Subject {
		return Session{}, auth.ErrUnauthenticated
	}
	refreshToken := refreshed.RefreshToken
	if refreshToken == "" {
		refreshToken = record.Tokens.RefreshToken
	}
	next := auth.ConsoleTokens{AccessToken: refreshed.AccessToken, RefreshToken: refreshToken,
		TokenType: refreshed.TokenType, ExpiresAt: refreshed.ExpiresAt}
	if _, err := m.store.UpdateConsoleSessionTokens(ctx, sessionID, principal.ID, record.Revision, next); err != nil {
		if errors.Is(err, auth.ErrSessionConflict) {
			return m.Resolve(ctx, sessionID)
		}
		return Session{}, auth.ErrUnauthenticated
	}
	record.Tokens = next
	record.Revision++
	return Session{Principal: principal, Record: record, SessionID: sessionID}, nil
}

func (m *Manager) auditEvent(ctx context.Context, eventType, actorID, tenantID, outcome string) {
	_ = m.audit.WriteSecurityAudit(ctx, auth.SecurityAuditEvent{
		EventID: m.eventID(), EventType: eventType, ActorID: actorID, TenantID: tenantID,
		RequestID: requestID(ctx), Outcome: outcome, OccurredAt: m.clock().UTC(),
	})
}

func randomURLToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate OIDC transaction value: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

// SafeReturnPath preserves a same-origin absolute path and falls back to the
// Console root for values that browsers could interpret as another authority.
func SafeReturnPath(value string) string {
	if value == "" || len(value) > 2048 || !strings.HasPrefix(value, "/") ||
		strings.HasPrefix(value, "//") || containsUnsafeReturnPathRune(value) {
		return "/"
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Opaque != "" ||
		parsed.Path == "" || !strings.HasPrefix(parsed.Path, "/") ||
		strings.HasPrefix(parsed.Path, "//") || containsUnsafeReturnPathRune(parsed.Path) {
		return "/"
	}
	return value
}

func containsUnsafeReturnPathRune(value string) bool {
	return strings.ContainsRune(value, '\\') || strings.IndexFunc(value, unicode.IsControl) >= 0
}

func requestID(ctx context.Context) string {
	if value, ok := ctx.Value(requestIDKey{}).(string); ok && len(value) <= 128 && value != "" {
		return value
	}
	return uuid.NewString()
}

type requestIDKey struct{}

func WithRequestID(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, value)
}

type DevelopmentManager struct {
	principal auth.Principal
	session   Session
}

func NewDevelopmentManager(tenantID, namespace string) (*DevelopmentManager, error) {
	if tenantID == "" || namespace == "" {
		return nil, fmt.Errorf("development Console tenant is required")
	}
	membership, err := auth.NewMembership(tenantID, true, auth.AllTenantPermissions()...)
	if err != nil {
		return nil, err
	}
	membership.TenantNamespace = namespace
	membership.TenantDisplayName = namespace
	membership.Role = auth.RoleTenantAdmin
	principal := auth.Principal{ID: "development", Subject: "development", Active: true,
		PolicyRevision: "development", Memberships: map[string]auth.Membership{tenantID: membership}}
	record := auth.ConsoleSession{PrincipalID: principal.ID, CSRFToken: "development-csrf", Revision: 1}
	return &DevelopmentManager{principal: principal, session: Session{Principal: principal, Record: record, SessionID: "development"}}, nil
}

func (m *DevelopmentManager) BeginLogin(context.Context, string) (LoginStart, error) {
	return LoginStart{}, fmt.Errorf("OIDC login is disabled in development")
}

func (m *DevelopmentManager) CompleteLogin(context.Context, string, string, string) (Session, string, error) {
	return Session{}, "", auth.ErrUnauthenticated
}

func (m *DevelopmentManager) Resolve(context.Context, string) (Session, error) { return m.session, nil }
func (m *DevelopmentManager) ValidateCSRF(session Session, token string) bool {
	return session.SessionID == m.session.SessionID && token == m.session.Record.CSRFToken
}
func (m *DevelopmentManager) Logout(context.Context, Session) error { return nil }
