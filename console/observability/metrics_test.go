package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetricsRegistered(t *testing.T) {
	ConsoleRequestTotal.WithLabelValues("tenant-a", "success", "dashboard").Inc()
	if testutil.ToFloat64(ConsoleRequestTotal.WithLabelValues("tenant-a", "success", "dashboard")) != 1 {
		t.Fatalf("console_request_total did not register")
	}
	ConsoleRenderDuration.WithLabelValues("dashboard").Observe(0.01)
	if testutil.CollectAndCount(ConsoleRenderDuration) == 0 {
		t.Fatalf("console_render_duration_seconds did not register")
	}
}

func TestHandlerExposesMetrics(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	recorder := httptest.NewRecorder()
	Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", recorder.Code)
	}
	for _, name := range []string{"console_request_total", "console_render_duration_seconds"} {
		if !strings.Contains(recorder.Body.String(), name) {
			t.Fatalf("/metrics body missing %s: %s", name, recorder.Body.String())
		}
	}
}
