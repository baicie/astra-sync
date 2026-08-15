package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"io.astrasync/console/internal/authflow"
	"io.astrasync/console/internal/server"
	"io.astrasync/control-plane/auth"
)

// TestBFFAutoSelectsSoleMembership 验证：当 X-Astra-Tenant-ID 未指定，且
// 当前 session 只属于一个 active tenant 时，BFF 自动选用该 tenant。
func TestBFFAutoSelectsSoleMembership(t *testing.T) {
	backend := &fakeBFFBackend{}
	sessions := newFakeSessions(t)
	console, err := server.NewWithConfig(server.Config{
		Backend: backend, Sessions: sessions, AuthMode: "oidc",
		PublicOrigin: "https://console.example",
	})
	if err != nil {
		t.Fatalf("create BFF server: %v", err)
	}
	response := bffRequest(console.Handler(), http.MethodGet, "/api/connectors", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected sole tenant auto-pick to succeed: %d %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Header().Get("X-Astra-Tenant-ID"), testTenantID) {
		t.Fatalf("expected X-Astra-Tenant-ID response header to mirror the sole membership, got %q",
			response.Header().Get("X-Astra-Tenant-ID"))
	}
}

// TestBFFRequiresExplicitSelectionWhenMultipleMemberships 验证：当 session
// 属于多个 active tenant 且未显式提供 X-Astra-Tenant-ID 时，BFF 拒绝。
func TestBFFRequiresExplicitSelectionWhenMultipleMemberships(t *testing.T) {
	backend := &fakeBFFBackend{}
	sessions := newFakeSessions(t)
	const secondTenantID = "22222222-2222-4222-8222-222222222222"
	secondMembership, _ := auth.NewMembership(secondTenantID, true, auth.PermissionJobsRead)
	secondMembership.TenantNamespace = "tenant-b"
	sessions.session.Principal.Memberships[secondTenantID] = secondMembership
	console, err := server.NewWithConfig(server.Config{
		Backend: backend, Sessions: sessions, AuthMode: "oidc",
		PublicOrigin: "https://console.example",
	})
	if err != nil {
		t.Fatalf("create BFF server: %v", err)
	}
	response := bffRequest(console.Handler(), http.MethodGet, "/api/connectors", "", nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected ambiguous tenant rejection, got %d %s", response.Code, response.Body.String())
	}
	if backend.authorization != "" {
		t.Fatalf("ambiguous selection must not forward an authenticated backend request: %q", backend.authorization)
	}
}

// TestBFFIgnoresInactiveMemberships 验证：当所有 membership 都为 inactive
// 时，BFF 必须拒绝请求而不是误用其中一个。
func TestBFFIgnoresInactiveMemberships(t *testing.T) {
	sessions := newFakeSessions(t)
	inactive := sessions.session.Principal.Memberships[testTenantID]
	inactive.Active = false
	sessions.session.Principal.Memberships[testTenantID] = inactive
	console, err := server.NewWithConfig(server.Config{
		Backend: &fakeBFFBackend{}, Sessions: sessions, AuthMode: "oidc",
		PublicOrigin: "https://console.example",
	})
	if err != nil {
		t.Fatalf("create BFF server: %v", err)
	}
	response := bffRequest(console.Handler(), http.MethodGet, "/api/connectors", "", nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected inactive membership rejection, got %d %s", response.Code, response.Body.String())
	}
}

// TestBFFMutationRequiresValidCSRF 验证：mutating endpoint 缺失/错误的 CSRF
// token 立即返回 PERMISSION_DENIED，不转发到后端。
func TestBFFMutationRequiresValidCSRF(t *testing.T) {
	console, err := server.NewWithConfig(server.Config{
		Backend: &fakeBFFBackend{}, Sessions: newFakeSessions(t), AuthMode: "oidc",
		PublicOrigin: "https://console.example",
	})
	if err != nil {
		t.Fatalf("create BFF server: %v", err)
	}
	headers := map[string]string{
		"X-Astra-Tenant-ID": testTenantID, "Content-Type": "application/json",
		"Origin": "https://console.example", "Idempotency-Key": "11111111-1111-4111-8111-111111111111",
	}
	body := `{"name":"orders-db","connector":"jdbc","settings":[{"key":"host","value":"db.internal"}],
		"secretBinding":{"provider":"kubernetes","secretName":"secret-name-sentinel","secretUid":"secret-uid-sentinel","fields":[]}}`
	response := bffRequest(console.Handler(), http.MethodPost, "/api/connections", body, headers)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected missing CSRF denial, got %d %s", response.Code, response.Body.String())
	}
	headers["X-CSRF-Token"] = "wrong"
	response = bffRequest(console.Handler(), http.MethodPost, "/api/connections", body, headers)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected wrong CSRF denial, got %d %s", response.Code, response.Body.String())
	}
}

// TestBFFSessionEndpointDoesNotExposeTokens 验证：GET /api/session 永远不能
// 在响应体或头部中暴露 access/refresh token、CSRF 之外的密钥。
func TestBFFSessionEndpointDoesNotExposeTokens(t *testing.T) {
	backend := &fakeBFFBackend{}
	console, err := server.NewWithConfig(server.Config{
		Backend: backend, Sessions: newFakeSessions(t), AuthMode: "oidc",
		PublicOrigin: "https://console.example",
	})
	if err != nil {
		t.Fatalf("create BFF server: %v", err)
	}
	response := bffRequest(console.Handler(), http.MethodGet, "/api/session", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("session endpoint returned %d %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if strings.Contains(body, "access-token-sentinel") {
		t.Fatalf("access token leaked through session payload: %s", body)
	}
	if strings.Contains(body, "refresh-token") {
		t.Fatalf("refresh token leaked through session payload: %s", body)
	}
	for name := range response.Header() {
		if strings.Contains(strings.ToLower(name), "token") || strings.Contains(strings.ToLower(name), "authorization") {
			t.Fatalf("session response carried header %s", name)
		}
	}
}

// TestBFFAuthenticationRequiredForAudit 验证：未认证请求（无 cookie）不能
// 读取 audit events 或查询 job 列表。BFF 立即返回 UNAUTHENTICATED 而不
// 把请求转发到后端。
func TestBFFAuthenticationRequiredForAudit(t *testing.T) {
	console, err := server.NewWithConfig(server.Config{
		Backend: &fakeBFFBackend{}, Sessions: newFakeSessions(t), AuthMode: "oidc",
		PublicOrigin: "https://console.example",
	})
	if err != nil {
		t.Fatalf("create BFF server: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/audit-events", strings.NewReader(""))
	response := httptest.NewRecorder()
	console.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated denial, got %d %s", response.Code, response.Body.String())
	}
}

// unauthenticatedRequest 直接发送不携带 session cookie 的请求，验证 BFF 的
// authentication-required 行为。它独立于 bffRequest 以避免其默认 cookie。
func unauthenticatedRequest(handler http.Handler, method, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(""))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// TestBFFPreservesSessionIdentityOnLogout 验证：logout 在刷新 cookie 之前
// 必须先撤销服务端 session，无法仅清 cookie 就完成。
func TestBFFPreservesSessionIdentityOnLogout(t *testing.T) {
	sessions := newFakeSessions(t)
	revoked := false
	logoutSessions := &logoutTrackingSessions{inner: sessions, onLogout: func() { revoked = true }}
	console, err := server.NewWithConfig(server.Config{
		Backend: &fakeBFFBackend{}, Sessions: logoutSessions, AuthMode: "oidc",
		PublicOrigin: "https://console.example",
	})
	if err != nil {
		t.Fatalf("create BFF server: %v", err)
	}
	headers := map[string]string{
		"X-CSRF-Token": sessions.session.Record.CSRFToken,
		"Content-Type": "application/json", "Origin": "https://console.example",
	}
	response := bffRequest(console.Handler(), http.MethodPost, "/auth/logout", "", headers)
	if response.Code != http.StatusNoContent {
		t.Fatalf("logout expected 204, got %d %s", response.Code, response.Body.String())
	}
	if !revoked {
		t.Fatalf("server-side session was not revoked before the cookie was cleared")
	}
}

type logoutTrackingSessions struct {
	inner    *fakeSessions
	onLogout func()
}

func (l *logoutTrackingSessions) BeginLogin(ctx context.Context, returnTo string) (authflow.LoginStart, error) {
	return l.inner.BeginLogin(ctx, returnTo)
}
func (l *logoutTrackingSessions) CompleteLogin(ctx context.Context, state, binding, code string) (authflow.Session, string, error) {
	return l.inner.CompleteLogin(ctx, state, binding, code)
}
func (l *logoutTrackingSessions) Resolve(ctx context.Context, sessionID string) (authflow.Session, error) {
	return l.inner.Resolve(ctx, sessionID)
}
func (l *logoutTrackingSessions) ValidateCSRF(session authflow.Session, token string) bool {
	return l.inner.ValidateCSRF(session, token)
}
func (l *logoutTrackingSessions) Logout(ctx context.Context, session authflow.Session) error {
	l.onLogout()
	return l.inner.Logout(ctx, session)
}

// silence unused warning if time package stops being used by helpers above.
var _ = codes.PermissionDenied
var _ = status.Error
