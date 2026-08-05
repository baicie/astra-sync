package controller

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	syncv1 "io.astrasync/control-plane/controller/api/v1"
	"io.astrasync/control-plane/job"
)

type SyncJobReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Clock  func() time.Time
}

func (r *SyncJobReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	resource := &syncv1.SyncJob{}
	if err := r.Get(ctx, request.NamespacedName, resource); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	changed, err := reconcileStatus(resource, r.now())
	if err != nil {
		return ctrl.Result{}, err
	}
	if !changed {
		return ctrl.Result{}, nil
	}
	if err := r.Status().Update(ctx, resource); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func reconcileStatus(resource *syncv1.SyncJob, now time.Time) (bool, error) {
	desired := resource.Spec.State
	if desired == "" {
		desired = job.DesiredStopped
	}
	if desired != job.DesiredStopped && desired != job.DesiredRunning {
		return false, fmt.Errorf("unsupported desired state %q", desired)
	}
	changed := false
	if resource.Status.State == "" {
		resource.Status.State = job.StateCreated
		changed = true
	}
	if resource.Status.Desired != desired {
		resource.Status.Desired = desired
		changed = true
	}

	switch {
	case desired == job.DesiredRunning && restartable(resource.Status.State):
		if resource.Status.Epoch > 0 {
			resource.Status.RestartCount++
		}
		resource.Status.Epoch++
		resource.Status.State = job.StateInitializing
		started := metav1Time(now)
		resource.Status.StartTime = &started
		resource.Status.EndTime = nil
		resource.Status.Failure = nil
		changed = true
	case desired == job.DesiredStopped &&
		(resource.Status.State == job.StateInitializing || resource.Status.State == job.StateRunning):
		resource.Status.State = job.StateCanceling
		changed = true
	}
	return changed, nil
}

func restartable(state job.State) bool {
	return state == job.StateCreated || state == job.StateCanceled || state == job.StateFinished || state == job.StateFailed
}

func metav1Time(value time.Time) metav1.Time {
	return metav1.NewTime(value.UTC())
}

func (r *SyncJobReconciler) now() time.Time {
	if r.Clock == nil {
		return time.Now()
	}
	return r.Clock()
}

func (r *SyncJobReconciler) SetupWithManager(manager ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(manager).For(&syncv1.SyncJob{}).Complete(r)
}
