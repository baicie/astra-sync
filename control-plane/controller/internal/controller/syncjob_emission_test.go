package controller

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	syncv1 "io.astrasync/control-plane/controller/api/v1"
	controllerobservability "io.astrasync/control-plane/controller/internal/metrics"
	"io.astrasync/control-plane/job"
)

// TestObserveTransitionEmitsBoundedSeries covers the controller_job_state_total
// emission path called from converge / reconcileDeletion. The test does not
// run a full reconcile loop; it verifies the label-funnel logic that derives
// tenant_id, namespace, from_state, to_state from the SyncJob resource and
// the pre/post job snapshots.
//
// The Recorder is wired through a fresh prometheus.NewRegistry so the
// scrape body is deterministic and the test does not depend on the
// controller-runtime global registry.
func TestObserveTransitionEmitsBoundedSeries(t *testing.T) {
	cases := []struct {
		name       string
		labels     map[string]string
		namespace  string
		fromState  job.State
		toState    job.State
		wantTenant string
		wantNS     string
		wantFrom   string
		wantTo     string
	}{
		{
			name:       "happy_initializing_to_running",
			labels:     map[string]string{"astrasync.io/tenant-id": "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"},
			namespace:  "prod",
			fromState:  job.StateInitializing,
			toState:    job.StateRunning,
			wantTenant: "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011",
			wantNS:     "prod",
			wantFrom:   "INITIALIZING",
			wantTo:     "RUNNING",
		},
		{
			name:       "missing_label_collapses_to_unknown",
			labels:     nil,
			namespace:  "default",
			fromState:  job.StateRunning,
			toState:    job.StateCanceling,
			wantTenant: "_unknown",
			wantNS:     "default",
			wantFrom:   "RUNNING",
			wantTo:     "CANCELING",
		},
		{
			name:       "non_canonical_tenant_collapsed",
			labels:     map[string]string{"astrasync.io/tenant-id": "ALICE@acme.example"},
			namespace:  "default",
			fromState:  job.StateCreated,
			toState:    job.StateInitializing,
			wantTenant: "_unknown",
			wantNS:     "default",
			wantFrom:   "CREATED",
			wantTo:     "INITIALIZING",
		},
		{
			name:       "platform_self_scope",
			labels:     map[string]string{"astrasync.io/tenant-id": "_platform"},
			namespace:  "kube-system",
			fromState:  job.StateRunning,
			toState:    job.StateFinished,
			wantTenant: "_platform",
			wantNS:     "kube-system",
			wantFrom:   "RUNNING",
			wantTo:     "FINISHED",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			registry := prometheus.NewRegistry()
			recorder, err := controllerobservability.NewRecorder(registry)
			if err != nil {
				t.Fatalf("new recorder: %v", err)
			}
			reconciler := &SyncJobReconciler{Recorder: recorder}
			resource := &syncv1.SyncJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "orders",
					Namespace: tc.namespace,
					Labels:    tc.labels,
					UID:       types.UID("0190f7c4-6c8d-7a01-9d2b-1ecabdff0012"),
				},
			}
			now := time.Now().UTC()
			stored := job.Job{
				UpdatedAt: now,
				Status:    job.Status{State: tc.fromState, Epoch: 1},
			}
			next := stored
			next.Status.State = tc.toState
			next.UpdatedAt = now
			reconciler.observeTransition(resource, stored, next)

			body := scrapeOpenMetricsBody(t, registry)
			want := `controller_job_state_total{` +
				`from_state="` + tc.wantFrom + `", ` +
				`namespace="` + tc.wantNS + `", ` +
				`tenant_id="` + tc.wantTenant + `", ` +
				`to_state="` + tc.wantTo + `"} 1.0`
			if !strings.Contains(body, want) {
				t.Fatalf("scrape body missing %q: %s", want, body)
			}
			// Defensive: caller input must never leak into a series
			// beyond the bounded allowlist documented in ADR-066.
			for _, leak := range []string{"ALICE@acme.example", `{not-a-uuid}`} {
				if strings.Contains(body, `="`+leak+`"`) {
					t.Fatalf("non-normalised label value %q leaked into scrape body: %s", leak, body)
				}
			}
		})
	}
}

// TestObserveTransitionDoesNotEmitWhenStateUnchanged verifies the
// durable-commit contract (ADR-058 §2): a no-op update must not produce
// a state transition sample.
func TestObserveTransitionDoesNotEmitWhenStateUnchanged(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := controllerobservability.NewRecorder(registry)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}
	reconciler := &SyncJobReconciler{Recorder: recorder}
	resource := &syncv1.SyncJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "orders",
			Namespace: "prod",
			Labels:    map[string]string{"astrasync.io/tenant-id": "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"},
		},
	}
	snapshot := job.Job{Status: job.Status{State: job.StateRunning, Epoch: 1}}
	reconciler.observeTransition(resource, snapshot, snapshot)
	body := scrapeOpenMetricsBody(t, registry)
	if strings.Contains(body, "controller_job_state_total") {
		t.Fatalf("expected no controller_job_state_total sample when state is unchanged; got: %s", body)
	}
}

// TestObserveTransitionNilResourceIsNoop verifies the helper is nil-safe
// at the resource boundary (the deletion path passes the resource
// through; this guards against a future caller forgetting to check).
func TestObserveTransitionNilResourceIsNoop(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := controllerobservability.NewRecorder(registry)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}
	reconciler := &SyncJobReconciler{Recorder: recorder}
	snapshot := job.Job{Status: job.Status{State: job.StateRunning, Epoch: 1}}
	reconciler.observeTransition(nil, snapshot, snapshot)
	body := scrapeOpenMetricsBody(t, registry)
	if strings.Contains(body, "controller_job_state_total") {
		t.Fatalf("expected no controller_job_state_total sample for nil resource; got: %s", body)
	}
}

func scrapeOpenMetricsBody(t *testing.T, gatherer prometheus.Gatherer) string {
	t.Helper()
	metrics, err := gatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	var out strings.Builder
	for _, mf := range metrics {
		if mf.GetName() != "controller_job_state_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			out.WriteString(mf.GetName())
			out.WriteByte('{')
			for i, label := range m.GetLabel() {
				if i > 0 {
					out.WriteString(", ")
				}
				out.WriteString(label.GetName())
				out.WriteString(`="`)
				out.WriteString(label.GetValue())
				out.WriteByte('"')
			}
			out.WriteString("} 1.0\n")
		}
	}
	return out.String()
}
