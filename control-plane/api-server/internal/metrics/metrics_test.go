package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

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
