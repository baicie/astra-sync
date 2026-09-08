// Package metrics registers Prometheus descriptors for Console business
// metrics and provides its /metrics handler. Business call sites create the
// samples. The names and labels follow the convention documented at
// docs/observability/metrics-catalog.md.
package metrics

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"io.astrasync/console/internal/syncjobcr"
)

const (
	unknownTenant  = "_unknown"
	unknownHandler = "unknown"
)

// ConsoleRequestTotal counts request outcomes served by the Console.
var ConsoleRequestTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "console_request_total",
	Help: "Total requests served by the Console.",
}, []string{"tenant_id", "outcome", "handler"})

// ConsoleRenderDuration records the latency of Console rendering.
var ConsoleRenderDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "console_render_duration_seconds",
	Help:    "Latency of Console HTML rendering.",
	Buckets: prometheus.DefBuckets,
}, []string{"handler"})

// DualWriteTotal counts Console -> SyncJob CR dual-write outcomes. ADR-073 §11.
// The mutation label has 3 values (create/update/delete); the outcome label
// has 5 values (success/admission_rejected/timeout/invalid/disabled). Total
// cardinality: 3 * 5 = 15.
var DualWriteTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "controller_syncjob_console_dual_write_total",
	Help: "Console -> SyncJob CR dual-write outcomes (ADR-073 Slice 28-B).",
}, []string{"mutation", "outcome"})

// Recorder updates the Console request metric families owned by HTTP business
// call sites.
type Recorder struct {
	requestTotal   *prometheus.CounterVec
	renderDuration *prometheus.HistogramVec
	dualWrite      *prometheus.CounterVec
}

// NewRecorder registers an isolated Console recorder. Production uses
// DefaultRecorder; this constructor keeps embedded runtimes and tests from
// sharing the process-global metric families.
func NewRecorder(registerer prometheus.Registerer) (*Recorder, error) {
	if registerer == nil {
		return nil, fmt.Errorf("metrics registerer must not be nil")
	}
	recorder := &Recorder{
		requestTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "console_request_total",
			Help: "Total requests served by the Console.",
		}, []string{"tenant_id", "outcome", "handler"}),
		renderDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "console_render_duration_seconds",
			Help:    "Latency of Console HTML rendering.",
			Buckets: prometheus.DefBuckets,
		}, []string{"handler"}),
		dualWrite: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "controller_syncjob_console_dual_write_total",
			Help: "Console -> SyncJob CR dual-write outcomes (ADR-073 Slice 28-B).",
		}, []string{"mutation", "outcome"}),
	}
	for _, collector := range []prometheus.Collector{recorder.requestTotal, recorder.renderDuration, recorder.dualWrite} {
		if err := registerer.Register(collector); err != nil {
			return nil, fmt.Errorf("register Console metric: %w", err)
		}
	}
	return recorder, nil
}

// DefaultRecorder returns a recorder backed by the process-global metric
// families exposed by Handler.
func DefaultRecorder() *Recorder {
	return &Recorder{requestTotal: ConsoleRequestTotal, renderDuration: ConsoleRenderDuration,
		dualWrite: DualWriteTotal}
}

// RecordDualWrite increments the dual-write counter. It is safe to call on a
// nil receiver; the call is a no-op. The mutation and outcome values are
// normalized through whitelist tables so cardinality remains bounded.
func (r *Recorder) RecordDualWrite(mutation, outcome string) {
	if r == nil || r.dualWrite == nil {
		return
	}
	r.dualWrite.WithLabelValues(normalizeMutation(mutation), normalizeDualOutcome(outcome)).Inc()
}

// syncjobcrRecorder adapts the Recorder to the syncjobcr.Recorder interface
// (which uses typed MutationKind / Outcome). This keeps the observability
// package free of the syncjobcr import to avoid an import cycle (server
// already imports both).
type syncjobcrRecorder struct{ inner *Recorder }

func (a syncjobcrRecorder) RecordDualWrite(mutation syncjobcr.MutationKind, outcome syncjobcr.Outcome) {
	a.inner.RecordDualWrite(string(mutation), string(outcome))
}

// SyncjobcrRecorder returns a syncjobcr.Recorder backed by this metrics
// Recorder. Use this in main.go when wiring the dual-writer.
func (r *Recorder) SyncjobcrRecorder() syncjobcrRecorder {
	return syncjobcrRecorder{inner: r}
}

func normalizeMutation(value string) string {
	switch value {
	case "create", "update", "delete":
		return value
	default:
		return "unknown"
	}
}

func normalizeDualOutcome(value string) string {
	switch value {
	case "success", "admission_rejected", "timeout", "invalid", "disabled":
		return value
	default:
		return "invalid"
	}
}

// ObserveRequest records one completed Console request. Tenant and label
// values are normalized here so every call site remains bounded.
func (r *Recorder) ObserveRequest(tenantID, outcome, handler string, duration time.Duration, rendered bool) {
	if r == nil {
		return
	}
	if duration < 0 {
		duration = 0
	}
	r.requestTotal.WithLabelValues(normalizeTenant(tenantID), normalizeOutcome(outcome), normalizeHandler(handler)).Inc()
	if rendered {
		r.renderDuration.WithLabelValues(normalizeHandler(handler)).Observe(duration.Seconds())
	}
}

func normalizeTenant(value string) string {
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.String() != value {
		return unknownTenant
	}
	return value
}

func normalizeOutcome(value string) string {
	switch value {
	case "success", "rejected", "failure":
		return value
	default:
		return "failure"
	}
}

func normalizeHandler(value string) string {
	switch value {
	case "health", "ready", "auth_login", "auth_callback", "auth_logout", "session",
		"jobs", "connectors", "connections", "connection_tests", "audit_events", "static", "api_unknown":
		return value
	default:
		return unknownHandler
	}
}

// Handler returns the Prometheus HTTP handler that scrapes the global
// default registerer.
func Handler() http.Handler {
	return HandlerFor(prometheus.DefaultGatherer)
}

// HandlerFor returns the Prometheus HTTP handler for the supplied gatherer.
func HandlerFor(gatherer prometheus.Gatherer) http.Handler {
	return promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{})
}
