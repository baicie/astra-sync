package authmetrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetricsRegistered(t *testing.T) {
	signIn := AuthSignInTotal.WithLabelValues("tenant-a", "success")
	signInBefore := testutil.ToFloat64(signIn)
	signIn.Inc()
	if testutil.ToFloat64(signIn) != signInBefore+1 {
		t.Fatalf("auth_sign_in_total did not register")
	}
	sessionRevoke := AuthSessionRevokeTotal.WithLabelValues("tenant-a")
	sessionRevokeBefore := testutil.ToFloat64(sessionRevoke)
	sessionRevoke.Inc()
	if testutil.ToFloat64(sessionRevoke) != sessionRevokeBefore+1 {
		t.Fatalf("auth_session_revoke_total did not register")
	}
}

func TestHandlerExposesMetrics(t *testing.T) {
	AuthSignInTotal.WithLabelValues("scrape-tenant", "success").Inc()
	AuthSessionRevokeTotal.WithLabelValues("scrape-tenant").Inc()

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	recorder := httptest.NewRecorder()
	Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", recorder.Code)
	}
	for _, name := range []string{"auth_sign_in_total", "auth_session_revoke_total"} {
		if !strings.Contains(recorder.Body.String(), name) {
			t.Fatalf("/metrics body missing %s: %s", name, recorder.Body.String())
		}
	}
}
