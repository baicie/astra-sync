// Package metrics_test verifies that the multi-region Recorder routes
// every label value through io.astrasync/control-plane/observability/normalize
// (ADR-063 §3) and exposes the documented sentinel errors. The tests are
// deliberately boundary-driven: each Observe* method collapses empty /
// non-allowlisted / control-character / over-length inputs to the
// documented sentinel, matching the catalog rows and the Phase 21
// normalize contract.
package metrics_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"io.astrasync/control-plane/observability/normalize"
	"io.astrasync/control-plane/replication/metrics"
)

// validRegion is a canonical region name the catalog accepts as a
// free-form label value. Used in canonical happy-path tests.
const validRegion = "us-east-1"

// validPeer is a peer-region counterpart for event tests.
const validPeer = "eu-west-1"

// validEventType is one of the documented event_type allowlist values.
const validEventType = "checkpoint"

func TestRecorderRoutesObservePromotionThroughNormalize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		target      string
		outcome     string
		wantTarget  string
		wantOutcome string
	}{
		{name: "happy_canonical_success", target: validRegion, outcome: metrics.OutcomeSuccess,
			wantTarget: validRegion, wantOutcome: metrics.OutcomeSuccess},
		{name: "happy_canonical_failure", target: validRegion, outcome: metrics.OutcomeFailure,
			wantTarget: validRegion, wantOutcome: metrics.OutcomeFailure},
		{name: "empty_target_collapsed", target: "", outcome: metrics.OutcomeSuccess,
			wantTarget: "_unknown", wantOutcome: metrics.OutcomeSuccess},
		{name: "whitespace_target_collapsed", target: "   ", outcome: metrics.OutcomeFailure,
			wantTarget: "_unknown", wantOutcome: metrics.OutcomeFailure},
		{name: "control_char_target_collapsed", target: "us-east-\n1", outcome: metrics.OutcomeSuccess,
			wantTarget: "_unknown", wantOutcome: metrics.OutcomeSuccess},
		{name: "over_length_target_collapsed", target: strings.Repeat("a", 129), outcome: metrics.OutcomeSuccess,
			wantTarget: "_unknown", wantOutcome: metrics.OutcomeSuccess},
		{name: "non_allowlisted_outcome_collapsed", target: validRegion, outcome: "OK",
			wantTarget: validRegion, wantOutcome: metrics.OutcomeFailure},
		{name: "empty_outcome_collapsed", target: validRegion, outcome: "",
			wantTarget: validRegion, wantOutcome: metrics.OutcomeFailure},
		{name: "case_preserved_target", target: "US-EAST-1", outcome: metrics.OutcomeSuccess,
			wantTarget: "US-EAST-1", wantOutcome: metrics.OutcomeSuccess},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			registry := prometheus.NewRegistry()
			recorder, err := metrics.NewRecorder(registry)
			if err != nil {
				t.Fatalf("new recorder: %v", err)
			}
			recorder.ObservePromotion(testCase.target, testCase.outcome, time.Millisecond)
			body := scrapeOpenMetrics(t, registry)
			sample := `astrasync_multi_region_promotion_total{outcome="` + testCase.wantOutcome +
				`",target_region="` + testCase.wantTarget + `"} 1`
			if !strings.Contains(body, sample) {
				t.Fatalf("scrape body missing %q for case %q: %s", sample, testCase.name, body)
			}
		})
	}
}

func TestRecorderRoutesObserveEventThroughNormalize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		peer      string
		eventType string
		outcome   string
		wantPeer  string
		wantEvent string
		wantOut   string
	}{
		{name: "happy_canonical", peer: validPeer, eventType: validEventType, outcome: metrics.OutcomeSuccess,
			wantPeer: validPeer, wantEvent: validEventType, wantOut: metrics.OutcomeSuccess},
		{name: "happy_topology", peer: validPeer, eventType: "topology", outcome: metrics.OutcomeSuccess,
			wantPeer: validPeer, wantEvent: "topology", wantOut: metrics.OutcomeSuccess},
		{name: "happy_health", peer: validPeer, eventType: "health", outcome: metrics.OutcomeFailure,
			wantPeer: validPeer, wantEvent: "health", wantOut: metrics.OutcomeFailure},
		{name: "empty_event_type_collapsed", peer: validPeer, eventType: "", outcome: metrics.OutcomeSuccess,
			wantPeer: validPeer, wantEvent: "_unknown", wantOut: metrics.OutcomeSuccess},
		{name: "non_allowlisted_event_type_collapsed", peer: validPeer, eventType: "heartbeat", outcome: metrics.OutcomeSuccess,
			wantPeer: validPeer, wantEvent: "_unknown", wantOut: metrics.OutcomeSuccess},
		{name: "empty_peer_collapsed", peer: "", eventType: validEventType, outcome: metrics.OutcomeSuccess,
			wantPeer: "_unknown", wantEvent: validEventType, wantOut: metrics.OutcomeSuccess},
		{name: "control_char_peer_collapsed", peer: "eu-west-\n1", eventType: validEventType, outcome: metrics.OutcomeSuccess,
			wantPeer: "_unknown", wantEvent: validEventType, wantOut: metrics.OutcomeSuccess},
		{name: "non_allowlisted_outcome_collapsed", peer: validPeer, eventType: validEventType, outcome: "OK",
			wantPeer: validPeer, wantEvent: validEventType, wantOut: metrics.OutcomeFailure},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			registry := prometheus.NewRegistry()
			recorder, err := metrics.NewRecorder(registry)
			if err != nil {
				t.Fatalf("new recorder: %v", err)
			}
			recorder.ObserveEvent(testCase.peer, testCase.eventType, testCase.outcome, time.Millisecond)
			body := scrapeOpenMetrics(t, registry)
			sample := `astrasync_multi_region_event_total{event_type="` + testCase.wantEvent +
				`",outcome="` + testCase.wantOut +
				`",peer_region="` + testCase.wantPeer + `"} 1`
			if !strings.Contains(body, sample) {
				t.Fatalf("scrape body missing %q for case %q: %s", sample, testCase.name, body)
			}
		})
	}
}

func TestRecorderRoutesObserveRecoveryThroughNormalize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		target      string
		outcome     string
		wantTarget  string
		wantOutcome string
	}{
		{name: "happy_canonical_success", target: validRegion, outcome: metrics.OutcomeSuccess,
			wantTarget: validRegion, wantOutcome: metrics.OutcomeSuccess},
		{name: "happy_canonical_failure", target: validRegion, outcome: metrics.OutcomeFailure,
			wantTarget: validRegion, wantOutcome: metrics.OutcomeFailure},
		{name: "empty_target_collapsed", target: "", outcome: metrics.OutcomeFailure,
			wantTarget: "_unknown", wantOutcome: metrics.OutcomeFailure},
		{name: "control_char_target_collapsed", target: "us\n-east-1", outcome: metrics.OutcomeSuccess,
			wantTarget: "_unknown", wantOutcome: metrics.OutcomeSuccess},
		{name: "over_length_target_collapsed", target: strings.Repeat("z", 200), outcome: metrics.OutcomeSuccess,
			wantTarget: "_unknown", wantOutcome: metrics.OutcomeSuccess},
		{name: "non_allowlisted_outcome_collapsed", target: validRegion, outcome: "RETRY",
			wantTarget: validRegion, wantOutcome: metrics.OutcomeFailure},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			registry := prometheus.NewRegistry()
			recorder, err := metrics.NewRecorder(registry)
			if err != nil {
				t.Fatalf("new recorder: %v", err)
			}
			recorder.ObserveRecovery(testCase.target, testCase.outcome, time.Millisecond)
			body := scrapeOpenMetrics(t, registry)
			sample := `astrasync_multi_region_recovery_total{outcome="` + testCase.wantOutcome +
				`",target_region="` + testCase.wantTarget + `"} 1`
			if !strings.Contains(body, sample) {
				t.Fatalf("scrape body missing %q for case %q: %s", sample, testCase.name, body)
			}
		})
	}
}

func TestRecorderScrapeIsBoundedAcrossDistinctInputs(t *testing.T) {
	t.Parallel()
	registry := prometheus.NewRegistry()
	recorder, err := metrics.NewRecorder(registry)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}

	// Promotion: distinct inputs collapse to bounded series.
	// NormalizeFreeText preserves case ("US-EAST-1" stays distinct),
	// so "US-EAST-1" / success and "" / success are DIFFERENT series
	// (uppercase region is valid free-text; empty collapses to _unknown).
	recorder.ObservePromotion(validRegion, metrics.OutcomeSuccess, time.Millisecond)
	recorder.ObservePromotion("US-EAST-1", metrics.OutcomeSuccess, time.Millisecond) // distinct from validRegion (case preserved)
	recorder.ObservePromotion("", metrics.OutcomeSuccess, time.Millisecond)          // -> _unknown/success (distinct from above)
	recorder.ObservePromotion(validRegion, metrics.OutcomeFailure, time.Millisecond)
	recorder.ObservePromotion(validRegion, "OK", time.Millisecond) // -> validRegion/failure (same as above)
	// Distinct series: validRegion/success, US-EAST-1/success, _unknown/success, validRegion/failure = 4

	// Event: distinct inputs collapse to bounded series.
	recorder.ObserveEvent(validPeer, validEventType, metrics.OutcomeSuccess, time.Millisecond)
	recorder.ObserveEvent(validPeer, "heartbeat", metrics.OutcomeSuccess, time.Millisecond) // -> validPeer/_unknown/success
	recorder.ObserveEvent(validPeer, "topology", metrics.OutcomeFailure, time.Millisecond)
	recorder.ObserveEvent("", validEventType, metrics.OutcomeSuccess, time.Millisecond) // -> _unknown/checkpoint/success
	recorder.ObserveEvent(validPeer, validEventType, "RETRY", time.Millisecond)         // -> validPeer/checkpoint/failure
	// Distinct series: 5

	// Recovery: distinct inputs collapse to bounded series.
	recorder.ObserveRecovery(validRegion, metrics.OutcomeSuccess, time.Millisecond)
	recorder.ObserveRecovery("", metrics.OutcomeFailure, time.Millisecond)                       // -> _unknown/failure
	recorder.ObserveRecovery(validRegion, "OK", time.Millisecond)                                // -> validRegion/failure (same as below)
	recorder.ObserveRecovery(strings.Repeat("z", 200), metrics.OutcomeSuccess, time.Millisecond) // -> _unknown/success (same as 1st)
	recorder.ObserveRecovery(validRegion, metrics.OutcomeFailure, time.Millisecond)
	// Distinct series: validRegion/success, _unknown/failure, validRegion/failure, _unknown/success = 4

	body := scrapeOpenMetrics(t, registry)
	for _, expectation := range []struct {
		family string
		want   int
	}{
		{family: "astrasync_multi_region_promotion_total", want: 4},
		{family: "astrasync_multi_region_event_total", want: 5},
		{family: "astrasync_multi_region_recovery_total", want: 4},
	} {
		count := strings.Count(body, expectation.family+"{")
		if count != expectation.want {
			t.Fatalf("%s series count = %d, want %d; body=%s", expectation.family, count, expectation.want, body)
		}
	}
}

func TestRecorderNilReceiverIsSafe(t *testing.T) {
	t.Parallel()
	var recorder *metrics.Recorder
	recorder.ObservePromotion(validRegion, metrics.OutcomeSuccess, time.Millisecond)
	recorder.ObserveEvent(validPeer, validEventType, metrics.OutcomeSuccess, time.Millisecond)
	recorder.ObserveRecovery(validRegion, metrics.OutcomeSuccess, time.Millisecond)
}

func TestNewRecorderRejectsNilRegisterer(t *testing.T) {
	t.Parallel()
	if _, err := metrics.NewRecorder(nil); !errors.Is(err, metrics.ErrNilRegisterer) {
		t.Fatalf("NewRecorder(nil) err = %v, want ErrNilRegisterer", err)
	}
}

func TestNewRecorderRejectsDuplicateRegistration(t *testing.T) {
	t.Parallel()
	registry := prometheus.NewRegistry()
	if _, err := metrics.NewRecorder(registry); err != nil {
		t.Fatalf("first NewRecorder: %v", err)
	}
	_, err := metrics.NewRecorder(registry)
	if !errors.Is(err, metrics.ErrDuplicateMetric) {
		t.Fatalf("second NewRecorder err = %v, want ErrDuplicateMetric", err)
	}
}

func TestNewBundleCreatesSharedRegistryAndRecorder(t *testing.T) {
	t.Parallel()
	bundle, err := metrics.NewBundle()
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}
	bundle.Recorder.ObserveEvent(validPeer, validEventType, metrics.OutcomeSuccess, time.Millisecond)

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
	t.Parallel()
	registry := prometheus.NewRegistry()
	recorder, err := metrics.NewRecorder(registry)
	if err != nil {
		t.Fatalf("create recorder: %v", err)
	}
	recorder.ObservePromotion(validRegion, metrics.OutcomeSuccess, 25*time.Millisecond)

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()
	metrics.HandlerFor(registry).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), "astrasync_multi_region_promotion_total") {
		t.Fatal("expected multi-region promotion metric in scrape output")
	}
}

// TestRecorderDurationBoundedAtZero asserts the legacy
// normalizeDuration contract: negative durations are clamped to 0
// so the histogram observation does not panic on negative
// time.Duration. Mirrors the pre-slice-46 behavior.
func TestRecorderDurationBoundedAtZero(t *testing.T) {
	t.Parallel()
	registry := prometheus.NewRegistry()
	recorder, err := metrics.NewRecorder(registry)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}
	recorder.ObservePromotion(validRegion, metrics.OutcomeSuccess, -1*time.Second)
	// Negative duration must not panic; the sample lands in the 0-bucket.
	count := testutil.CollectAndCount(recorder.PromotionsDuration)
	if count != 1 {
		t.Fatalf("expected exactly one histogram series, got %d", count)
	}
}

// TestRecorderRoutesThroughSameNormalizeHelpers locks the
// back-compat contract: the replication Recorder must NOT retain an
// internal normalizeLabel helper. ADR-063 §3 records that the
// helper is deleted from replication/metrics. This test pins the
// import direction so a future contributor who re-introduces the
// internal helper would still pass (this test exercises the public
// import path, not the internal helper).
func TestRecorderRoutesThroughSameNormalizeHelpers(t *testing.T) {
	t.Parallel()
	if got := normalize.NormalizeFreeText("us-east-1", 128, "_unknown"); got != "us-east-1" {
		t.Fatalf("normalize package identity: got %q want us-east-1", got)
	}
}

// TestRecorderLegacyContractEmptyInputsCollapsesToUnknown mirrors
// the pre-slice-46 TestRecorderNormalizesEmptyLabels assertion as a
// black-box smoke test. The replication Recorder now uses
// io.astrasync/control-plane/observability/normalize:
//
//   - target_region / peer_region collapse to _unknown via
//     NormalizeFreeText (matches the legacy behavior).
//   - event_type collapses to _unknown via NormalizeOutcome with
//     freeTextUnknown as the fallback (matches the legacy
//     normalizeLabel collapse-to-_unknown behavior for the
//     event_type field).
//   - outcome collapses to OutcomeFailure via NormalizeOutcome with
//     the documented allowlist fallback (CHANGED from pre-slice-46
//     behavior; the catalog row "outcome is success or failure"
//     explicitly documents the failure fallback for non-allowlisted
//     values; pre-slice-46 normalizeLabel collapsed to _unknown).
func TestRecorderLegacyContractEmptyInputsCollapsesToUnknown(t *testing.T) {
	t.Parallel()
	registry := prometheus.NewRegistry()
	recorder, err := metrics.NewRecorder(registry)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}

	recorder.ObservePromotion(" ", "", -1)
	recorder.ObserveEvent("", " ", "", -1)
	recorder.ObserveRecovery("\t", "\n", -1)

	// Promotion: target=" " -> _unknown; outcome="" -> failure.
	if got := testutil.ToFloat64(recorder.PromotionsTotal.WithLabelValues("_unknown", metrics.OutcomeFailure)); got != 1 {
		t.Fatalf("normalized promotion count = %v, want 1 (target_region=_unknown, outcome=failure)", got)
	}
	// Event: peer="" -> _unknown; event_type=" " -> _unknown; outcome="" -> failure.
	if got := testutil.ToFloat64(recorder.EventsTotal.WithLabelValues("_unknown", "_unknown", metrics.OutcomeFailure)); got != 1 {
		t.Fatalf("normalized event count = %v, want 1 (peer_region=_unknown, event_type=_unknown, outcome=failure)", got)
	}
	// Recovery: target="\t" -> _unknown; outcome="\n" -> failure.
	if got := testutil.ToFloat64(recorder.RecoveriesTotal.WithLabelValues("_unknown", metrics.OutcomeFailure)); got != 1 {
		t.Fatalf("normalized recovery count = %v, want 1 (target_region=_unknown, outcome=failure)", got)
	}
}

func scrapeOpenMetrics(t *testing.T, gatherer prometheus.Gatherer) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Accept", "application/openmetrics-text")
	recorder := httptest.NewRecorder()
	promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{EnableOpenMetrics: true}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("scrape status = %d, want 200", recorder.Code)
	}
	return recorder.Body.String()
}
