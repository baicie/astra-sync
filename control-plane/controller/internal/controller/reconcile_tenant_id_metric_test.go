package controller

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	syncv1 "io.astrasync/control-plane/controller/api/v1"
	"io.astrasync/control-plane/job"
	jobmemory "io.astrasync/control-plane/job/memory"
)

// TestReconcileDerivesTenantFromLabel pins the contract (ADR-058 §3 +
// ADR-082 §Follow-ups): when the SyncJob CR carries the
// `astrasync.io/tenant-id` label, the Controller reconcile defer must
// route the bound tenant through `normalize.NormalizeTenant` rather
// than emitting the bound label `"_unknown"` that locked earlier
// dashboards to a single series.
//
// Prior to Phase 34 the Reconcile defer was a hard-coded
// `ObserveReconcile("_unknown", ...)`, which made
// `controller_job_controller_reconcile_duration_seconds{tenant_id}` a
// single degenerate series and erased the BFF label translation
// (ADR-072 / ADR-074) at the controller boundary. After Phase 34 the
// label routes through the same `NormalizeTenant` allowlist every
// other tenant-deriving Recorder already uses.
func TestReconcileDerivesTenantFromLabel(t *testing.T) {
	t.Parallel()

	const (
		canonicalUUID  = "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"
		platformTenant = "_platform"
	)
	scheme := runtime.NewScheme()
	if err := syncv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	now := time.Date(2026, 9, 8, 18, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	cases := []struct {
		name       string
		labels     map[string]string
		wantTenant string
	}{
		{
			name:       "canonical_uuid_passes_through",
			labels:     map[string]string{"astrasync.io/tenant-id": canonicalUUID},
			wantTenant: canonicalUUID,
		},
		{
			name:       "missing_label_collapses_to_unknown",
			labels:     nil,
			wantTenant: "_unknown",
		},
		{
			name:       "non_canonical_uuid_collapses_to_unknown",
			labels:     map[string]string{"astrasync.io/tenant-id": "ALICE@acme.example"},
			wantTenant: "_unknown",
		},
		{
			name:       "platform_self_scope_passes_through",
			labels:     map[string]string{"astrasync.io/tenant-id": platformTenant},
			wantTenant: platformTenant,
		},
		{
			name:       "empty_string_label_collapses_to_unknown",
			labels:     map[string]string{"astrasync.io/tenant-id": ""},
			wantTenant: "_unknown",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resource := &syncv1.SyncJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "orders",
					Namespace: "default",
					UID:       types.UID("0190f7c4-6c8d-7a01-9d2b-1ecabdff0099"),
					Labels:    tc.labels,
				},
				Spec: syncv1.SyncJobSpec{
					Source:   job.ConnectorSpec{Connector: "mysql-cdc"},
					Sink:     job.ConnectorSpec{Connector: "postgres-sink"},
					Delivery: job.DeliverySpec{Guarantee: job.DeliveryAtLeastOnce},
					Runtime:  job.RuntimeSpec{MaxBatchRecords: 512},
					State:    job.DesiredStopped,
				},
			}
			recorder := &recordingReconcileMetrics{}
			kubeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&syncv1.SyncJob{}).
				WithObjects(resource).
				Build()
			reconciler := &SyncJobReconciler{
				Client:                kubeClient,
				Scheme:                scheme,
				Clock:                 clock,
				Jobs:                  jobmemory.New(),
				StatusRefreshInterval: 5 * time.Second,
				Metrics:               recorder,
			}
			request := ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: "default", Name: "orders"},
			}
			if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
				t.Fatalf("reconcile: %v", err)
			}

			if len(recorder.observations) != 1 {
				t.Fatalf("expected exactly 1 observation, got %d: %+v",
					len(recorder.observations), recorder.observations)
			}
			got := recorder.observations[0]
			if got.tenantID != tc.wantTenant {
				t.Fatalf("tenant-id label: want %q, got %q (outcome=%q duration=%s)",
					tc.wantTenant, got.tenantID, got.outcome, got.duration)
			}
			if got.outcome != "success" {
				t.Fatalf("expected outcome=success, got %q", got.outcome)
			}
			if got.duration < 0 {
				t.Fatalf("duration must be non-negative, got %s", got.duration)
			}
		})
	}
}
