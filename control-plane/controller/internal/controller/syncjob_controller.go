package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	syncv1 "io.astrasync/control-plane/controller/api/v1"
	"io.astrasync/control-plane/job"
)

type SyncJobReconciler struct {
	client.Client
	Scheme                *runtime.Scheme
	Clock                 func() time.Time
	Jobs                  job.Repository
	StatusRefreshInterval time.Duration
}

const controlPlaneFinalizer = "sync.astrasync.io/control-plane-finalizer"

func (r *SyncJobReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	resource := &syncv1.SyncJob{}
	if err := r.Get(ctx, request.NamespacedName, resource); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if r.Jobs == nil {
		return ctrl.Result{}, fmt.Errorf("controller Job repository must not be nil")
	}
	if resource.DeletionTimestamp.IsZero() && !controllerutil.ContainsFinalizer(resource, controlPlaneFinalizer) {
		controllerutil.AddFinalizer(resource, controlPlaneFinalizer)
		if err := r.Update(ctx, resource); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	key, err := job.NewKey(resource.Namespace, resource.Name)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !resource.DeletionTimestamp.IsZero() {
		return r.reconcileDeletion(ctx, resource, key)
	}

	spec, desired, err := resourceSpec(resource)
	if err != nil {
		return ctrl.Result{}, err
	}
	stored, err := r.converge(ctx, resource, key, spec, desired)
	if err != nil {
		if errors.Is(err, job.ErrConflict) || errors.Is(err, job.ErrAlreadyExists) {
			return r.requeue(), nil
		}
		return ctrl.Result{}, err
	}
	if err := r.projectStatus(ctx, resource, stored.Status); err != nil {
		return ctrl.Result{}, err
	}
	return r.requeue(), nil
}

func (r *SyncJobReconciler) converge(
	ctx context.Context,
	resource *syncv1.SyncJob,
	key job.Key,
	spec job.Spec,
	desired job.DesiredState,
) (job.Job, error) {
	for attempt := 0; attempt < 5; attempt++ {
		stored, err := r.Jobs.Get(ctx, key)
		if errors.Is(err, job.ErrNotFound) {
			candidate, createErr := job.New(key, stableJobUID(resource), spec, r.now())
			if createErr != nil {
				return job.Job{}, createErr
			}
			created, createErr := r.Jobs.Create(ctx, candidate)
			if errors.Is(createErr, job.ErrAlreadyExists) {
				continue
			}
			if createErr != nil {
				return job.Job{}, createErr
			}
			stored = created
		} else if err != nil {
			return job.Job{}, err
		}

		next := stored
		changed := false
		if !sameSpec(stored.Spec, spec) {
			if activeState(stored.Status.State) {
				next, changed, err = stored.RequestStop(r.now())
				if err != nil {
					return job.Job{}, err
				}
				if !changed {
					return stored, nil
				}
				updated, updateErr := r.Jobs.Update(ctx, next, stored.Version)
				if errors.Is(updateErr, job.ErrConflict) {
					continue
				}
				return updated, updateErr
			}
			var replaceErr error
			next, replaceErr = stored.ReplaceSpec(spec, r.now())
			if replaceErr != nil {
				return job.Job{}, replaceErr
			}
			changed = true
		}
		if next.Status.Desired != desired {
			var transitionErr error
			var transitionChanged bool
			if desired == job.DesiredRunning {
				next, transitionChanged, transitionErr = next.RequestStart(r.now())
			} else {
				next, transitionChanged, transitionErr = next.RequestStop(r.now())
			}
			if transitionErr != nil {
				return job.Job{}, transitionErr
			}
			changed = changed || transitionChanged
		}
		if !changed {
			return stored, nil
		}
		updated, updateErr := r.Jobs.Update(ctx, next, stored.Version)
		if errors.Is(updateErr, job.ErrConflict) {
			continue
		}
		if updateErr != nil {
			return job.Job{}, updateErr
		}
		return updated, nil
	}
	return job.Job{}, job.ErrConflict
}

func (r *SyncJobReconciler) reconcileDeletion(
	ctx context.Context, resource *syncv1.SyncJob, key job.Key,
) (ctrl.Result, error) {
	stored, err := r.Jobs.Get(ctx, key)
	if errors.Is(err, job.ErrNotFound) {
		return r.removeFinalizer(ctx, resource)
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	if activeState(stored.Status.State) {
		next, changed, transitionErr := stored.RequestStop(r.now())
		if transitionErr != nil {
			return ctrl.Result{}, transitionErr
		}
		if changed {
			if _, err := r.Jobs.Update(ctx, next, stored.Version); errors.Is(err, job.ErrConflict) {
				return r.requeue(), nil
			} else if err != nil {
				return ctrl.Result{}, err
			}
		}
		if err := r.projectStatus(ctx, resource, next.Status); err != nil {
			return ctrl.Result{}, err
		}
		return r.requeue(), nil
	}
	if err := stored.Deletable(); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.Jobs.Delete(ctx, key, stored.Version); errors.Is(err, job.ErrConflict) {
		return r.requeue(), nil
	} else if err != nil && !errors.Is(err, job.ErrNotFound) {
		return ctrl.Result{}, err
	}
	return r.removeFinalizer(ctx, resource)
}

func (r *SyncJobReconciler) removeFinalizer(ctx context.Context, resource *syncv1.SyncJob) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(resource, controlPlaneFinalizer) {
		return ctrl.Result{}, nil
	}
	controllerutil.RemoveFinalizer(resource, controlPlaneFinalizer)
	if err := r.Update(ctx, resource); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *SyncJobReconciler) projectStatus(
	ctx context.Context, resource *syncv1.SyncJob, status job.Status,
) error {
	projected := statusFromJob(status)
	if reflect.DeepEqual(resource.Status, projected) {
		return nil
	}
	resource.Status = projected
	return r.Status().Update(ctx, resource)
}

func (r *SyncJobReconciler) requeue() ctrl.Result {
	interval := r.StatusRefreshInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return ctrl.Result{RequeueAfter: interval}
}

func resourceSpec(resource *syncv1.SyncJob) (job.Spec, job.DesiredState, error) {
	desired := resource.Spec.State
	if desired == "" {
		desired = job.DesiredStopped
	}
	if desired != job.DesiredStopped && desired != job.DesiredRunning {
		return job.Spec{}, "", fmt.Errorf("unsupported desired state %q", desired)
	}
	spec := job.Spec{
		Source: resource.Spec.Source, Sink: resource.Spec.Sink, Transforms: resource.Spec.Transforms,
		Delivery: resource.Spec.Delivery, Runtime: resource.Spec.Runtime,
	}
	if err := spec.Validate(); err != nil {
		return job.Spec{}, "", err
	}
	return spec, desired, nil
}

func sameSpec(left, right job.Spec) bool {
	leftDocument, leftErr := json.Marshal(left)
	rightDocument, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytesEqual(leftDocument, rightDocument)
}

func statusFromJob(source job.Status) syncv1.SyncJobStatus {
	result := syncv1.SyncJobStatus{
		Desired: source.Desired, State: source.State, Epoch: source.Epoch, RestartCount: source.RestartCount,
	}
	if source.StartTime != nil {
		value := metav1Time(*source.StartTime)
		result.StartTime = &value
	}
	if source.EndTime != nil {
		value := metav1Time(*source.EndTime)
		result.EndTime = &value
	}
	if source.LastCheckpoint != nil {
		result.LastCheckpoint = &syncv1.CheckpointInfo{
			ID: source.LastCheckpoint.ID, Timestamp: metav1Time(source.LastCheckpoint.Timestamp),
			StateSize: source.LastCheckpoint.StateSize, DurationMS: source.LastCheckpoint.DurationMS,
		}
	}
	if source.Failure != nil {
		result.Failure = &syncv1.FailureInfo{
			Reason: source.Failure.Reason, RootCause: source.Failure.RootCause,
			Timestamp: metav1Time(source.Failure.Timestamp), Host: source.Failure.Host,
		}
	}
	return result
}

func stableJobUID(resource *syncv1.SyncJob) string {
	uid := string(resource.UID)
	if parsed, err := uuid.Parse(uid); err == nil {
		return parsed.String()
	}
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("syncjob:"+resource.Namespace+"/"+resource.Name+":"+uid)).String()
}

func activeState(state job.State) bool {
	return state == job.StateInitializing || state == job.StateRunning || state == job.StateCanceling
}

func bytesEqual(left, right []byte) bool {
	return string(left) == string(right)
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
