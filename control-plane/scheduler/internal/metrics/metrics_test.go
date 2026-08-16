package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetricsRegistered(t *testing.T) {
	JobAssignmentTotal.WithLabelValues("tenant-a", "success").Inc()
	if testutil.ToFloat64(JobAssignmentTotal.WithLabelValues("tenant-a", "success")) != 1 {
		t.Fatalf("scheduler_job_assignment_total did not register")
	}

	LeaseTakeoverTotal.WithLabelValues("elected").Inc()
	if testutil.ToFloat64(LeaseTakeoverTotal.WithLabelValues("elected")) != 1 {
		t.Fatalf("scheduler_lease_takeover_total did not register")
	}

	JobReconcileDuration.WithLabelValues("tenant-a").Observe(0.05)
	if testutil.CollectAndCount(JobReconcileDuration) == 0 {
		t.Fatalf("scheduler_job_reconcile_duration_seconds did not register")
	}
}

func TestHandlerExposesMetrics(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	recorder := httptest.NewRecorder()
	Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, name := range []string{
		"scheduler_job_assignment_total",
		"scheduler_lease_takeover_total",
		"scheduler_job_reconcile_duration_seconds",
	} {
		if !strings.Contains(body, name) {
			t.Fatalf("/metrics body missing %s: %s", name, body)
		}
	}
}
