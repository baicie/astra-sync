package metrics

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const canonicalTenantUUID = "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"

func TestRecorderNormalizesBoundedLabelsAndDuration(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := NewRecorder(registry)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}

	recorder.ObserveReconcile("caller-controlled", "unexpected", -time.Second)
	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	if len(metricFamilies) != 1 || metricFamilies[0].GetName() != "controller_job_controller_reconcile_duration_seconds" {
		t.Fatalf("unexpected metric families: %+v", metricFamilies)
	}
	metric := metricFamilies[0].GetMetric()
	if len(metric) != 1 {
		t.Fatalf("unexpected normalized labels: %+v", metric)
	}
	labels := make(map[string]string, len(metric[0].GetLabel()))
	for _, label := range metric[0].GetLabel() {
		labels[label.GetName()] = label.GetValue()
	}
	if labels["tenant_id"] != "_unknown" || labels["outcome"] != "failure" {
		t.Fatalf("unexpected normalized labels: %+v", labels)
	}
	if metric[0].GetHistogram().GetSampleSum() != 0 {
		t.Fatalf("negative duration was not clamped: %+v", metric[0].GetHistogram())
	}
}

func TestRecorderRecordsSuccessfulReconcile(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := NewRecorder(registry)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}
	recorder.ObserveReconcile(canonicalTenantUUID, "success", 25*time.Millisecond)
	metricFamilies, err := registry.Gather()
	if err != nil || len(metricFamilies) != 1 || math.Abs(metricFamilies[0].GetMetric()[0].GetHistogram().GetSampleSum()-0.025) > 1e-9 {
		t.Fatalf("gather registered metric: families=%d err=%v", len(metricFamilies), err)
	}
}

// TestRecorderJobStateTransitionsEmitsBoundedSeries exercises the
// controller_job_state_total Recorder method on a table that covers the
// Job state machine (ADR-029) plus three rejection paths
// (non-canonical tenant, empty namespace, oversize state names).
func TestRecorderJobStateTransitionsEmitsBoundedSeries(t *testing.T) {
	cases := []struct {
		name       string
		tenantID   string
		namespace  string
		fromState  string
		toState    string
		wantTenant string
		wantNS     string
		wantFrom   string
		wantTo     string
	}{
		{name: "happy_initializing_to_compiling", tenantID: canonicalTenantUUID, namespace: "default",
			fromState: "initializing", toState: "compiling",
			wantTenant: canonicalTenantUUID, wantNS: "default", wantFrom: "initializing", wantTo: "compiling"},
		{name: "happy_running_to_failing", tenantID: canonicalTenantUUID, namespace: "prod",
			fromState: "running", toState: "failing",
			wantTenant: canonicalTenantUUID, wantNS: "prod", wantFrom: "running", wantTo: "failing"},
		{name: "platform_self_scope", tenantID: "_platform", namespace: "default",
			fromState: "ready", toState: "running",
			wantTenant: "_platform", wantNS: "default", wantFrom: "ready", wantTo: "running"},
		{name: "non_canonical_tenant_collapsed", tenantID: "ALICE@acme.example", namespace: "default",
			fromState: "ready", toState: "running",
			wantTenant: "_unknown", wantNS: "default", wantFrom: "ready", wantTo: "running"},
		{name: "empty_namespace_collapsed", tenantID: canonicalTenantUUID, namespace: "   ",
			fromState: "ready", toState: "running",
			wantTenant: canonicalTenantUUID, wantNS: "_unknown", wantFrom: "ready", wantTo: "running"},
		{name: "oversize_from_state_collapsed", tenantID: canonicalTenantUUID, namespace: "default",
			fromState: strings.Repeat("x", 64), toState: "running",
			wantTenant: canonicalTenantUUID, wantNS: "default", wantFrom: "_unknown", wantTo: "running"},
		{name: "empty_to_state_collapsed", tenantID: canonicalTenantUUID, namespace: "default",
			fromState: "running", toState: "",
			wantTenant: canonicalTenantUUID, wantNS: "default", wantFrom: "running", wantTo: "_unknown"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			registry := prometheus.NewRegistry()
			recorder, err := NewRecorder(registry)
			if err != nil {
				t.Fatalf("new recorder: %v", err)
			}
			recorder.ObserveStateTransition(testCase.tenantID, testCase.namespace, testCase.fromState, testCase.toState)
			body := scrapeOpenMetrics(t, registry)
			sample := `controller_job_state_total{from_state="` + testCase.wantFrom + `",namespace="` + testCase.wantNS +
				`",tenant_id="` + testCase.wantTenant + `",to_state="` + testCase.wantTo + `"} 1.0`
			if !strings.Contains(body, sample) {
				t.Fatalf("scrape body missing %q for case %q: %s", sample, testCase.name, body)
			}
			// Defensive: raw caller input must never leak into a series.
			for _, leak := range []string{testCase.tenantID, testCase.fromState, testCase.toState, testCase.namespace} {
				if leak == "" || leak == canonicalTenantUUID || leak == "_platform" || leak == testCase.wantNS ||
					leak == testCase.wantFrom || leak == testCase.wantTo {
					continue
				}
				if strings.Contains(body, `="`+leak+`"`) {
					t.Fatalf("non-normalised label value %q leaked into scrape body for case %q: %s", leak, testCase.name, body)
				}
			}
		})
	}
}

// TestRecorderEpochFenceEnforcesAllowlist covers the second Controller
// metric family wired in slice 43.3. The outcome allowlist is
// `success|fenced|failure` (ADR-058 §3); `fenced` records a clean
// fence of an obsolete writer and is exclusive to this metric.
func TestRecorderEpochFenceEnforcesAllowlist(t *testing.T) {
	cases := []struct {
		name        string
		tenantID    string
		outcome     string
		wantTenant  string
		wantOutcome string
	}{
		{name: "happy_canonical_success", tenantID: canonicalTenantUUID, outcome: "success",
			wantTenant: canonicalTenantUUID, wantOutcome: "success"},
		{name: "fenced_canonical", tenantID: canonicalTenantUUID, outcome: "fenced",
			wantTenant: canonicalTenantUUID, wantOutcome: "fenced"},
		{name: "failure_canonical", tenantID: canonicalTenantUUID, outcome: "failure",
			wantTenant: canonicalTenantUUID, wantOutcome: "failure"},
		{name: "platform_self_scope", tenantID: "_platform", outcome: "fenced",
			wantTenant: "_platform", wantOutcome: "fenced"},
		{name: "non_canonical_tenant_collapsed", tenantID: "{not-a-uuid}", outcome: "success",
			wantTenant: "_unknown", wantOutcome: "success"},
		{name: "non_allowlisted_outcome_collapsed", tenantID: canonicalTenantUUID, outcome: "OK",
			wantTenant: canonicalTenantUUID, wantOutcome: "failure"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			registry := prometheus.NewRegistry()
			recorder, err := NewRecorder(registry)
			if err != nil {
				t.Fatalf("new recorder: %v", err)
			}
			recorder.ObserveEpochFence(testCase.tenantID, testCase.outcome)
			body := scrapeOpenMetrics(t, registry)
			sample := `controller_epoch_fence_total{outcome="` + testCase.wantOutcome + `",tenant_id="` + testCase.wantTenant + `"} 1.0`
			if !strings.Contains(body, sample) {
				t.Fatalf("scrape body missing %q for case %q: %s", sample, testCase.name, body)
			}
			if testCase.outcome != "" && !strings.Contains(testCase.wantOutcome, testCase.outcome) &&
				testCase.outcome != testCase.wantOutcome && strings.Contains(body, `outcome="`+testCase.outcome+`"`) {
				t.Fatalf("non-allowlisted outcome %q leaked into series for case %q: %s", testCase.outcome, testCase.name, body)
			}
		})
	}
}

// TestRecorderScrapeIsBoundedAcrossDistinctInputs guards the slice 43.3
// contract that distinct caller inputs never widen the cardinality of
// controller_job_state_total or controller_epoch_fence_total beyond the
// documented allowlists.
func TestRecorderScrapeIsBoundedAcrossDistinctInputs(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := NewRecorder(registry)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}

	distinct := []struct {
		tenant    string
		namespace string
		fromState string
		toState   string
		outcome   string
	}{
		{tenant: canonicalTenantUUID, namespace: "default", fromState: "initializing", toState: "compiling", outcome: "success"},
		{tenant: "ALICE@acme.example", namespace: "default", fromState: "initializing", toState: "compiling", outcome: "success"},
		{tenant: "{0190f7c4-6c8d-7a01-9d2b-1ecabdff0011}", namespace: "default", fromState: "initializing", toState: "compiling", outcome: "rejected"},
		{tenant: canonicalTenantUUID, namespace: "default", fromState: "ready", toState: "running", outcome: "OK"},
		{tenant: canonicalTenantUUID, namespace: strings.Repeat("x", 200), fromState: "ready", toState: "running", outcome: "fenced"},
		{tenant: canonicalTenantUUID, namespace: "default", fromState: strings.Repeat("y", 64), toState: "running", outcome: "success"},
		{tenant: canonicalTenantUUID, namespace: "default", fromState: "running", toState: strings.Repeat("z", 64), outcome: "failure"},
		{tenant: canonicalTenantUUID, namespace: "default", fromState: "ready", toState: "running", outcome: "INVALID"},
	}
	for _, call := range distinct {
		recorder.ObserveStateTransition(call.tenant, call.namespace, call.fromState, call.toState)
		recorder.ObserveEpochFence(call.tenant, call.outcome)
	}

	body := scrapeOpenMetrics(t, registry)
	jobStateSeries := strings.Count(body, "controller_job_state_total{")
	epochFenceSeries := strings.Count(body, "controller_epoch_fence_total{")
	if jobStateSeries != 6 {
		t.Fatalf("controller_job_state_total series count = %d, want 6 (canonical/non-canonical tenant / oversize-ns / oversize-from / oversize-to + duplicate collapse); body=%s", jobStateSeries, body)
	}
	if epochFenceSeries != 5 {
		t.Fatalf("controller_epoch_fence_total series count = %d, want 5 (success / fenced / failure via collapse + per-tenant variants); body=%s", epochFenceSeries, body)
	}
	for _, leak := range []string{
		`tenant_id="ALICE@acme.example"`,
		`tenant_id="{0190f7c4-6c8d-7a01-9d2b-1ecabdff0011}"`,
		`outcome="rejected"`,
		`outcome="OK"`,
		`outcome="INVALID"`,
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
	promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{EnableOpenMetrics: true}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("scrape status = %d, want 200", recorder.Code)
	}
	return recorder.Body.String()
}
