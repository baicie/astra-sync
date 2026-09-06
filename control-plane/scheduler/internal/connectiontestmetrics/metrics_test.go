package connectiontestmetrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRecorderObservesOnlyBoundedOutcomes(t *testing.T) {
	recorder, err := NewRecorder(prometheus.NewRegistry())
	if err != nil {
		t.Fatalf("create recorder: %v", err)
	}

	recorder.Observe("tenant-a", OutcomeSuccess)
	recorder.Observe("tenant-a", OutcomeRejected)
	recorder.Observe("tenant-a", "unexpected")
	recorder.Observe(" ", OutcomeFailure)

	if got := testutil.ToFloat64(recorder.ConnectionTestTotal.WithLabelValues("tenant-a", OutcomeSuccess)); got != 1 {
		t.Fatalf("successful connection tests = %v, want 1", got)
	}
	if got := testutil.ToFloat64(recorder.ConnectionTestTotal.WithLabelValues("tenant-a", OutcomeRejected)); got != 1 {
		t.Fatalf("rejected connection tests = %v, want 1", got)
	}
	if got := testutil.ToFloat64(recorder.ConnectionTestTotal.WithLabelValues("tenant-a", OutcomeFailure)); got != 1 {
		t.Fatalf("failed connection tests = %v, want 1", got)
	}
	if got := testutil.ToFloat64(recorder.ConnectionTestTotal.WithLabelValues("_unknown", OutcomeFailure)); got != 1 {
		t.Fatalf("unknown-tenant connection tests = %v, want 1", got)
	}
}

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
