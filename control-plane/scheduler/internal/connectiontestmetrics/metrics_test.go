package connectiontestmetrics_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"io.astrasync/control-plane/scheduler/internal/connectiontestmetrics"
)

const canonicalTenantUUID = "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"

// TestRecorderRoutesThroughNormalize exercises the slice-45.1
// migration: connection_test_total labels must route through
// io.astrasync/control-plane/observability/normalize so every
// Recorder method enforces the ADR-058 §3 contract.
func TestRecorderRoutesThroughNormalize(t *testing.T) {
	cases := []struct {
		name       string
		tenantID   string
		outcome    string
		wantTenant string
		wantOut    string
	}{
		{name: "happy_canonical_success", tenantID: canonicalTenantUUID, outcome: connectiontestmetrics.OutcomeSuccess,
			wantTenant: canonicalTenantUUID, wantOut: connectiontestmetrics.OutcomeSuccess},
		{name: "happy_canonical_rejected", tenantID: canonicalTenantUUID, outcome: connectiontestmetrics.OutcomeRejected,
			wantTenant: canonicalTenantUUID, wantOut: connectiontestmetrics.OutcomeRejected},
		{name: "happy_canonical_failure", tenantID: canonicalTenantUUID, outcome: connectiontestmetrics.OutcomeFailure,
			wantTenant: canonicalTenantUUID, wantOut: connectiontestmetrics.OutcomeFailure},
		{name: "platform_self_scope", tenantID: "_platform", outcome: connectiontestmetrics.OutcomeSuccess,
			wantTenant: "_platform", wantOut: connectiontestmetrics.OutcomeSuccess},
		{name: "non_canonical_tenant_collapsed", tenantID: "ALICE@acme.example", outcome: connectiontestmetrics.OutcomeSuccess,
			wantTenant: "_unknown", wantOut: connectiontestmetrics.OutcomeSuccess},
		{name: "uppercase_uuid_collapsed", tenantID: strings.ToUpper(canonicalTenantUUID), outcome: connectiontestmetrics.OutcomeSuccess,
			wantTenant: "_unknown", wantOut: connectiontestmetrics.OutcomeSuccess},
		{name: "braced_uuid_collapsed", tenantID: "{" + canonicalTenantUUID + "}", outcome: connectiontestmetrics.OutcomeSuccess,
			wantTenant: "_unknown", wantOut: connectiontestmetrics.OutcomeSuccess},
		{name: "empty_tenant_collapsed", tenantID: "", outcome: connectiontestmetrics.OutcomeSuccess,
			wantTenant: "_unknown", wantOut: connectiontestmetrics.OutcomeSuccess},
		{name: "whitespace_tenant_collapsed", tenantID: "   ", outcome: connectiontestmetrics.OutcomeFailure,
			wantTenant: "_unknown", wantOut: connectiontestmetrics.OutcomeFailure},
		{name: "non_allowlisted_outcome_collapsed", tenantID: canonicalTenantUUID, outcome: "OK",
			wantTenant: canonicalTenantUUID, wantOut: connectiontestmetrics.OutcomeFailure},
		{name: "empty_outcome_collapsed", tenantID: canonicalTenantUUID, outcome: "",
			wantTenant: canonicalTenantUUID, wantOut: connectiontestmetrics.OutcomeFailure},
		{name: "whitespace_outcome_collapsed", tenantID: canonicalTenantUUID, outcome: "  ",
			wantTenant: canonicalTenantUUID, wantOut: connectiontestmetrics.OutcomeFailure},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			registry := prometheus.NewRegistry()
			recorder, err := connectiontestmetrics.NewRecorder(registry)
			if err != nil {
				t.Fatalf("new recorder: %v", err)
			}
			recorder.Observe(testCase.tenantID, testCase.outcome)
			body := scrapeOpenMetrics(t, registry)
			sample := `connection_test_total{outcome="` + testCase.wantOut +
				`",tenant_id="` + testCase.wantTenant + `"} 1`
			if !strings.Contains(body, sample) {
				t.Fatalf("scrape body missing %q for case %q: %s", sample, testCase.name, body)
			}
		})
	}
}

// TestRecorderScrapeIsBoundedAcrossDistinctInputs guards the
// slice-45 contract that distinct caller inputs never widen the
// cardinality of connection_test_total beyond the documented
// allowlists. It mirrors the Phase 18 slice-44.2 contract.
func TestRecorderScrapeIsBoundedAcrossDistinctInputs(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := connectiontestmetrics.NewRecorder(registry)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}

	distinct := []struct {
		tenant  string
		outcome string
	}{
		{tenant: canonicalTenantUUID, outcome: connectiontestmetrics.OutcomeSuccess},
		{tenant: "ALICE@acme.example", outcome: connectiontestmetrics.OutcomeSuccess},
		{tenant: "{" + canonicalTenantUUID + "}", outcome: connectiontestmetrics.OutcomeRejected},
		{tenant: strings.ToUpper(canonicalTenantUUID), outcome: connectiontestmetrics.OutcomeSuccess},
		{tenant: canonicalTenantUUID, outcome: "OK"},
		{tenant: canonicalTenantUUID, outcome: connectiontestmetrics.OutcomeFailure},
		{tenant: canonicalTenantUUID, outcome: ""},
		{tenant: canonicalTenantUUID, outcome: "  "},
		{tenant: "", outcome: connectiontestmetrics.OutcomeSuccess},
		{tenant: "   ", outcome: connectiontestmetrics.OutcomeFailure},
		{tenant: "BOB@acme.example", outcome: connectiontestmetrics.OutcomeRejected},
	}
	for _, call := range distinct {
		recorder.Observe(call.tenant, call.outcome)
	}

	body := scrapeOpenMetrics(t, registry)
	series := strings.Count(body, "connection_test_total{")
	if series != 5 {
		t.Fatalf("connection_test_total series count = %d, want 5 (canonical tenant success + canonical tenant failure (collapsed from OK / empty / whitespace / outcome=failure) + non-canonical collapsed tenant success + non-canonical collapsed tenant rejected + non-canonical collapsed tenant failure); body=%s",
			series, body)
	}
	for _, leak := range []string{
		`tenant_id="ALICE@acme.example"`,
		`tenant_id="BOB@acme.example"`,
		`tenant_id="{` + canonicalTenantUUID + `}"`,
		`tenant_id="` + strings.ToUpper(canonicalTenantUUID) + `"`,
		`outcome="OK"`,
		`tenant_id=""`,
		`tenant_id="   "`,
	} {
		if strings.Contains(body, leak) {
			t.Fatalf("non-allowlisted label value %q leaked into scrape body: %s", leak, body)
		}
	}
}

// TestRecorderNilReceiverIsSafe documents the slice-45 contract
// that nil-receiver Recorder methods are no-ops. Call sites at
// the boundary (Connection Test Executor reconcile + dispatch)
// own the Recorder; callers should never have to nil-check
// before calling Observe.
func TestRecorderNilReceiverIsSafe(t *testing.T) {
	var recorder *connectiontestmetrics.Recorder
	recorder.Observe(canonicalTenantUUID, connectiontestmetrics.OutcomeSuccess)
}

// TestNewRecorderRejectsNilRegisterer asserts the sentinel error
// path. Mirrors the Phase 18 slice-44.2 design.
func TestNewRecorderRejectsNilRegisterer(t *testing.T) {
	if _, err := connectiontestmetrics.NewRecorder(nil); !errors.Is(err, connectiontestmetrics.ErrNilRegisterer) {
		t.Fatalf("NewRecorder(nil) err = %v, want ErrNilRegisterer", err)
	}
}

// TestNewRecorderRejectsDuplicateRegistration asserts that a
// second Recorder registration against the same registerer fails
// with the duplicate-metric sentinel. This is the contract that
// prevents two Connection Test Executor processes (for example,
// a primary + a shadow) from silently re-registering the same
// metric on a shared registry.
func TestNewRecorderRejectsDuplicateRegistration(t *testing.T) {
	registry := prometheus.NewRegistry()
	if _, err := connectiontestmetrics.NewRecorder(registry); err != nil {
		t.Fatalf("first NewRecorder: %v", err)
	}
	_, err := connectiontestmetrics.NewRecorder(registry)
	if !errors.Is(err, connectiontestmetrics.ErrDuplicateMetric) {
		t.Fatalf("second NewRecorder err = %v, want ErrDuplicateMetric", err)
	}
}

// TestPackageLevelVecRemainsRegistered locks the slice-45
// backward-compatibility contract: the package-level CounterVec
// surface (ConnectionTestTotal, registered against the default
// registry) continues to work for any consumer that uses the
// pre-slice-45 import pattern. The Recorder path is additive,
// not destructive. This test is the black-box mirror of the
// pre-slice-45 white-box TestMetricsRegistered / TestRecorderObservesOnlyBoundedOutcomes
// contract.
func TestPackageLevelVecRemainsRegistered(t *testing.T) {
	successBefore := testutil.ToFloat64(connectiontestmetrics.ConnectionTestTotal.WithLabelValues(canonicalTenantUUID, connectiontestmetrics.OutcomeSuccess))
	connectiontestmetrics.ConnectionTestTotal.WithLabelValues(canonicalTenantUUID, connectiontestmetrics.OutcomeSuccess).Inc()
	if got := testutil.ToFloat64(connectiontestmetrics.ConnectionTestTotal.WithLabelValues(canonicalTenantUUID, connectiontestmetrics.OutcomeSuccess)); got != successBefore+1 {
		t.Fatalf("ConnectionTestTotal sample = %v, want %v", got, successBefore+1)
	}

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	recorder := httptest.NewRecorder()
	connectiontestmetrics.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("Handler() scrape status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "connection_test_total") {
		t.Fatalf("legacy Handler() body missing connection_test_total: %s", recorder.Body.String())
	}
}

// TestHandlerForGathererExposesRecorderMetrics asserts the
// Recorder-owned registry scrape path. A long-running consumer
// (Connection Test Executor daemon) can host the Recorder
// through its own registerer and expose the family from its own
// /metrics endpoint without competing for the global default
// registry. Mirrors the Phase 18 slice-44.2 HandlerFor design.
func TestHandlerForGathererExposesRecorderMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := connectiontestmetrics.NewRecorder(registry)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}
	recorder.Observe(canonicalTenantUUID, connectiontestmetrics.OutcomeSuccess)

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Accept", "application/openmetrics-text")
	recorderWriter := httptest.NewRecorder()
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{EnableOpenMetrics: true}).ServeHTTP(recorderWriter, request)
	if recorderWriter.Code != http.StatusOK {
		t.Fatalf("HandlerFor scrape status = %d, want 200", recorderWriter.Code)
	}
	want := `connection_test_total{outcome="success",tenant_id="` + canonicalTenantUUID + `"} 1`
	if !strings.Contains(recorderWriter.Body.String(), want) {
		t.Fatalf("HandlerFor body missing %q: %s", want, recorderWriter.Body.String())
	}

	handler := connectiontestmetrics.HandlerFor(registry)
	request2 := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	recorderWriter2 := httptest.NewRecorder()
	handler.ServeHTTP(recorderWriter2, request2)
	if recorderWriter2.Code != http.StatusOK {
		t.Fatalf("HandlerFor(gatherer) scrape status = %d, want 200", recorderWriter2.Code)
	}
	if !strings.Contains(recorderWriter2.Body.String(), want) {
		t.Fatalf("HandlerFor(gatherer) body missing %q: %s", want, recorderWriter2.Body.String())
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
