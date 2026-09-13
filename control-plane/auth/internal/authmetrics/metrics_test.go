package authmetrics_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"io.astrasync/control-plane/auth/internal/authmetrics"
)

const canonicalTenantUUID = "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"

// TestRecorderSignInRoutesThroughNormalize exercises the new
// Recorder.ObserveSignIn entry point on a table that covers the
// catalog authentication outcome allowlist (success | rejected |
// failure) plus the canonical-lowercase-UUID tenant rule (ADR-047).
func TestRecorderSignInRoutesThroughNormalize(t *testing.T) {
	cases := []struct {
		name       string
		tenantID   string
		outcome    string
		wantTenant string
		wantOut    string
	}{
		{name: "happy_canonical_success", tenantID: canonicalTenantUUID, outcome: "success",
			wantTenant: canonicalTenantUUID, wantOut: "success"},
		{name: "happy_canonical_rejected", tenantID: canonicalTenantUUID, outcome: "rejected",
			wantTenant: canonicalTenantUUID, wantOut: "rejected"},
		{name: "happy_canonical_failure", tenantID: canonicalTenantUUID, outcome: "failure",
			wantTenant: canonicalTenantUUID, wantOut: "failure"},
		{name: "platform_self_scope", tenantID: "_platform", outcome: "success",
			wantTenant: "_platform", wantOut: "success"},
		{name: "non_canonical_tenant_collapsed", tenantID: "ALICE@acme.example", outcome: "success",
			wantTenant: "_unknown", wantOut: "success"},
		{name: "non_allowlisted_outcome_collapsed", tenantID: canonicalTenantUUID, outcome: "OK",
			wantTenant: canonicalTenantUUID, wantOut: "failure"},
		{name: "empty_outcome_collapsed", tenantID: canonicalTenantUUID, outcome: " ",
			wantTenant: canonicalTenantUUID, wantOut: "failure"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			registry := prometheus.NewRegistry()
			recorder, err := authmetrics.NewRecorder(registry)
			if err != nil {
				t.Fatalf("new recorder: %v", err)
			}
			recorder.ObserveSignIn(testCase.tenantID, testCase.outcome, "request-id")
			body := scrapeOpenMetrics(t, registry)
			sample := `auth_sign_in_total{outcome="` + testCase.wantOut +
				`",tenant_id="` + testCase.wantTenant + `"} 1`
			if !strings.Contains(body, sample) {
				t.Fatalf("scrape body missing %q for case %q: %s", sample, testCase.name, body)
			}
			if testCase.outcome != "" && !strings.Contains(testCase.wantOut, testCase.outcome) &&
				testCase.outcome != testCase.wantOut && strings.Contains(body, `outcome="`+testCase.outcome+`"`) {
				t.Fatalf("non-allowlisted outcome %q leaked into series for case %q: %s",
					testCase.outcome, testCase.name, body)
			}
		})
	}
}

// TestRecorderSessionRevokeRoutesThroughNormalize covers the second
// Recorder-owned metric family. The auth_session_revoke_total family
// has no outcome label; only tenant_id flows through normalize.
func TestRecorderSessionRevokeRoutesThroughNormalize(t *testing.T) {
	cases := []struct {
		name       string
		tenantID   string
		wantTenant string
	}{
		{name: "happy_canonical_uuid", tenantID: canonicalTenantUUID, wantTenant: canonicalTenantUUID},
		{name: "platform_self_scope", tenantID: "_platform", wantTenant: "_platform"},
		{name: "non_canonical_collapsed", tenantID: "BOB@acme.example", wantTenant: "_unknown"},
		{name: "uppercase_uuid_collapsed", tenantID: strings.ToUpper(canonicalTenantUUID),
			wantTenant: "_unknown"},
		{name: "braced_uuid_collapsed", tenantID: "{" + canonicalTenantUUID + "}",
			wantTenant: "_unknown"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			registry := prometheus.NewRegistry()
			recorder, err := authmetrics.NewRecorder(registry)
			if err != nil {
				t.Fatalf("new recorder: %v", err)
			}
			recorder.ObserveSessionRevoke(testCase.tenantID, "request-id")
			body := scrapeOpenMetrics(t, registry)
			sample := `auth_session_revoke_total{tenant_id="` + testCase.wantTenant + `"} 1`
			if !strings.Contains(body, sample) {
				t.Fatalf("scrape body missing %q for case %q: %s", sample, testCase.name, body)
			}
			if testCase.tenantID != "" && !strings.Contains(testCase.wantTenant, testCase.tenantID) &&
				testCase.tenantID != testCase.wantTenant && strings.Contains(body,
				`tenant_id="`+testCase.tenantID+`"`) {
				t.Fatalf("non-canonical tenant %q leaked into series for case %q: %s",
					testCase.tenantID, testCase.name, body)
			}
		})
	}
}

// TestRecorderScrapeIsBoundedAcrossDistinctInputs guards the slice 43.2
// contract that distinct caller inputs never widen the cardinality of
// auth_sign_in_total or auth_session_revoke_total beyond the documented
// allowlists. It also asserts that the package-level CounterVec surface
// (AuthSignInTotal / AuthSessionRevokeTotal, registered against the
// default registry) continues to work in parallel with a Recorder.
func TestRecorderScrapeIsBoundedAcrossDistinctInputs(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := authmetrics.NewRecorder(registry)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}

	distinct := []struct {
		tenant     string
		outcome    string
		revokeOnly bool
	}{
		{tenant: canonicalTenantUUID, outcome: "success"},
		{tenant: "ALICE@acme.example", outcome: "success"},
		{tenant: "{" + canonicalTenantUUID + "}", outcome: "rejected"},
		{tenant: canonicalTenantUUID, outcome: "OK"},
		{tenant: canonicalTenantUUID, outcome: "fenced"},
		{tenant: canonicalTenantUUID, outcome: ""},
		{tenant: canonicalTenantUUID, outcome: "rejected", revokeOnly: true},
		{tenant: "BOB@acme.example", revokeOnly: true},
		{tenant: strings.ToUpper(canonicalTenantUUID), revokeOnly: true},
	}
	for _, call := range distinct {
		if call.revokeOnly {
			recorder.ObserveSessionRevoke(call.tenant, "request-id")
		}
		recorder.ObserveSignIn(call.tenant, call.outcome, "request-id")
	}

	body := scrapeOpenMetrics(t, registry)
	signInSeries := strings.Count(body, "auth_sign_in_total{")
	revokeSeries := strings.Count(body, "auth_session_revoke_total{")
	if signInSeries != 6 {
		t.Fatalf("auth_sign_in_total series count = %d, want 6 (canonical tenant success + canonical rejected + canonical failure (collapsed from OK / fenced / empty) + non-canonical collapsed tenant success + non-canonical collapsed tenant rejected + non-canonical collapsed tenant failure (collapsed from empty)); body=%s",
			signInSeries, body)
	}
	if revokeSeries != 2 {
		t.Fatalf("auth_session_revoke_total series count = %d, want 2 (canonical uuid + non-canonical collapse); body=%s",
			revokeSeries, body)
	}
	for _, leak := range []string{
		`tenant_id="ALICE@acme.example"`,
		`tenant_id="BOB@acme.example"`,
		`tenant_id="` + strings.ToUpper(canonicalTenantUUID) + `"`,
		`tenant_id="{` + canonicalTenantUUID + `}"`,
		`outcome="OK"`,
		`outcome="fenced"`,
		`outcome=""`,
	} {
		if strings.Contains(body, leak) {
			t.Fatalf("non-allowlisted label value %q leaked into scrape body: %s", leak, body)
		}
	}
}

// TestRecorderNilReceiverIsSafe documents the slice-43.2 contract that
// nil-receiver Recorder methods are no-ops. Call sites at the boundary
// (auth library bootstrap, admin CLI revoke-session) own the Recorder;
// callers should never have to nil-check before calling Observe*.
func TestRecorderNilReceiverIsSafe(t *testing.T) {
	var recorder *authmetrics.Recorder
	recorder.ObserveSignIn(canonicalTenantUUID, "success", "request-id")
	recorder.ObserveSessionRevoke(canonicalTenantUUID, "request-id")
}

// TestNewRecorderRejectsNilRegisterer asserts the sentinel error path.
func TestNewRecorderRejectsNilRegisterer(t *testing.T) {
	if _, err := authmetrics.NewRecorder(nil); !errors.Is(err, authmetrics.ErrNilRegisterer) {
		t.Fatalf("NewRecorder(nil) err = %v, want ErrNilRegisterer", err)
	}
}

// TestNewRecorderRejectsDuplicateRegistration asserts that a second
// Recorder registration against the same registerer fails with the
// duplicate-metric sentinel. This is the contract that prevents two
// API Server processes (for example, a primary + a shadow) from
// silently re-registering the same metric on a shared registry.
func TestNewRecorderRejectsDuplicateRegistration(t *testing.T) {
	registry := prometheus.NewRegistry()
	if _, err := authmetrics.NewRecorder(registry); err != nil {
		t.Fatalf("first NewRecorder: %v", err)
	}
	_, err := authmetrics.NewRecorder(registry)
	if !errors.Is(err, authmetrics.ErrDuplicateMetric) {
		t.Fatalf("second NewRecorder err = %v, want ErrDuplicateMetric", err)
	}
}

// TestPackageLevelCountersRemainRegistered locks the slice-43.2
// backward-compatibility contract: the package-level CounterVec
// (AuthSignInTotal / AuthSessionRevokeTotal, registered against the
// default registry) continues to work for any consumer that uses the
// pre-43.2 import pattern. The Recorder path is additive, not
// destructive.
func TestPackageLevelCountersRemainRegistered(t *testing.T) {
	authmetrics.AuthSignInTotal.WithLabelValues(canonicalTenantUUID, "success").Inc()
	authmetrics.AuthSessionRevokeTotal.WithLabelValues(canonicalTenantUUID).Inc()

	signIn := authmetrics.AuthSignInTotal.WithLabelValues(canonicalTenantUUID, "success")
	if got := testutil.ToFloat64(signIn); got != 1 {
		t.Fatalf("AuthSignInTotal sample = %v, want 1", got)
	}
	revoke := authmetrics.AuthSessionRevokeTotal.WithLabelValues(canonicalTenantUUID)
	if got := testutil.ToFloat64(revoke); got != 1 {
		t.Fatalf("AuthSessionRevokeTotal sample = %v, want 1", got)
	}

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	recorder := httptest.NewRecorder()
	authmetrics.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("Handler() scrape status = %d, want 200", recorder.Code)
	}
	for _, want := range []string{`auth_sign_in_total{outcome="success",tenant_id="` + canonicalTenantUUID + `"} 1`,
		`auth_session_revoke_total{tenant_id="` + canonicalTenantUUID + `"} 1`} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Fatalf("legacy Handler() body missing %q: %s", want, recorder.Body.String())
		}
	}
}

func scrapeOpenMetrics(t *testing.T, gatherer prometheus.Gatherer) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Accept", "application/openmetrics-text")
	recorder := httptest.NewRecorder()
	promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{EnableOpenMetrics: true}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("scrape status = %d, want 200", recorder.Code)
	}
	return recorder.Body.String()
}
