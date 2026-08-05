package controller

import (
	"testing"
	"time"

	syncv1 "io.astrasync/control-plane/controller/api/v1"
	"io.astrasync/control-plane/job"
)

func TestReconcileStatusAppliesDesiredStateOnce(t *testing.T) {
	now := time.Date(2026, 8, 5, 5, 0, 0, 0, time.UTC)
	resource := &syncv1.SyncJob{Spec: syncv1.SyncJobSpec{State: job.DesiredRunning}}

	changed, err := reconcileStatus(resource, now)
	if err != nil || !changed {
		t.Fatalf("first reconcile: changed=%v err=%v", changed, err)
	}
	if resource.Status.Desired != job.DesiredRunning || resource.Status.State != job.StateInitializing ||
		resource.Status.Epoch != 1 || resource.Status.StartTime == nil {
		t.Fatalf("unexpected initialized status: %+v", resource.Status)
	}

	changed, err = reconcileStatus(resource, now.Add(time.Minute))
	if err != nil || changed || resource.Status.Epoch != 1 {
		t.Fatalf("repeated reconcile was not idempotent: changed=%v err=%v status=%+v", changed, err, resource.Status)
	}

	resource.Spec.State = job.DesiredStopped
	changed, err = reconcileStatus(resource, now.Add(2*time.Minute))
	if err != nil || !changed || resource.Status.State != job.StateCanceling {
		t.Fatalf("stop reconcile: changed=%v err=%v status=%+v", changed, err, resource.Status)
	}
	changed, err = reconcileStatus(resource, now.Add(3*time.Minute))
	if err != nil || changed {
		t.Fatalf("repeated stop reconcile was not idempotent: changed=%v err=%v", changed, err)
	}
}

func TestReconcileStatusDefaultsToStoppedAndRejectsUnknownState(t *testing.T) {
	resource := &syncv1.SyncJob{}
	changed, err := reconcileStatus(resource, time.Now())
	if err != nil || !changed || resource.Status.State != job.StateCreated ||
		resource.Status.Desired != job.DesiredStopped {
		t.Fatalf("default reconcile: changed=%v err=%v status=%+v", changed, err, resource.Status)
	}
	resource.Spec.State = "PAUSED"
	if _, err := reconcileStatus(resource, time.Now()); err == nil {
		t.Fatal("expected unsupported desired state failure")
	}
}
