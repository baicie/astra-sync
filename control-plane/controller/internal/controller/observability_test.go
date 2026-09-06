package controller

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	syncv1 "io.astrasync/control-plane/controller/api/v1"
	"io.astrasync/control-plane/job"
	"io.astrasync/control-plane/job/memory"
)

func TestReconcileObservesSuccessAndFailureOutcomes(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := syncv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	resource := testSyncJob("11111111-1111-4111-8111-111111111111", job.DesiredStopped)
	client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&syncv1.SyncJob{}).
		WithObjects(resource).Build()
	recorder := &recordingReconcileMetrics{}
	reconciler := &SyncJobReconciler{
		Client: client, Scheme: scheme, Jobs: memory.New(), Clock: func() time.Time {
			return time.Date(2026, 8, 5, 7, 0, 0, 0, time.UTC)
		},
		Metrics: recorder,
	}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "orders"}}
	if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatalf("add finalizer: %v", err)
	}
	if len(recorder.observations) != 1 || recorder.observations[0].tenantID != "_unknown" ||
		recorder.observations[0].outcome != "success" || recorder.observations[0].duration < 0 {
		t.Fatalf("unexpected success observation: %+v", recorder.observations)
	}

	broken := &SyncJobReconciler{Client: client, Scheme: scheme, Clock: reconciler.Clock, Metrics: recorder}
	if _, err := broken.Reconcile(context.Background(), request); err == nil {
		t.Fatal("expected missing repository error")
	}
	if len(recorder.observations) != 2 || recorder.observations[1].tenantID != "_unknown" ||
		recorder.observations[1].outcome != "failure" || recorder.observations[1].duration < 0 {
		t.Fatalf("unexpected failure observation: %+v", recorder.observations)
	}
}

type recordingReconcileMetrics struct {
	observations []reconcileObservation
}

type reconcileObservation struct {
	tenantID string
	outcome  string
	duration time.Duration
}

func (r *recordingReconcileMetrics) ObserveReconcile(tenantID, outcome string, duration time.Duration) {
	r.observations = append(r.observations, reconcileObservation{
		tenantID: tenantID, outcome: outcome, duration: duration,
	})
}
