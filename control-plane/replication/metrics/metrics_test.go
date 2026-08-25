package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNewBundleCreatesSharedRegistryAndRecorder(t *testing.T) {
	bundle, err := NewBundle()
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}
	bundle.Recorder.ObserveEvent("eu-west-1", "checkpoint", "success", time.Millisecond)

	families, err := bundle.Registry.Gather()
	if err != nil {
		t.Fatalf("gather bundle metrics: %v", err)
	}
	found := false
	for _, family := range families {
		if family.GetName() == "astrasync_multi_region_event_total" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected event metric in bundle registry")
	}
}

func TestHandlerForScrapesTheRecorderRegistry(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := NewRecorder(registry)
	if err != nil {
		t.Fatalf("create recorder: %v", err)
	}
	recorder.ObservePromotion("eu-west-1", "success", 25*time.Millisecond)

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()
	HandlerFor(registry).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), "astrasync_multi_region_promotion_total") {
		t.Fatal("expected multi-region promotion metric in scrape output")
	}
}

func TestRecorderNormalizesEmptyLabels(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := NewRecorder(registry)
	if err != nil {
		t.Fatalf("create recorder: %v", err)
	}

	recorder.ObservePromotion(" ", "", -1)
	recorder.ObserveEvent("", " ", "", -1)
	recorder.ObserveRecovery("\t", "\n", -1)

	if got := testutil.ToFloat64(recorder.PromotionsTotal.WithLabelValues("_unknown", "_unknown")); got != 1 {
		t.Fatalf("normalized promotion count = %v, want 1", got)
	}
	if got := testutil.ToFloat64(recorder.EventsTotal.WithLabelValues("_unknown", "_unknown", "_unknown")); got != 1 {
		t.Fatalf("normalized event count = %v, want 1", got)
	}
	if got := testutil.ToFloat64(recorder.RecoveriesTotal.WithLabelValues("_unknown", "_unknown")); got != 1 {
		t.Fatalf("normalized recovery count = %v, want 1", got)
	}
}

func TestRecorderObservesPromotionOutcomeAndDuration(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := NewRecorder(registry)
	if err != nil {
		t.Fatalf("create recorder: %v", err)
	}

	recorder.ObservePromotion("eu-west-1", "success", 25*time.Millisecond)
	recorder.ObservePromotion("eu-west-1", "failure", 40*time.Millisecond)

	if got := testutil.ToFloat64(recorder.PromotionsTotal.WithLabelValues("eu-west-1", "success")); got != 1 {
		t.Fatalf("expected one successful promotion, got %v", got)
	}
	if got := testutil.ToFloat64(recorder.PromotionsTotal.WithLabelValues("eu-west-1", "failure")); got != 1 {
		t.Fatalf("expected one failed promotion, got %v", got)
	}
	recorder.ObserveEvent("eu-west-1", "checkpoint", "success", 15*time.Millisecond)
	recorder.ObserveEvent("eu-west-1", "checkpoint", "failure", -1)
	recorder.ObserveRecovery("eu-west-1", "success", 50*time.Millisecond)

	if got := testutil.ToFloat64(recorder.EventsTotal.WithLabelValues("eu-west-1", "checkpoint", "success")); got != 1 {
		t.Fatalf("expected one successful event, got %v", got)
	}
	if got := testutil.ToFloat64(recorder.EventsTotal.WithLabelValues("eu-west-1", "checkpoint", "failure")); got != 1 {
		t.Fatalf("expected one failed event, got %v", got)
	}
	if got := testutil.ToFloat64(recorder.RecoveriesTotal.WithLabelValues("eu-west-1", "success")); got != 1 {
		t.Fatalf("expected one successful recovery, got %v", got)
	}
	if _, err := registry.Gather(); err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
}
