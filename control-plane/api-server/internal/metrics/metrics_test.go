package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

const exemplarRequestID = "d724ad9a-30a2-4dab-9704-2b01ea1f67e1"

func TestRecorderObservesSLOMetricsWithBoundedExemplars(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := NewRecorder(registry)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}

	recorder.ObserveAuthRequest("tenant-a", "success", exemplarRequestID, 25*time.Millisecond)
	recorder.ObserveAuditQuery("tenant-a", exemplarRequestID, 40*time.Millisecond)

	body := scrapeOpenMetrics(t, registry)
	for _, sample := range []string{
		`apiserver_auth_request_total{outcome="success",tenant_id="tenant-a"} 1.0`,
		`apiserver_auth_request_duration_seconds_count{outcome="success",tenant_id="tenant-a"} 1`,
		`apiserver_audit_query_duration_seconds_count{tenant_id="tenant-a"} 1`,
	} {
		if !strings.Contains(body, sample) {
			t.Fatalf("OpenMetrics body missing %q: %s", sample, body)
		}
	}
	if count := strings.Count(body, `request_id="`+exemplarRequestID+`"`); count != 3 {
		t.Fatalf("request_id exemplar count = %d, want 3: %s", count, body)
	}
}

func TestRecorderDropsNonCanonicalRequestIDExemplars(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := NewRecorder(registry)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}

	recorder.ObserveAuthRequest("tenant-a", "rejected", "attacker-controlled", 5*time.Millisecond)
	recorder.ObserveAuditQuery("tenant-a", "D724AD9A-30A2-4DAB-9704-2B01EA1F67E1", 8*time.Millisecond)

	body := scrapeOpenMetrics(t, registry)
	if strings.Contains(body, "request_id=") {
		t.Fatalf("non-canonical request ID was exposed as an exemplar: %s", body)
	}
	for _, sample := range []string{
		`apiserver_auth_request_total{outcome="rejected",tenant_id="tenant-a"} 1.0`,
		`apiserver_audit_query_duration_seconds_count{tenant_id="tenant-a"} 1`,
	} {
		if !strings.Contains(body, sample) {
			t.Fatalf("observation without exemplar missing %q: %s", sample, body)
		}
	}
}

func TestMetricsRegistered(t *testing.T) {
	// Touch each metric to make sure registration happened at init time.
	metrics := []struct {
		name    string
		collect func() float64
	}{
		{"apiserver_auth_request_total", func() float64 {
			metric := AuthRequestTotal.WithLabelValues("tenant-a", "success")
			before := testutil.ToFloat64(metric)
			metric.Inc()
			return testutil.ToFloat64(metric) - before
		}},
		{"apiserver_sign_in_total", func() float64 {
			metric := SignInTotal.WithLabelValues("tenant-a", "started")
			before := testutil.ToFloat64(metric)
			metric.Inc()
			return testutil.ToFloat64(metric) - before
		}},
		{"apiserver_session_revoke_total", func() float64 {
			metric := SessionRevokeTotal.WithLabelValues("tenant-a", "actor-a")
			before := testutil.ToFloat64(metric)
			metric.Inc()
			return testutil.ToFloat64(metric) - before
		}},
		{"apiserver_trusted_proxy_hsts_total", func() float64 {
			metric := TrustedProxyHSTS.WithLabelValues("tenant-a")
			before := testutil.ToFloat64(metric)
			metric.Inc()
			return testutil.ToFloat64(metric) - before
		}},
	}
	for _, metric := range metrics {
		if metric.collect() != 1 {
			t.Fatalf("metric %s did not register or increment", metric.name)
		}
	}

	AuthRequestDuration.WithLabelValues("tenant-a", "success").Observe(0.01)
	AuditQueryDuration.WithLabelValues("tenant-a").Observe(0.02)
	if testutil.CollectAndCount(AuthRequestDuration) == 0 {
		t.Fatalf("apiserver_auth_request_duration_seconds did not register")
	}
	if testutil.CollectAndCount(AuditQueryDuration) == 0 {
		t.Fatalf("apiserver_audit_query_duration_seconds did not register")
	}
}

func TestHandlerExposesMetrics(t *testing.T) {
	AuthRequestTotal.WithLabelValues("scrape-tenant", "success").Inc()
	AuthRequestDuration.WithLabelValues("scrape-tenant", "success").Observe(0.01)
	SignInTotal.WithLabelValues("scrape-tenant", "success").Inc()
	SessionRevokeTotal.WithLabelValues("scrape-tenant", "actor-a").Inc()
	AuditQueryDuration.WithLabelValues("scrape-tenant").Observe(0.01)
	TrustedProxyHSTS.WithLabelValues("scrape-tenant").Inc()

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	recorder := httptest.NewRecorder()
	Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, name := range []string{
		"apiserver_auth_request_total",
		"apiserver_auth_request_duration_seconds",
		"apiserver_sign_in_total",
		"apiserver_session_revoke_total",
		"apiserver_audit_query_duration_seconds",
		"apiserver_trusted_proxy_hsts_total",
	} {
		if !strings.Contains(body, name) {
			t.Fatalf("/metrics body missing %s: %s", name, body)
		}
	}
}

func TestHandlerNegotiatesOpenMetrics(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Accept", "application/openmetrics-text")
	recorder := httptest.NewRecorder()
	Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/openmetrics-text") {
		t.Fatalf("Content-Type = %q, want OpenMetrics", contentType)
	}
}

// canonicalTenantUUID is a lowercase UUID accepted by NormalizeTenant as
// the canonical tenant shape. The dashboard recipes (ADR-047 §126) bind
// on this exact form so the slice-43.1 tests can assert exact-match
// series names.
const canonicalTenantUUID = "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"

// TestRecorderSignInEmitsCanonicalTenantAndOutcomeAllowlist exercises the
// Phase 17 slice 43.1 happy / rejected / failure paths through the
// Recorder. The scrape-level assertion mirrors the existing scrape
// patterns for AuthRequestTotal (ADR-058 §4 test contract, item 2).
func TestRecorderSignInEmitsCanonicalTenantAndOutcomeAllowlist(t *testing.T) {
	cases := []struct {
		name        string
		tenantID    string
		outcome     string
		wantTenant  string
		wantOutcome string
	}{
		{name: "happy_canonical_success", tenantID: canonicalTenantUUID, outcome: "success", wantTenant: canonicalTenantUUID, wantOutcome: "success"},
		{name: "rejected_canonical", tenantID: canonicalTenantUUID, outcome: "rejected", wantTenant: canonicalTenantUUID, wantOutcome: "rejected"},
		{name: "failure_canonical", tenantID: canonicalTenantUUID, outcome: "failure", wantTenant: canonicalTenantUUID, wantOutcome: "failure"},
		{name: "platform_self_scope", tenantID: "_platform", outcome: "success", wantTenant: "_platform", wantOutcome: "success"},
		{name: "non_canonical_tenant_collapsed", tenantID: "ALICE@acme.example", outcome: "success", wantTenant: "_unknown", wantOutcome: "success"},
		{name: "non_allowlisted_outcome_collapsed", tenantID: canonicalTenantUUID, outcome: "started", wantTenant: canonicalTenantUUID, wantOutcome: "failure"},
		{name: "empty_tenant_collapsed", tenantID: "   ", outcome: "rejected", wantTenant: "_unknown", wantOutcome: "rejected"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			registry := prometheus.NewRegistry()
			recorder, err := NewRecorder(registry)
			if err != nil {
				t.Fatalf("new recorder: %v", err)
			}
			recorder.ObserveSignIn(testCase.tenantID, testCase.outcome, "")
			body := scrapeOpenMetrics(t, registry)
			sample := `apiserver_sign_in_total{outcome="` + testCase.wantOutcome + `",tenant_id="` + testCase.wantTenant + `"} 1.0`
			if !strings.Contains(body, sample) {
				t.Fatalf("scrape body missing %q for case %q: %s", sample, testCase.name, body)
			}
			// Defensive: raw caller input must never leak into a label
			// value, regardless of case.
			if testCase.tenantID != "" && testCase.tenantID != canonicalTenantUUID && testCase.tenantID != "_platform" && strings.Contains(body, `tenant_id="`+testCase.tenantID+`"`) {
				t.Fatalf("non-canonical tenant %q leaked into a series for case %q: %s", testCase.tenantID, testCase.name, body)
			}
		})
	}
}

// TestRecorderSessionRevokeNormalisesActorAndTenant covers the second
// Phase 17 slice 43.1 family. actor_id routes through NormalizeWorkerID
// so two runaway callers cannot collide on a truncated label (slice
// 43.0 worker-id contract).
func TestRecorderSessionRevokeNormalisesActorAndTenant(t *testing.T) {
	cases := []struct {
		name       string
		tenantID   string
		actorID    string
		wantTenant string
		wantActor  string
	}{
		{name: "happy_canonical_pair", tenantID: canonicalTenantUUID, actorID: "admin-7", wantTenant: canonicalTenantUUID, wantActor: "admin-7"},
		{name: "platform_self_scope", tenantID: "_platform", actorID: "platform-admin", wantTenant: "_platform", wantActor: "platform-admin"},
		{name: "non_canonical_tenant_collapsed", tenantID: "alice@acme.example", actorID: "admin-7", wantTenant: "_unknown", wantActor: "admin-7"},
		{name: "empty_actor_collapsed", tenantID: canonicalTenantUUID, actorID: "  ", wantTenant: canonicalTenantUUID, wantActor: "_unknown"},
		{name: "oversize_actor_collapsed", tenantID: canonicalTenantUUID, actorID: strings.Repeat("x", 200), wantTenant: canonicalTenantUUID, wantActor: "_unknown"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			registry := prometheus.NewRegistry()
			recorder, err := NewRecorder(registry)
			if err != nil {
				t.Fatalf("new recorder: %v", err)
			}
			recorder.ObserveSessionRevoke(testCase.tenantID, testCase.actorID)
			body := scrapeOpenMetrics(t, registry)
			sample := `apiserver_session_revoke_total{actor_id="` + testCase.wantActor + `",tenant_id="` + testCase.wantTenant + `"} 1.0`
			if !strings.Contains(body, sample) {
				t.Fatalf("scrape body missing %q for case %q: %s", sample, testCase.name, body)
			}
		})
	}
}

// TestRecorderTrustedProxyHSTSRoutesPreAuthThroughNormalize asserts the
// trusted-proxy pre-auth tenant scope is funnelled through
// NormalizeTenant. The expected value is "_unknown" (the catalog
// pre-auth sentinel).
func TestRecorderTrustedProxyHSTSRoutesPreAuthThroughNormalize(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := NewRecorder(registry)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}

	recorder.ObserveTrustedProxyHSTS("_unknown")
	recorder.ObserveTrustedProxyHSTS("")

	body := scrapeOpenMetrics(t, registry)
	if !strings.Contains(body, `apiserver_trusted_proxy_hsts_total{tenant_id="_unknown"} 2.0`) {
		t.Fatalf("expected single _unknown series with count 2; body=%s", body)
	}
	if strings.Contains(body, `tenant_id="_platform"`) {
		t.Fatalf("trusted-proxy HSTS leaked _platform series: %s", body)
	}
}

// TestRecorderSignInScrapeIsBoundedAcrossDistinctInputs guarantees the
// slice 43.1 contract that distinct caller inputs never widen the
// cardinality of the sign-in family beyond the documented allowlists.
func TestRecorderSignInScrapeIsBoundedAcrossDistinctInputs(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := NewRecorder(registry)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}

	distinct := []struct {
		tenant  string
		outcome string
	}{
		{tenant: canonicalTenantUUID, outcome: "success"},
		{tenant: "ALICE@acme.example", outcome: "success"},
		{tenant: "{0190f7c4-6c8d-7a01-9d2b-1ecabdff0011}", outcome: "rejected"},
		{tenant: "urn:uuid:0190f7c4-6c8d-7a01-9d2b-1ecabdff0011", outcome: "rejected"},
		{tenant: canonicalTenantUUID, outcome: "started"},
		{tenant: canonicalTenantUUID, outcome: "OK"},
	}
	for _, call := range distinct {
		recorder.ObserveSignIn(call.tenant, call.outcome, "")
	}

	body := scrapeOpenMetrics(t, registry)
	distinctSeries := strings.Count(body, "apiserver_sign_in_total{")
	if distinctSeries != 4 {
		t.Fatalf("apiserver_sign_in_total series count = %d, want 4 (canonical/success, _unknown/success, _unknown/rejected, canonical/failure via outcome-collapse); body=%s", distinctSeries, body)
	}
	for _, leak := range []string{
		`tenant_id="ALICE@acme.example"`,
		`tenant_id="{0190f7c4-6c8d-7a01-9d2b-1ecabdff0011}"`,
		`tenant_id="urn:uuid:`,
		`outcome="started"`,
		`outcome="OK"`,
	} {
		if strings.Contains(body, leak) {
			t.Fatalf("non-allowlisted label value %q leaked into scrape body: %s", leak, body)
		}
	}
}

func scrapeOpenMetrics(t *testing.T, gatherer prometheus.Gatherer) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Accept", "application/openmetrics-text")
	recorder := httptest.NewRecorder()
	handler(gatherer).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("scrape status = %d, want 200", recorder.Code)
	}
	return recorder.Body.String()
}
