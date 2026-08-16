package connectiontestmetrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetricsRegistered(t *testing.T) {
	metric := ConnectionTestTotal.WithLabelValues("tenant-a", "success")
	before := testutil.ToFloat64(metric)
	metric.Inc()
	if testutil.ToFloat64(metric) != before+1 {
		t.Fatalf("connection_test_total did not register")
	}
}

func TestHandlerExposesMetrics(t *testing.T) {
	ConnectionTestTotal.WithLabelValues("scrape-tenant", "success").Inc()

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	recorder := httptest.NewRecorder()
	Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "connection_test_total") {
		t.Fatalf("/metrics body missing metric: %s", recorder.Body.String())
	}
}
