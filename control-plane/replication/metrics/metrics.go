// Package metrics exposes bounded Prometheus metrics for multi-region operations.
package metrics

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Recorder records multi-region promotion metrics.
type Recorder struct {
	PromotionsTotal    *prometheus.CounterVec
	PromotionsDuration *prometheus.HistogramVec
	EventsTotal        *prometheus.CounterVec
	EventsDuration     *prometheus.HistogramVec
	RecoveriesTotal    *prometheus.CounterVec
	RecoveriesDuration *prometheus.HistogramVec
}

// Bundle owns the registry and recorder shared by replication components.
type Bundle struct {
	Registry *prometheus.Registry
	Recorder *Recorder
}

// NewBundle creates an isolated registry and recorder for one process.
func NewBundle() (*Bundle, error) {
	registry := prometheus.NewRegistry()
	recorder, err := NewRecorder(registry)
	if err != nil {
		return nil, err
	}
	return &Bundle{Registry: registry, Recorder: recorder}, nil
}

// NewRecorder registers an isolated multi-region metrics recorder.
func NewRecorder(registerer prometheus.Registerer) (*Recorder, error) {
	if registerer == nil {
		return nil, fmt.Errorf("metrics registerer must not be nil")
	}
	recorder := &Recorder{
		PromotionsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "astrasync_multi_region_promotion_total",
			Help: "Total multi-region promotion attempts by target region and outcome.",
		}, []string{"target_region", "outcome"}),
		PromotionsDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "astrasync_multi_region_promotion_duration_seconds",
			Help:    "Duration of multi-region promotion attempts.",
			Buckets: prometheus.DefBuckets,
		}, []string{"target_region"}),
		EventsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "astrasync_multi_region_event_total",
			Help: "Total cross-region events by peer region, type, and outcome.",
		}, []string{"peer_region", "event_type", "outcome"}),
		EventsDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "astrasync_multi_region_event_duration_seconds",
			Help:    "Duration of cross-region event delivery attempts.",
			Buckets: prometheus.DefBuckets,
		}, []string{"peer_region", "event_type"}),
		RecoveriesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "astrasync_multi_region_recovery_total",
			Help: "Total cross-region checkpoint recovery attempts by target region and outcome.",
		}, []string{"target_region", "outcome"}),
		RecoveriesDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "astrasync_multi_region_recovery_duration_seconds",
			Help:    "Duration of cross-region checkpoint recovery attempts.",
			Buckets: prometheus.DefBuckets,
		}, []string{"target_region"}),
	}
	for _, collector := range []prometheus.Collector{
		recorder.PromotionsTotal, recorder.PromotionsDuration,
		recorder.EventsTotal, recorder.EventsDuration,
		recorder.RecoveriesTotal, recorder.RecoveriesDuration,
	} {
		if err := registerer.Register(collector); err != nil {
			return nil, fmt.Errorf("register multi-region metric: %w", err)
		}
	}
	return recorder, nil
}

// ObservePromotion records one completed promotion attempt.
func (r *Recorder) ObservePromotion(targetRegion, outcome string, duration time.Duration) {
	if r == nil {
		return
	}
	targetRegion = normalizeLabel(targetRegion)
	outcome = normalizeLabel(outcome)
	r.PromotionsTotal.WithLabelValues(targetRegion, outcome).Inc()
	r.PromotionsDuration.WithLabelValues(targetRegion).Observe(normalizeDuration(duration).Seconds())
}

// ObserveEvent records one cross-region event delivery attempt.
func (r *Recorder) ObserveEvent(peerRegion, eventType, outcome string, duration time.Duration) {
	if r == nil {
		return
	}
	peerRegion = normalizeLabel(peerRegion)
	eventType = normalizeLabel(eventType)
	outcome = normalizeLabel(outcome)
	r.EventsTotal.WithLabelValues(peerRegion, eventType, outcome).Inc()
	r.EventsDuration.WithLabelValues(peerRegion, eventType).Observe(normalizeDuration(duration).Seconds())
}

// ObserveRecovery records one checkpoint recovery attempt.
func (r *Recorder) ObserveRecovery(targetRegion, outcome string, duration time.Duration) {
	if r == nil {
		return
	}
	targetRegion = normalizeLabel(targetRegion)
	outcome = normalizeLabel(outcome)
	r.RecoveriesTotal.WithLabelValues(targetRegion, outcome).Inc()
	r.RecoveriesDuration.WithLabelValues(targetRegion).Observe(normalizeDuration(duration).Seconds())
}

func normalizeLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "_unknown"
	}
	return value
}

func normalizeDuration(duration time.Duration) time.Duration {
	if duration < 0 {
		return 0
	}
	return duration
}

// Handler returns an OpenMetrics-capable handler for the global registry.
func Handler() http.Handler {
	return HandlerFor(prometheus.DefaultGatherer)
}

// HandlerFor returns an OpenMetrics-capable handler for the supplied gatherer.
func HandlerFor(gatherer prometheus.Gatherer) http.Handler {
	if gatherer == nil {
		return http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			http.Error(response, "metrics gatherer must not be nil", http.StatusInternalServerError)
		})
	}
	return promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{EnableOpenMetrics: true})
}
