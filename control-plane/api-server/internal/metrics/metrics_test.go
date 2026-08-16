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
