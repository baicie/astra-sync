package metrics

import (
	"math"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestRecorderNormalizesBoundedLabelsAndDuration(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := NewRecorder(registry)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}

	recorder.ObserveReconcile("caller-controlled", "unexpected", -time.Second)
	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	if len(metricFamilies) != 1 || metricFamilies[0].GetName() != "controller_job_controller_reconcile_duration_seconds" {
		t.Fatalf("unexpected metric families: %+v", metricFamilies)
	}
	metric := metricFamilies[0].GetMetric()
	if len(metric) != 1 {
		t.Fatalf("unexpected normalized labels: %+v", metric)
	}
	labels := make(map[string]string, len(metric[0].GetLabel()))
	for _, label := range metric[0].GetLabel() {
		labels[label.GetName()] = label.GetValue()
	}
	if labels["tenant_id"] != unknownTenant || labels["outcome"] != "failure" {
		t.Fatalf("unexpected normalized labels: %+v", labels)
	}
	if metric[0].GetHistogram().GetSampleSum() != 0 {
		t.Fatalf("negative duration was not clamped: %+v", metric[0].GetHistogram())
	}
}

func TestRecorderRecordsSuccessfulReconcile(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := NewRecorder(registry)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}
	recorder.ObserveReconcile("_unknown", "success", 25*time.Millisecond)
	metricFamilies, err := registry.Gather()
	if err != nil || len(metricFamilies) != 1 || math.Abs(metricFamilies[0].GetMetric()[0].GetHistogram().GetSampleSum()-0.025) > 1e-9 {
		t.Fatalf("gather registered metric: families=%d err=%v", len(metricFamilies), err)
	}
}
