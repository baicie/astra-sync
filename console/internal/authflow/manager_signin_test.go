package authflow

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"io.astrasync/console/internal/oidc"
	"io.astrasync/control-plane/auth"
)

// fakeStore implements auth.ConsoleSessionStore for sign-in metric tests.
type fakeStore struct {
	consumeErr          error
	consumeTransaction  auth.LoginTransaction
	createSessionErr    error
	createSessionCreds  auth.SessionCredentials
	resolveSessionErr   error
	resolveSessionRec   auth.ConsoleSession
}

func (f *fakeStore) ConsumeLoginTransaction(ctx context.Context, state, browserBinding string) (auth.LoginTransaction, error) {
	if f.consumeErr != nil {
		return auth.LoginTransaction{}, f.consumeErr
	}
	return f.consumeTransaction, nil
}

func (f *fakeStore) CreateConsoleSession(ctx context.Context, principalID string, tokens auth.ConsoleTokens, idle, absolute time.Duration) (auth.SessionCredentials, error) {
	if f.createSessionErr != nil {
		return auth.SessionCredentials{}, f.createSessionErr
	}
	return f.createSessionCreds, nil
}

func (f *fakeStore) ResolveConsoleSession(ctx context.Context, sessionID string, idleTTL time.Duration) (auth.ConsoleSession, error) {
	if f.resolveSessionErr != nil {
		return auth.ConsoleSession{}, f.resolveSessionErr
	}
	return f.resolveSessionRec, nil
}

func (f *fakeStore) DeleteConsoleSession(ctx context.Context, sessionID string) error { return nil }

func (f *fakeStore) CreateLoginTransaction(ctx context.Context, t auth.LoginTransaction, d time.Duration) (auth.LoginCredentials, error) {
	return auth.LoginCredentials{}, nil
}

func (f *fakeStore) UpdateConsoleSessionTokens(ctx context.Context, id string, pid string, rev int64, t auth.ConsoleTokens) (int64, error) {
	return 0, nil
}

// fakeResolver implements PrincipalResolver for sign-in metric tests.
type fakeResolver struct {
	principal  auth.Principal
	resolveErr error
}

func (f *fakeResolver) ResolveOrCreatePrincipal(ctx context.Context, identity auth.ExternalIdentity) (auth.Principal, error) {
	if f.resolveErr != nil {
		return auth.Principal{}, f.resolveErr
	}
	return f.principal, nil
}

func (f *fakeResolver) ResolvePrincipalByID(ctx context.Context, id string) (auth.Principal, error) {
	return f.principal, nil
}

// fakeAudit implements auth.AuditWriter for sign-in metric tests.
type fakeAudit struct {
	writeErr error
}

func (f *fakeAudit) WriteSecurityAudit(ctx context.Context, event auth.SecurityAuditEvent) error {
	return f.writeErr
}

// scrapeOpenMetrics returns the OpenMetrics text produced by gathering from the
// supplied registerer. Mirrors the helper in authmetrics_test.go.
func scrapeOpenMetrics(t *testing.T, reg *prometheus.Registry) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Accept", "application/openmetrics-text")
	rec := httptest.NewRecorder()
	promhttp.HandlerFor(reg, promhttp.HandlerOpts{EnableOpenMetrics: true}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("scrape status = %d, want 200", rec.Code)
	}
	return rec.Body.String()
}

// fakeOIDCClient satisfies oidcProvider for test injection. Methods guard
// against nil receiver so (*fakeOIDCClient)(nil) is a safe no-op stub for
// early-return test paths that never invoke OIDC calls.
type fakeOIDCClient struct{}

func (f *fakeOIDCClient) AuthorizationURL(state, nonce, codeChallenge string) (string, error) {
	if f == nil {
		return "", errors.New("oidcProvider: nil receiver")
	}
	return "", errors.New("not called in this test path")
}
func (f *fakeOIDCClient) Exchange(ctx context.Context, code, verifier string) (oidc.TokenSet, error) {
	if f == nil {
		return oidc.TokenSet{}, errors.New("oidcProvider: nil receiver")
	}
	return oidc.TokenSet{}, errors.New("not called in this test path")
}
func (f *fakeOIDCClient) ValidateIDToken(ctx context.Context, token, expectedNonce string) (auth.ValidatedToken, error) {
	if f == nil {
		return auth.ValidatedToken{}, errors.New("oidcProvider: nil receiver")
	}
	return auth.ValidatedToken{}, errors.New("not called in this test path")
}
func (f *fakeOIDCClient) ValidateAccessToken(ctx context.Context, token string) (auth.ValidatedToken, error) {
	if f == nil {
		return auth.ValidatedToken{}, errors.New("oidcProvider: nil receiver")
	}
	return auth.ValidatedToken{}, errors.New("not called in this test path")
}
func (f *fakeOIDCClient) Refresh(ctx context.Context, refreshToken string) (oidc.TokenSet, error) {
	if f == nil {
		return oidc.TokenSet{}, errors.New("oidcProvider: nil receiver")
	}
	return oidc.TokenSet{}, errors.New("not called in this test path")
}

func validCfg() Config {
	return Config{IdleTTL: 30 * time.Minute, AbsoluteTTL: 8 * time.Hour,
		LoginTTL: 10 * time.Minute, RefreshWindow: time.Minute}
}

// TestCompleteLoginRecorderEmitsRejectedOnStoreDenied covers the slice
// 43.1.5 contract: when the store rejects ConsumeLoginTransaction,
// CompleteLogin emits auth_sign_in_total with outcome="rejected" and
// tenant_id="_platform" (no principal is established yet, ADR-065 §2).
//
// This test exercises the early-return path in CompleteLogin that fires
// BEFORE any *oidc.Client call, so it can be driven by a fake store.
// The oidc.Client field on Manager is nil; CompleteLogin short-circuits
// on the store error before reaching the provider.
func TestCompleteLoginRecorderEmitsRejectedOnStoreDenied(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	rec, err := auth.NewRecorder(reg)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}

	store := &fakeStore{consumeErr: errors.New("tx not found")}
	m, err := New((*fakeOIDCClient)(nil), store, &fakeResolver{}, &fakeAudit{}, validCfg(), WithRecorder(rec))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, _, err = m.CompleteLogin(context.Background(), "state", "binding", "code")
	if !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("CompleteLogin error = %v, want ErrUnauthenticated", err)
	}

	body := scrapeOpenMetrics(t, reg)
	wantSample := `auth_sign_in_total{outcome="rejected",tenant_id="_platform"} 1`
	if !strings.Contains(body, wantSample) {
		t.Fatalf("scrape body missing %q; body=%s", wantSample, body)
	}
}

// TestCompleteLoginRecorderEmitsRejectedOnEmptyCode covers the second
// early-return path: when the authorization code is empty (or oversized),
// CompleteLogin emits outcome="rejected" with tenant_id="_platform"
// without ever consulting the OIDC provider.
func TestCompleteLoginRecorderEmitsRejectedOnEmptyCode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		code string
	}{
		{name: "empty_code", code: ""},
		{name: "whitespace_code", code: "   "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reg := prometheus.NewRegistry()
			rec, err := auth.NewRecorder(reg)
			if err != nil {
				t.Fatalf("new recorder: %v", err)
			}

			store := &fakeStore{
				consumeTransaction: auth.LoginTransaction{
					Nonce:        strings.Repeat("n", 64),
					CodeVerifier: strings.Repeat("v", 64),
					ReturnTo:     "/",
				},
			}
			m, err := New((*fakeOIDCClient)(nil), store, &fakeResolver{}, &fakeAudit{}, validCfg(), WithRecorder(rec))
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			_, _, err = m.CompleteLogin(context.Background(), "state", "binding", tc.code)
			if !errors.Is(err, auth.ErrUnauthenticated) {
				t.Fatalf("CompleteLogin error = %v, want ErrUnauthenticated", err)
			}

			body := scrapeOpenMetrics(t, reg)
			wantSample := `auth_sign_in_total{outcome="rejected",tenant_id="_platform"} 1`
			if !strings.Contains(body, wantSample) {
				t.Fatalf("scrape body missing %q; body=%s", wantSample, body)
			}
		})
	}
}

// TestCompleteLoginRecorderNilSafe documents the slice 43.1.5 contract:
// when no Recorder is injected (or a nil Recorder is injected), the
// recorder calls in CompleteLogin must not panic. This mirrors the
// authmetrics.Recorder nil-receiver safety documented in
// TestRecorderNilReceiverIsSafe.
func TestCompleteLoginRecorderNilSafe(t *testing.T) {
	t.Parallel()

	store := &fakeStore{consumeErr: errors.New("tx not found")}
	m, err := New((*fakeOIDCClient)(nil), store, &fakeResolver{}, &fakeAudit{}, validCfg())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Must not panic even though recorder field is nil.
	_, _, err = m.CompleteLogin(context.Background(), "state", "binding", "code")
	if !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("CompleteLogin error = %v, want ErrUnauthenticated", err)
	}

	// Also confirm explicit nil Recorder does not panic.
	m, err = New((*fakeOIDCClient)(nil), store, &fakeResolver{}, &fakeAudit{}, validCfg(), WithRecorder(nil))
	if err != nil {
		t.Fatalf("New with nil Recorder: %v", err)
	}
	_, _, err = m.CompleteLogin(context.Background(), "state", "binding", "code")
	if !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("CompleteLogin error = %v, want ErrUnauthenticated", err)
	}
}

// TestFirstTenantID covers the helper that derives tenant_id from
// principal.Memberships. This determines the label value for successful
// sign-in emissions.
func TestFirstTenantID(t *testing.T) {
	t.Parallel()

	t.Run("returns_first_membership_key", func(t *testing.T) {
		t.Parallel()
		principal := auth.Principal{
			ID: "alice",
			Memberships: map[string]auth.Membership{
				"0190f7c4-6c8d-7a01-9d2b-1ecabdff0011": {TenantID: "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"},
				"0190f7c4-6c8d-7a01-9d2b-1ecabdff0012": {TenantID: "0190f7c4-6c8d-7a01-9d2b-1ecabdff0012"},
			},
		}
		got := firstTenantID(principal)
		if got != "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011" && got != "0190f7c4-6c8d-7a01-9d2b-1ecabdff0012" {
			t.Fatalf("firstTenantID = %q, want one of the two membership keys", got)
		}
	})

	t.Run("platform_scope_for_no_memberships", func(t *testing.T) {
		t.Parallel()
		principal := auth.Principal{ID: "bob", Active: true, Memberships: nil}
		got := firstTenantID(principal)
		if got != "_platform" {
			t.Fatalf("firstTenantID = %q, want _platform", got)
		}
	})

	t.Run("platform_scope_for_empty_memberships", func(t *testing.T) {
		t.Parallel()
		principal := auth.Principal{ID: "bob", Active: true, Memberships: map[string]auth.Membership{}}
		got := firstTenantID(principal)
		if got != "_platform" {
			t.Fatalf("firstTenantID = %q, want _platform", got)
		}
	})
}

// TestRecorderEmitsSuccessAndFailureOutcomes documents the full outcome
// surface for auth_sign_in_total emitted through the Manager's Recorder.
// This is a Recorder-level contract test (the same pattern used in
// authmetrics_test.go) that pins the success / failure labels for
// future regressions. It complements TestCompleteLoginRecorderEmitsRejectedOn*
// by exercising the non-rejected outcomes that are only reachable through
// the post-OIDC paths in CompleteLogin.
func TestRecorderEmitsSuccessAndFailureOutcomes(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	rec, err := auth.NewRecorder(reg)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}

	tenantID := "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"

	// Simulate the success branch (called after audit write succeeds).
	rec.ObserveSignIn(tenantID, "success", "req-success")
	// Simulate the failure branch (called when CreateConsoleSession fails).
	rec.ObserveSignIn(tenantID, "failure", "req-failure")

	body := scrapeOpenMetrics(t, reg)
	wantSuccess := `auth_sign_in_total{outcome="success",tenant_id="` + tenantID + `"} 1`
	wantFailure := `auth_sign_in_total{outcome="failure",tenant_id="` + tenantID + `"} 1`
	if !strings.Contains(body, wantSuccess) {
		t.Fatalf("scrape body missing %q; body=%s", wantSuccess, body)
	}
	if !strings.Contains(body, wantFailure) {
		t.Fatalf("scrape body missing %q; body=%s", wantFailure, body)
	}
}
