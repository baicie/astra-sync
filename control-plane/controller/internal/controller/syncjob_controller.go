package controller

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	syncv1 "io.astrasync/control-plane/controller/api/v1"
)

type SyncJobReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *SyncJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	fmt.Printf("Reconciling SyncJob: %s\n", req.NamespacedName)

	// Fetch the SyncJob instance
	job := &syncv1.SyncJob{}
	if err := r.Get(ctx, req.NamespacedName, job); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Reconcile logic
	if job.Spec.State == "RUNNING" {
		return r.reconcileRunning(ctx, job)
	}

	return ctrl.Result{}, nil
}

func (r *SyncJobReconciler) reconcileRunning(ctx context.Context, job *syncv1.SyncJob) (ctrl.Result, error) {
	// Check if job is scheduled
	if job.Status.Phase == "" {
		return r.scheduleJob(ctx, job)
	}

	// Check for failures
	if job.Status.Failed {
		return r.handleFailure(ctx, job)
	}

	// Ensure coordinator is running
	return r.ensureCoordinator(ctx, job)
}

func (r *SyncJobReconciler) scheduleJob(ctx context.Context, job *syncv1.SyncJob) (ctrl.Result, error) {
	job.Status.Phase = "Pending"
	if err := r.Status().Update(ctx, job); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{Requeue: true}, nil
}

func (r *SyncJobReconciler) handleFailure(ctx context.Context, job *syncv1.SyncJob) (ctrl.Result, error) {
	// Implement failure handling
	return ctrl.Result{}, nil
}

func (r *SyncJobReconciler) ensureCoordinator(ctx context.Context, job *syncv1.SyncJob) (ctrl.Result, error) {
	// Implement coordinator management
	return ctrl.Result{}, nil
}

func (r *SyncJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&syncv1.SyncJob{}).
		Complete(r)
}
