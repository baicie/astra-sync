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

const (
	testTenantID  = "11111111-1111-4111-8111-111111111111"
	testRequestID = "d724ad9a-30a2-4dab-9704-2b01ea1f67e1"
)

func TestRecorderNormalizesLabelsAndRecordsRenderedDuration(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := NewRecorder(registry)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}

	recorder.ObserveRequest(testTenantID, "success", "jobs", testRequestID, 25*time.Millisecond, true)
	recorder.ObserveRequest("attacker-controlled", "unexpected", "/api/jobs/secret", "attacker-controlled", -time.Second, true)

	body := scrapeOpenMetrics(t, registry)
	for _, sample := range []string{
		`console_request_total{handler="jobs",outcome="success",tenant_id="` + testTenantID + `"} 1`,
		`console_request_total{handler="unknown",outcome="failure",tenant_id="_unknown"} 1`,
		`console_render_duration_seconds_count{handler="jobs"} 1`,
		`console_render_duration_seconds_count{handler="unknown"} 1`,
	} {
		if !strings.Contains(body, sample) {
			t.Fatalf("metrics body missing %q: %s", sample, body)
		}
	}
}

func TestRecorderAttachesCanonicalRequestIDExemplars(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := NewRecorder(registry)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}

	recorder.ObserveRequest(testTenantID, "success", "jobs", testRequestID, 25*time.Millisecond, true)

	body := scrapeOpenMetrics(t, registry)
	if count := strings.Count(body, `request_id="`+testRequestID+`"`); count != 2 {
		t.Fatalf("request_id exemplar count = %d, want 2: %s", count, body)
	}
}

func TestRecorderDropsNonCanonicalRequestIDExemplars(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := NewRecorder(registry)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}

	recorder.ObserveRequest(testTenantID, "success", "jobs", "", 25*time.Millisecond, true)
	recorder.ObserveRequest(testTenantID, "success", "jobs", "req-success", 30*time.Millisecond, true)
	recorder.ObserveRequest(testTenantID, "success", "jobs", strings.ToUpper(testRequestID), 35*time.Millisecond, true)
	recorder.ObserveRequest(testTenantID, "success", "jobs", strings.Repeat("a", 129), 40*time.Millisecond, true)

	body := scrapeOpenMetrics(t, registry)
	if strings.Contains(body, "request_id=") {
		t.Fatalf("non-canonical request ID was exposed as an exemplar: %s", body)
	}
}

func TestMetricsRegistered(t *testing.T) {
	requestTotal := ConsoleRequestTotal.WithLabelValues("tenant-a", "success", "dashboard")
	before := testutil.ToFloat64(requestTotal)
	requestTotal.Inc()
	if testutil.ToFloat64(requestTotal) != before+1 {
		t.Fatalf("console_request_total did not register")
	}
	ConsoleRenderDuration.WithLabelValues("dashboard").Observe(0.01)
	if testutil.CollectAndCount(ConsoleRenderDuration) == 0 {
		t.Fatalf("console_render_duration_seconds did not register")
	}
}

func TestHandlerExposesMetrics(t *testing.T) {
	ConsoleRequestTotal.WithLabelValues("scrape-tenant", "success", "dashboard").Inc()
	ConsoleRenderDuration.WithLabelValues("dashboard").Observe(0.01)

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
	for _, name := range []string{"console_request_total", "console_render_duration_seconds"} {
		if !strings.Contains(recorder.Body.String(), name) {
			t.Fatalf("/metrics body missing %s: %s", name, recorder.Body.String())
		}
	}
}

func scrapeOpenMetrics(t *testing.T, gatherer prometheus.Gatherer) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Accept", "application/openmetrics-text")
	recorder := httptest.NewRecorder()
	HandlerFor(gatherer).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("scrape status = %d, want 200", recorder.Code)
	}
	return recorder.Body.String()
}
