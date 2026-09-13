package metrics_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"io.astrasync/control-plane/scheduler/internal/metrics"
)

const canonicalTenantUUID = "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"

// TestRecorderAssignmentRoutesThroughNormalize exercises the new
// Recorder.ObserveAssignment entry point on a table that covers the
// catalog assignment outcome allowlist (success | rejected | failure)
// plus the canonical-lowercase-UUID tenant rule (ADR-047) and the
// length-bounded worker-id rule (ADR-058 §3 NormalizeWorkerID).
func TestRecorderAssignmentRoutesThroughNormalize(t *testing.T) {
	cases := []struct {
		name       string
		tenantID   string
		workerID   string
		outcome    string
		wantTenant string
		wantWorker string
		wantOut    string
	}{
		{name: "happy_canonical_success", tenantID: canonicalTenantUUID, workerID: "worker-1", outcome: "success",
			wantTenant: canonicalTenantUUID, wantWorker: "worker-1", wantOut: "success"},
		{name: "happy_canonical_rejected", tenantID: canonicalTenantUUID, workerID: "worker-1", outcome: "rejected",
			wantTenant: canonicalTenantUUID, wantWorker: "worker-1", wantOut: "rejected"},
		{name: "happy_canonical_failure", tenantID: canonicalTenantUUID, workerID: "worker-1", outcome: "failure",
			wantTenant: canonicalTenantUUID, wantWorker: "worker-1", wantOut: "failure"},
		{name: "platform_self_scope", tenantID: "_platform", workerID: "worker-1", outcome: "success",
			wantTenant: "_platform", wantWorker: "worker-1", wantOut: "success"},
		{name: "non_canonical_tenant_collapsed", tenantID: "ALICE@acme.example", workerID: "worker-1", outcome: "success",
			wantTenant: "_unknown", wantWorker: "worker-1", wantOut: "success"},
		{name: "uppercase_uuid_collapsed", tenantID: strings.ToUpper(canonicalTenantUUID), workerID: "worker-1",
			outcome: "success", wantTenant: "_unknown", wantWorker: "worker-1", wantOut: "success"},
		{name: "empty_worker_id_collapsed", tenantID: canonicalTenantUUID, workerID: "", outcome: "success",
			wantTenant: canonicalTenantUUID, wantWorker: "_unknown", wantOut: "success"},
		{name: "whitespace_worker_id_collapsed", tenantID: canonicalTenantUUID, workerID: "   ",
			outcome: "success", wantTenant: canonicalTenantUUID, wantWorker: "_unknown", wantOut: "success"},
		{name: "non_allowlisted_outcome_collapsed", tenantID: canonicalTenantUUID, workerID: "worker-1",
			outcome: "OK", wantTenant: canonicalTenantUUID, wantWorker: "worker-1", wantOut: "failure"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			registry := prometheus.NewRegistry()
			recorder, err := metrics.NewRecorder(registry)
			if err != nil {
				t.Fatalf("new recorder: %v", err)
			}
			recorder.ObserveAssignment(testCase.tenantID, testCase.workerID, testCase.outcome, "request-id")
			body := scrapeOpenMetrics(t, registry)
			sample := `scheduler_job_assignment_total{outcome="` + testCase.wantOut +
				`",tenant_id="` + testCase.wantTenant +
				`",worker_id="` + testCase.wantWorker + `"} 1`
			if !strings.Contains(body, sample) {
				t.Fatalf("scrape body missing %q for case %q: %s", sample, testCase.name, body)
			}
		})
	}
}

// TestRecorderLeaseTakeoverRoutesThroughNormalize exercises the
// lease-takeover outcome allowlist (success only). Non-allowlisted
// outcome values collapse to "_unknown", matching the catalog
// documentation (ADR-058 §2 + ADR-060 §3).
func TestRecorderLeaseTakeoverRoutesThroughNormalize(t *testing.T) {
	cases := []struct {
		name       string
		tenantID   string
		outcome    string
		wantTenant string
		wantOut    string
	}{
		{name: "happy_canonical_success", tenantID: canonicalTenantUUID, outcome: "success",
			wantTenant: canonicalTenantUUID, wantOut: "success"},
		{name: "platform_self_scope", tenantID: "_platform", outcome: "success",
			wantTenant: "_platform", wantOut: "success"},
		{name: "non_canonical_tenant_collapsed", tenantID: "BOB@acme.example", outcome: "success",
			wantTenant: "_unknown", wantOut: "success"},
		{name: "non_allowlisted_outcome_collapsed", tenantID: canonicalTenantUUID, outcome: "elected",
			wantTenant: canonicalTenantUUID, wantOut: "_unknown"},
		{name: "empty_outcome_collapsed", tenantID: canonicalTenantUUID, outcome: "",
			wantTenant: canonicalTenantUUID, wantOut: "_unknown"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			registry := prometheus.NewRegistry()
			recorder, err := metrics.NewRecorder(registry)
			if err != nil {
				t.Fatalf("new recorder: %v", err)
			}
			recorder.ObserveLeaseTakeover(testCase.tenantID, testCase.outcome, "request-id")
			body := scrapeOpenMetrics(t, registry)
			sample := `scheduler_lease_takeover_total{outcome="` + testCase.wantOut +
				`",tenant_id="` + testCase.wantTenant + `"} 1`
			if !strings.Contains(body, sample) {
				t.Fatalf("scrape body missing %q for case %q: %s", sample, testCase.name, body)
			}
		})
	}
}

// TestRecorderReconcileRoutesThroughNormalize covers the histogram
// call site. The duration is recorded verbatim; only tenant_id
// routes through normalize.
func TestRecorderReconcileRoutesThroughNormalize(t *testing.T) {
	cases := []struct {
		name       string
		tenantID   string
		duration   time.Duration
		wantTenant string
	}{
		{name: "happy_canonical_uuid", tenantID: canonicalTenantUUID, duration: 50 * time.Millisecond,
			wantTenant: canonicalTenantUUID},
		{name: "platform_self_scope", tenantID: "_platform", duration: 10 * time.Millisecond,
			wantTenant: "_platform"},
		{name: "non_canonical_collapsed", tenantID: "alice@acme.example", duration: 100 * time.Millisecond,
			wantTenant: "_unknown"},
		{name: "negative_duration_clamped", tenantID: canonicalTenantUUID, duration: -50 * time.Millisecond,
			wantTenant: canonicalTenantUUID},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			registry := prometheus.NewRegistry()
			recorder, err := metrics.NewRecorder(registry)
			if err != nil {
				t.Fatalf("new recorder: %v", err)
			}
			recorder.ObserveReconcile(testCase.tenantID, testCase.duration)
			body := scrapeOpenMetrics(t, registry)
			sample := `scheduler_job_reconcile_duration_seconds_count{tenant_id="` + testCase.wantTenant + `"} 1`
			if !strings.Contains(body, sample) {
				t.Fatalf("scrape body missing %q for case %q: %s", sample, testCase.name, body)
			}
		})
	}
}

// TestRecorderScrapeIsBoundedAcrossDistinctInputs guards the
// slice-44 contract that distinct caller inputs never widen the
// cardinality of the three Scheduler families beyond the documented
// allowlists. It also asserts that the package-level CounterVec /
// HistogramVec surface remains untouched (the next test covers that
// contract).
func TestRecorderScrapeIsBoundedAcrossDistinctInputs(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := metrics.NewRecorder(registry)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}

	distinct := []struct {
		tenant     string
		worker     string
		outcome    string
		reconcile  bool
		duration   time.Duration
		revokeOnly bool
	}{
		{tenant: canonicalTenantUUID, worker: "worker-a", outcome: "success"},
		{tenant: "ALICE@acme.example", worker: "worker-a", outcome: "success"},
		{tenant: "{" + canonicalTenantUUID + "}", worker: "worker-b", outcome: "rejected"},
		{tenant: canonicalTenantUUID, worker: "worker-a", outcome: "OK"},
		{tenant: canonicalTenantUUID, worker: "worker-a", outcome: "elected"},
		{tenant: canonicalTenantUUID, worker: "", outcome: "success"},
		{tenant: canonicalTenantUUID, worker: "   ", outcome: "success"},
		{tenant: canonicalTenantUUID, worker: "worker-a", outcome: "rejected", revokeOnly: true},
		{tenant: "BOB@acme.example", worker: "worker-c", outcome: "success", revokeOnly: true},
		{tenant: canonicalTenantUUID, worker: "worker-a", outcome: "success", reconcile: true, duration: 50 * time.Millisecond},
		{tenant: "alice@acme.example", worker: "worker-a", outcome: "success", reconcile: true, duration: 10 * time.Millisecond},
	}
	for _, call := range distinct {
		if call.reconcile {
			recorder.ObserveReconcile(call.tenant, call.duration)
		}
		if call.revokeOnly {
			recorder.ObserveLeaseTakeover(call.tenant, call.outcome, "request-id")
		}
		recorder.ObserveAssignment(call.tenant, call.worker, call.outcome, "request-id")
	}

	body := scrapeOpenMetrics(t, registry)
	assignmentSeries := strings.Count(body, "scheduler_job_assignment_total{")
	revokeSeries := strings.Count(body, "scheduler_lease_takeover_total{")
	reconcileSeries := strings.Count(body, "scheduler_job_reconcile_duration_seconds_count{")
	if assignmentSeries != 7 {
		t.Fatalf("scheduler_job_assignment_total series count = %d, want 7 (canonical tenant worker-a success + canonical tenant worker-a rejected + canonical tenant worker-a failure (collapsed from OK / elected) + canonical tenant _unknown worker success (collapsed from empty / whitespace) + non-canonical collapsed tenant worker-a success + non-canonical collapsed tenant worker-b rejected + non-canonical collapsed tenant worker-c success); body=%s",
			assignmentSeries, body)
	}
	if revokeSeries != 2 {
		t.Fatalf("scheduler_lease_takeover_total series count = %d, want 2 (canonical tenant success + non-canonical collapsed tenant success); body=%s",
			revokeSeries, body)
	}
	if reconcileSeries != 2 {
		t.Fatalf("scheduler_job_reconcile_duration_seconds_count series count = %d, want 2 (canonical tenant + non-canonical collapsed tenant); body=%s",
			reconcileSeries, body)
	}
	for _, leak := range []string{
		`tenant_id="ALICE@acme.example"`,
		`tenant_id="BOB@acme.example"`,
		`tenant_id="alice@acme.example"`,
		`tenant_id="` + strings.ToUpper(canonicalTenantUUID) + `"`,
		`tenant_id="{` + canonicalTenantUUID + `}"`,
		`worker_id="worker-a",outcome="OK"`,
		`worker_id="worker-a",outcome="elected"`,
		`outcome="OK"`,
		`outcome="elected"`,
		`worker_id=""`,
		`worker_id="   "`,
	} {
		if strings.Contains(body, leak) {
			t.Fatalf("non-allowlisted label value %q leaked into scrape body: %s", leak, body)
		}
	}
}

// TestRecorderNilReceiverIsSafe documents the slice-44 contract that
// nil-receiver Recorder methods are no-ops. Call sites at the
// boundary (Scheduler reconcile + dispatch) own the Recorder;
// callers should never have to nil-check before calling Observe*.
func TestRecorderNilReceiverIsSafe(t *testing.T) {
	var recorder *metrics.Recorder
	recorder.ObserveAssignment(canonicalTenantUUID, "worker-a", "success", "request-id")
	recorder.ObserveLeaseTakeover(canonicalTenantUUID, "success", "request-id")
	recorder.ObserveReconcile(canonicalTenantUUID, 50*time.Millisecond)
}

// TestNewRecorderRejectsNilRegisterer asserts the sentinel error path.
func TestNewRecorderRejectsNilRegisterer(t *testing.T) {
	if _, err := metrics.NewRecorder(nil); !errors.Is(err, metrics.ErrNilRegisterer) {
		t.Fatalf("NewRecorder(nil) err = %v, want ErrNilRegisterer", err)
	}
}

// TestNewRecorderRejectsDuplicateRegistration asserts that a second
// Recorder registration against the same registerer fails with the
// duplicate-metric sentinel. This is the contract that prevents two
// Scheduler processes (for example, a primary + a shadow) from
// silently re-registering the same metric on a shared registry.
func TestNewRecorderRejectsDuplicateRegistration(t *testing.T) {
	registry := prometheus.NewRegistry()
	if _, err := metrics.NewRecorder(registry); err != nil {
		t.Fatalf("first NewRecorder: %v", err)
	}
	_, err := metrics.NewRecorder(registry)
	if !errors.Is(err, metrics.ErrDuplicateMetric) {
		t.Fatalf("second NewRecorder err = %v, want ErrDuplicateMetric", err)
	}
}

// TestPackageLevelVecsRemainRegistered locks the slice-44
// backward-compatibility contract: the package-level CounterVec /
// HistogramVec surface (JobAssignmentTotal / LeaseTakeoverTotal /
// JobReconcileDuration, registered against the default registry)
// continues to work for any consumer that uses the pre-slice-44
// import pattern. The Recorder path is additive, not destructive.
func TestPackageLevelVecsRemainRegistered(t *testing.T) {
	metrics.JobAssignmentTotal.WithLabelValues(canonicalTenantUUID, "worker-a", "success").Inc()
	metrics.LeaseTakeoverTotal.WithLabelValues(canonicalTenantUUID, "success").Inc()
	metrics.JobReconcileDuration.WithLabelValues(canonicalTenantUUID).Observe(0.05)

	assignment := metrics.JobAssignmentTotal.WithLabelValues(canonicalTenantUUID, "worker-a", "success")
	if got := testutil.ToFloat64(assignment); got != 1 {
		t.Fatalf("JobAssignmentTotal sample = %v, want 1", got)
	}
	lease := metrics.LeaseTakeoverTotal.WithLabelValues(canonicalTenantUUID, "success")
	if got := testutil.ToFloat64(lease); got != 1 {
		t.Fatalf("LeaseTakeoverTotal sample = %v, want 1", got)
	}

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	recorder := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("Handler() scrape status = %d, want 200", recorder.Code)
	}
	for _, want := range []string{
		`scheduler_job_assignment_total{outcome="success",tenant_id="` + canonicalTenantUUID + `",worker_id="worker-a"} 1`,
		`scheduler_lease_takeover_total{outcome="success",tenant_id="` + canonicalTenantUUID + `"} 1`,
		`scheduler_job_reconcile_duration_seconds_count{tenant_id="` + canonicalTenantUUID + `"} 1`,
	} {
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
