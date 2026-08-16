package connectiontestmetrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetricsRegistered(t *testing.T) {
	ConnectionTestTotal.WithLabelValues("tenant-a", "success").Inc()
	if testutil.ToFloat64(ConnectionTestTotal.WithLabelValues("tenant-a", "success")) != 1 {
		t.Fatalf("connection_test_executor_connection_test_total did not register")
	}
}

func TestHandlerExposesMetrics(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	recorder := httptest.NewRecorder()
	Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "connection_test_executor_connection_test_total") {
		t.Fatalf("/metrics body missing metric: %s", recorder.Body.String())
	}
}
