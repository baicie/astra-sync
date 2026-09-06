package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	consoleTenantUnknown   = "_unknown"
	consoleOutcomeSuccess  = "success"
	consoleOutcomeRejected = "rejected"
	consoleOutcomeFailure  = "failure"
)

func observeRequests(next http.Handler, recorder RequestMetrics, clock func() time.Time) http.Handler {
	if recorder == nil {
		return next
	}
	if clock == nil {
		clock = time.Now
	}
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		started := clock()
		observed := &observedResponseWriter{ResponseWriter: response}
		next.ServeHTTP(observed, request)

		outcome := consoleOutcomeSuccess
		switch {
		case observed.status >= http.StatusInternalServerError:
			outcome = consoleOutcomeFailure
		case observed.status >= http.StatusBadRequest:
			outcome = consoleOutcomeRejected
		}
		duration := clock().Sub(started)
		handler := consoleHandlerName(request)
		rendered := handler == "static" &&
			strings.HasPrefix(strings.ToLower(observed.Header().Get("Content-Type")), "text/html")
		recorder.ObserveRequest(trustedTenantID(observed), outcome, handler, duration, rendered)
	})
}

func trustedTenantID(response *observedResponseWriter) string {
	value := strings.TrimSpace(response.Header().Get("X-Astra-Tenant-ID"))
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.String() != value {
		return consoleTenantUnknown
	}
	return value
}

func consoleHandlerName(request *http.Request) string {
	path := request.URL.Path
	switch {
	case path == "/health":
		return "health"
	case path == "/ready":
		return "ready"
	case path == "/auth/login":
		return "auth_login"
	case path == "/auth/callback":
		return "auth_callback"
	case path == "/auth/logout":
		return "auth_logout"
	case path == "/api/session":
		return "session"
	case path == "/api/jobs" || strings.HasPrefix(path, "/api/jobs/"):
		return "jobs"
	case path == "/api/connectors" || strings.HasPrefix(path, "/api/connectors/"):
		return "connectors"
	case path == "/api/connections" || strings.HasPrefix(path, "/api/connections/"):
		return "connections"
	case path == "/api/connection-tests" || strings.HasPrefix(path, "/api/connection-tests/"):
		return "connection_tests"
	case path == "/api/audit-events":
		return "audit_events"
	case strings.HasPrefix(path, "/api/"):
		return "api_unknown"
	default:
		return "static"
	}
}

type observedResponseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *observedResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.status = status
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *observedResponseWriter) Write(payload []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(payload)
}

func (w *observedResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
