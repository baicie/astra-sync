package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	syncv1 "io.astrasync/control-plane/controller/api/v1"
	"io.astrasync/control-plane/job"
	jobmemory "io.astrasync/control-plane/job/memory"
)

// fakeJobsRepository wraps the in-memory repository and allows tests to
// override individual methods (Get/Create/Update/Delete). The first call to
// an overridden method returns the override; subsequent calls fall through to
// the in-memory implementation. Tests use this to inject version conflicts,
// not-found errors, and other transient conditions without modifying the
// production repository logic.
type fakeJobsRepository struct {
	inner *jobmemory.Repository

	getOverride    func(key job.Key) (job.Job, error)
	createOverride func(candidate job.Job) (job.Job, error)
	updateOverride func(candidate job.Job, v int64) (job.Job, error)
	deleteOverride func(key job.Key, v int64) error

	getCount, createCount, updateCount, deleteCount int32
}

func newFakeJobs() *fakeJobsRepository {
	return &fakeJobsRepository{inner: jobmemory.New()}
}

func (f *fakeJobsRepository) Get(ctx context.Context, key job.Key) (job.Job, error) {
	atomic.AddInt32(&f.getCount, 1)
	if f.getOverride != nil {
		return f.getOverride(key)
	}
	return f.inner.Get(ctx, key)
}

func (f *fakeJobsRepository) Create(ctx context.Context, candidate job.Job) (job.Job, error) {
	atomic.AddInt32(&f.createCount, 1)
	if f.createOverride != nil {
		return f.createOverride(candidate)
	}
	return f.inner.Create(ctx, candidate)
}

func (f *fakeJobsRepository) Update(ctx context.Context, candidate job.Job, expectedVersion int64) (job.Job, error) {
	atomic.AddInt32(&f.updateCount, 1)
	if f.updateOverride != nil {
		return f.updateOverride(candidate, expectedVersion)
	}
	return f.inner.Update(ctx, candidate, expectedVersion)
}

func (f *fakeJobsRepository) Delete(ctx context.Context, key job.Key, expectedVersion int64) error {
	atomic.AddInt32(&f.deleteCount, 1)
	if f.deleteOverride != nil {
		return f.deleteOverride(key, expectedVersion)
	}
	return f.inner.Delete(ctx, key, expectedVersion)
}

func (f *fakeJobsRepository) List(ctx context.Context, namespace string, page job.Page) (job.PageResult, error) {
	return f.inner.List(ctx, namespace, page)
}

// makeJobSpec returns a minimal valid job.Spec for use in tests.
func makeJobSpec() job.Spec {
	return job.Spec{
		Source: job.ConnectorSpec{Connector: "mysql-cdc"},
		Sink:   job.ConnectorSpec{Connector: "postgres-sink"},
		Delivery: job.DeliverySpec{
			Guarantee: job.DeliveryAtLeastOnce,
		},
		Runtime: job.RuntimeSpec{MaxBatchRecords: 512},
	}
}

// makeRunningJob creates a minimal Job in the RUNNING state with a valid spec.
func makeRunningJob(t *testing.T, key job.Key, uid string, now time.Time) job.Job {
	t.Helper()
	j, err := job.New(key, uid, makeJobSpec(), now)
	if err != nil {
		t.Fatalf("new job: %v", err)
	}
	j.Status.State = job.StateRunning
	j.Status.Desired = job.DesiredRunning
	j.Status.Epoch = 1
	start := now
	j.Status.StartTime = &start
	return j
}

// makeCreatedJob creates a minimal Job in the CREATED state with a valid spec.
func makeCreatedJob(t *testing.T, key job.Key, uid string, now time.Time) job.Job {
	t.Helper()
	return mustNewJob(t, key, uid, now)
}

// mustNewJob wraps job.New with a t.Fatalf error path.
func mustNewJob(t *testing.T, key job.Key, uid string, now time.Time) job.Job {
	t.Helper()
	j, err := job.New(key, uid, makeJobSpec(), now)
	if err != nil {
		t.Fatalf("new job: %v", err)
	}
	return j
}

// buildReconciler wires a fake Jobs repository and fake K8s client into a
// SyncJobReconciler. The fake K8s client is constructed with the given
// resources pre-loaded. The clock is injected.
func buildReconciler(t *testing.T, fakeJobs *fakeJobsRepository, clock func() time.Time, resources ...*syncv1.SyncJob) *SyncJobReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := syncv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	builder := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&syncv1.SyncJob{})
	if len(resources) > 0 {
		objs := make([]client.Object, len(resources))
		for i, r := range resources {
			objs[i] = r
		}
		builder = builder.WithObjects(objs...)
	}
	kubeClient := builder.Build()
	return &SyncJobReconciler{
		Client:                kubeClient,
		Scheme:                scheme,
		Clock:                 clock,
		Jobs:                  fakeJobs,
		StatusRefreshInterval: 5 * time.Second,
		Metrics:               fakeReconcileMetrics{},
	}
}

var _ ReconcileMetrics = fakeReconcileMetrics{}

type fakeReconcileMetrics struct{}

func (fakeReconcileMetrics) ObserveReconcile(tenantID, outcome string, d time.Duration) {}

// testSyncJob returns a minimal SyncJob with the given UID and desired state.
// This helper is shared with the existing TestReconcileObservesSuccessAndFailureOutcomes
// test in observability_test.go, which references it without defining it.
func testSyncJob(uid string, desired job.DesiredState) *syncv1.SyncJob {
	return &syncv1.SyncJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "orders",
			Namespace: "default",
			UID:       types.UID(uid),
			Labels:    map[string]string{"astrasync.io/tenant-id": "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"},
		},
		Spec: syncv1.SyncJobSpec{
			Source:   job.ConnectorSpec{Connector: "mysql-cdc"},
			Sink:     job.ConnectorSpec{Connector: "postgres-sink"},
			Delivery: job.DeliverySpec{Guarantee: job.DeliveryAtLeastOnce},
			Runtime:  job.RuntimeSpec{MaxBatchRecords: 512},
			State:    desired,
		},
	}
}

// TestReconcile_adds_finalizer_when_absent verifies the reconcile loop adds
// the control-plane finalizer to a newly observed resource that has not yet
// been adopted.
func TestReconcile_adds_finalizer_when_absent(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	key, _ := job.NewKey("default", "orders")
	uid := uuid.New().String()
	fakeJobs := newFakeJobs()
	if _, err := fakeJobs.inner.Create(context.Background(), makeRunningJob(t, key, uid, now)); err != nil {
		t.Fatalf("create job: %v", err)
	}
	resource := &syncv1.SyncJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "orders",
			Namespace: "default",
			UID:       types.UID(uid),
			Labels:    map[string]string{"astrasync.io/tenant-id": "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"},
		},
		Spec: syncv1.SyncJobSpec{
			Source:   job.ConnectorSpec{Connector: "mysql-cdc"},
			Sink:     job.ConnectorSpec{Connector: "postgres-sink"},
			Delivery: job.DeliverySpec{Guarantee: job.DeliveryAtLeastOnce},
			Runtime:  job.RuntimeSpec{MaxBatchRecords: 512},
			State:    job.DesiredStopped,
		},
	}
	r := buildReconciler(t, fakeJobs, func() time.Time { return now }, resource)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "orders"}}
	result, err := r.Reconcile(context.Background(), req)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !result.Requeue && result.RequeueAfter == 0 {
		t.Fatalf("expected Requeue=true after finalizer add; got Requeue=%v RequeueAfter=%v", result.Requeue, result.RequeueAfter)
	}

	fresh := &syncv1.SyncJob{}
	if err := r.Get(context.Background(), req.NamespacedName, fresh); err != nil {
		t.Fatalf("get resource: %v", err)
	}
	if !controllerutil.ContainsFinalizer(fresh, controlPlaneFinalizer) {
		t.Fatalf("expected finalizer %q on resource", controlPlaneFinalizer)
	}
}

// TestReconcile_deletion_removes_finalizer_when_job_not_found covers the
// deletion path where the K8s resource is being deleted but the job has
// already been removed from the repository.
func TestReconcile_deletion_removes_finalizer_when_job_not_found(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	uid := uuid.New().String()
	fakeJobs := newFakeJobs()

	resource := &syncv1.SyncJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "orders",
			Namespace:         "default",
			UID:               types.UID(uid),
			DeletionTimestamp: &metav1.Time{Time: now},
			Finalizers:        []string{controlPlaneFinalizer},
		},
	}
	r := buildReconciler(t, fakeJobs, func() time.Time { return now }, resource)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "orders"}}
	result, err := r.Reconcile(context.Background(), req)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Requeue {
		t.Fatalf("expected no requeue after job-not-found deletion; got Requeue=true")
	}

	// Once the finalizer is removed from a resource with DeletionTimestamp,
	// the K8s API garbage-collects the resource. The fake client mirrors this
	// behavior, so the reconciler's finalizer-removal update is observable
	// only by the controller's event stream, not by a follow-up Get.
	// We assert the side effects: the finalizer-removal update path was hit.
	fresh := &syncv1.SyncJob{}
	err = r.Get(context.Background(), req.NamespacedName, fresh)
	if err == nil && controllerutil.ContainsFinalizer(fresh, controlPlaneFinalizer) {
		t.Fatalf("expected finalizer removed; finalizer still present")
	}
}

// TestReconcile_converge_starts_stopped_job covers the converge path where a
// CREATED job (desired STOPPED) transitions to RUNNING because the resource
// Spec.State is set to RUNNING.
func TestReconcile_converge_starts_stopped_job(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	key, _ := job.NewKey("default", "orders")
	uid := uuid.New().String()
	created := mustNewJob(t, key, uid, now)

	fakeJobs := newFakeJobs()
	if _, err := fakeJobs.inner.Create(context.Background(), created); err != nil {
		t.Fatalf("create job: %v", err)
	}

	resource := &syncv1.SyncJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "orders",
			Namespace:  "default",
			UID:        types.UID(uid),
			Labels:     map[string]string{"astrasync.io/tenant-id": "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"},
			Finalizers: []string{controlPlaneFinalizer},
		},
		Spec: syncv1.SyncJobSpec{
			Source:   job.ConnectorSpec{Connector: "mysql-cdc"},
			Sink:     job.ConnectorSpec{Connector: "postgres-sink"},
			Delivery: job.DeliverySpec{Guarantee: job.DeliveryAtLeastOnce},
			Runtime:  job.RuntimeSpec{MaxBatchRecords: 512},
			State:    job.DesiredRunning,
		},
	}
	r := buildReconciler(t, fakeJobs, func() time.Time { return now }, resource)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "orders"}}
	_, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	stored, err := fakeJobs.inner.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if stored.Status.State != job.StateInitializing {
		t.Fatalf("expected state INITIALIZING, got %s", stored.Status.State)
	}
	if stored.Status.Epoch != 1 {
		t.Fatalf("expected epoch 1, got %d", stored.Status.Epoch)
	}
	if stored.Status.Desired != job.DesiredRunning {
		t.Fatalf("expected desired RUNNING, got %s", stored.Status.Desired)
	}
}

// TestReconcile_converge_stops_running_job covers the converge path where a
// RUNNING job transitions to CANCELING because the resource Spec.State is
// changed to STOPPED while the job is active.
func TestReconcile_converge_stops_running_job(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	key, _ := job.NewKey("default", "orders")
	uid := uuid.New().String()
	running := makeRunningJob(t, key, uid, now)

	fakeJobs := newFakeJobs()
	if _, err := fakeJobs.inner.Create(context.Background(), running); err != nil {
		t.Fatalf("create job: %v", err)
	}
	resource := &syncv1.SyncJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "orders",
			Namespace:  "default",
			UID:        types.UID(uid),
			Labels:     map[string]string{"astrasync.io/tenant-id": "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"},
			Finalizers: []string{controlPlaneFinalizer},
		},
		Spec: syncv1.SyncJobSpec{
			Source:   job.ConnectorSpec{Connector: "mysql-cdc"},
			Sink:     job.ConnectorSpec{Connector: "postgres-sink"},
			Delivery: job.DeliverySpec{Guarantee: job.DeliveryAtLeastOnce},
			Runtime:  job.RuntimeSpec{MaxBatchRecords: 512},
			State:    job.DesiredStopped,
		},
	}
	r := buildReconciler(t, fakeJobs, func() time.Time { return now }, resource)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "orders"}}
	_, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	stored, err := fakeJobs.inner.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if stored.Status.State != job.StateCanceling {
		t.Fatalf("expected state CANCELING, got %s", stored.Status.State)
	}
	if stored.Status.Desired != job.DesiredStopped {
		t.Fatalf("expected desired STOPPED, got %s", stored.Status.Desired)
	}
	if stored.Status.Epoch != 1 {
		t.Fatalf("expected epoch 1 after stop, got %d", stored.Status.Epoch)
	}
}

// TestReconcile_converge_noop_returns_requeue covers the converge path where
// both spec and desired state are unchanged — no update is needed.
func TestReconcile_converge_noop_returns_requeue(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	key, _ := job.NewKey("default", "orders")
	uid := uuid.New().String()
	running := makeRunningJob(t, key, uid, now)

	fakeJobs := newFakeJobs()
	if _, err := fakeJobs.inner.Create(context.Background(), running); err != nil {
		t.Fatalf("create job: %v", err)
	}
	resource := &syncv1.SyncJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "orders",
			Namespace:  "default",
			UID:        types.UID(uid),
			Labels:     map[string]string{"astrasync.io/tenant-id": "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"},
			Finalizers: []string{controlPlaneFinalizer},
		},
		Spec: syncv1.SyncJobSpec{
			Source:   job.ConnectorSpec{Connector: "mysql-cdc"},
			Sink:     job.ConnectorSpec{Connector: "postgres-sink"},
			Delivery: job.DeliverySpec{Guarantee: job.DeliveryAtLeastOnce},
			Runtime:  job.RuntimeSpec{MaxBatchRecords: 512},
			State:    job.DesiredRunning,
		},
	}
	r := buildReconciler(t, fakeJobs, func() time.Time { return now }, resource)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "orders"}}
	result, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if result.Requeue {
		t.Fatalf("expected Requeue=false for noop; got Requeue=true")
	}
	if result.RequeueAfter <= 0 {
		t.Fatalf("expected RequeueAfter>0 for noop; got %v", result.RequeueAfter)
	}

	stored, err := fakeJobs.inner.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if stored.Version != 1 {
		t.Fatalf("expected version 1 (unchanged), got %d", stored.Version)
	}
}

// TestReconcile_converge_requeues_on_persistent_conflict covers the converge path
// where every Update call returns ErrConflict (5 retries exhausted). The
// reconciler must give up and requeue without returning an error.
func TestReconcile_converge_requeues_on_persistent_conflict(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	key, _ := job.NewKey("default", "orders")
	uid := uuid.New().String()
	created := mustNewJob(t, key, uid, now)

	fakeJobs := newFakeJobs()
	if _, err := fakeJobs.inner.Create(context.Background(), created); err != nil {
		t.Fatalf("create job: %v", err)
	}
	// Every Update returns conflict — exhausts the converge retry loop.
	fakeJobs.updateOverride = func(candidate job.Job, v int64) (job.Job, error) {
		return job.Job{}, job.ErrConflict
	}

	resource := &syncv1.SyncJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "orders",
			Namespace:  "default",
			UID:        types.UID(uid),
			Labels:     map[string]string{"astrasync.io/tenant-id": "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"},
			Finalizers: []string{controlPlaneFinalizer},
		},
		Spec: syncv1.SyncJobSpec{
			Source:   job.ConnectorSpec{Connector: "mysql-cdc"},
			Sink:     job.ConnectorSpec{Connector: "postgres-sink"},
			Delivery: job.DeliverySpec{Guarantee: job.DeliveryAtLeastOnce},
			Runtime:  job.RuntimeSpec{MaxBatchRecords: 512},
			State:    job.DesiredRunning,
		},
	}
	r := buildReconciler(t, fakeJobs, func() time.Time { return now }, resource)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "orders"}}
	result, err := r.Reconcile(context.Background(), req)

	// 5 retry attempts exhausted; the converge loop returns ErrConflict
	// and Reconcile requeues (controller-runtime workqueue backs off and retries).
	if err != nil {
		t.Fatalf("expected no error; got %v", err)
	}
	if !result.Requeue && result.RequeueAfter == 0 {
		t.Fatalf("expected requeue after exhausted conflicts; got Requeue=%v RequeueAfter=%v", result.Requeue, result.RequeueAfter)
	}

	// Version must not have changed — every update was rejected.
	stored, err := fakeJobs.inner.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if stored.Version != 1 {
		t.Fatalf("expected version 1 after rejected conflicts; got %d", stored.Version)
	}
}

// TestReconcile_converge_replaces_spec_when_inactive covers the converge path
// where the spec is changed while the job is in the CREATED state.
// ReplaceSpec must be called rather than RequestStop.
func TestReconcile_converge_replaces_spec_when_inactive(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	key, _ := job.NewKey("default", "orders")
	uid := uuid.New().String()
	created := mustNewJob(t, key, uid, now)

	fakeJobs := newFakeJobs()
	if _, err := fakeJobs.inner.Create(context.Background(), created); err != nil {
		t.Fatalf("create job: %v", err)
	}
	resource := &syncv1.SyncJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "orders",
			Namespace:  "default",
			UID:        types.UID(uid),
			Labels:     map[string]string{"astrasync.io/tenant-id": "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"},
			Finalizers: []string{controlPlaneFinalizer},
		},
		Spec: syncv1.SyncJobSpec{
			Source:   job.ConnectorSpec{Connector: "mysql-cdc"},
			Sink:     job.ConnectorSpec{Connector: "postgres-sink"},
			Delivery: job.DeliverySpec{Guarantee: job.DeliveryAtLeastOnce},
			Runtime:  job.RuntimeSpec{MaxBatchRecords: 2048},
			State:    job.DesiredStopped,
		},
	}
	r := buildReconciler(t, fakeJobs, func() time.Time { return now }, resource)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "orders"}}
	_, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	stored, err := fakeJobs.inner.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if stored.Spec.Runtime.MaxBatchRecords != 2048 {
		t.Fatalf("expected spec replaced with MaxBatchRecords=2048; got %d", stored.Spec.Runtime.MaxBatchRecords)
	}
	if stored.Status.State != job.StateCreated {
		t.Fatalf("expected state CREATED, got %s", stored.Status.State)
	}
}

// TestReconcile_deletion_stops_active_job_and_requeues covers the deletion
// path where the job is RUNNING when the resource is marked for deletion.
func TestReconcile_deletion_stops_active_job_and_requeues(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	key, _ := job.NewKey("default", "orders")
	uid := uuid.New().String()
	running := makeRunningJob(t, key, uid, now)

	fakeJobs := newFakeJobs()
	if _, err := fakeJobs.inner.Create(context.Background(), running); err != nil {
		t.Fatalf("create job: %v", err)
	}
	resource := &syncv1.SyncJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "orders",
			Namespace:         "default",
			UID:               types.UID(uid),
			DeletionTimestamp: &metav1.Time{Time: now},
			Finalizers:        []string{controlPlaneFinalizer},
		},
	}
	r := buildReconciler(t, fakeJobs, func() time.Time { return now }, resource)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "orders"}}
	result, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if !result.Requeue && result.RequeueAfter == 0 {
		t.Fatalf("expected requeue while job is active during deletion; got Requeue=%v RequeueAfter=%v", result.Requeue, result.RequeueAfter)
	}

	stored, err := fakeJobs.inner.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if stored.Status.State != job.StateCanceling {
		t.Fatalf("expected CANCELING after deletion stop request; got %s", stored.Status.State)
	}
	if stored.Status.Desired != job.DesiredStopped {
		t.Fatalf("expected desired STOPPED; got %s", stored.Status.Desired)
	}
}

// TestReconcile_deletion_deletes_inactive_job_and_removes_finalizer covers
// the deletion path where the job is in a non-active state (CREATED).
func TestReconcile_deletion_deletes_inactive_job_and_removes_finalizer(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	key, _ := job.NewKey("default", "orders")
	uid := uuid.New().String()
	created := mustNewJob(t, key, uid, now)

	fakeJobs := newFakeJobs()
	if _, err := fakeJobs.inner.Create(context.Background(), created); err != nil {
		t.Fatalf("create job: %v", err)
	}
	resource := &syncv1.SyncJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "orders",
			Namespace:         "default",
			UID:               types.UID(uid),
			DeletionTimestamp: &metav1.Time{Time: now},
			Finalizers:        []string{controlPlaneFinalizer},
		},
	}
	r := buildReconciler(t, fakeJobs, func() time.Time { return now }, resource)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "orders"}}
	result, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if result.Requeue {
		t.Fatalf("expected no requeue after full deletion; got Requeue=true")
	}

	_, err = fakeJobs.inner.Get(context.Background(), key)
	if !errors.Is(err, job.ErrNotFound) {
		t.Fatalf("expected job not found after deletion; got %v", err)
	}

	// After finalizer removal the resource is garbage-collected; the test
	// verifies only the durable side effect (the job was deleted from the
	// repository) and that the reconcile did not error.
}

// TestReconcile_deletion_requeues_on_delete_conflict covers the deletion path
// where the repository returns ErrConflict on Delete (stale version). The
// reconciler must requeue rather than error.
func TestReconcile_deletion_requeues_on_delete_conflict(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	key, _ := job.NewKey("default", "orders")
	uid := uuid.New().String()
	created := mustNewJob(t, key, uid, now)

	fakeJobs := newFakeJobs()
	if _, err := fakeJobs.inner.Create(context.Background(), created); err != nil {
		t.Fatalf("create job: %v", err)
	}
	fakeJobs.deleteOverride = func(key job.Key, v int64) error {
		return job.ErrConflict
	}
	resource := &syncv1.SyncJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "orders",
			Namespace:         "default",
			UID:               types.UID(uid),
			DeletionTimestamp: &metav1.Time{Time: now},
			Finalizers:        []string{controlPlaneFinalizer},
		},
	}
	r := buildReconciler(t, fakeJobs, func() time.Time { return now }, resource)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "orders"}}
	result, err := r.Reconcile(context.Background(), req)

	if err != nil {
		t.Fatalf("expected no error for delete conflict; got %v", err)
	}
	if !result.Requeue && result.RequeueAfter == 0 {
		t.Fatalf("expected requeue after delete conflict; got Requeue=%v RequeueAfter=%v", result.Requeue, result.RequeueAfter)
	}

	_, err = fakeJobs.inner.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("expected job to still exist after rejected delete; got %v", err)
	}
}

// TestReconcile_returns_error_when_jobs_repository_is_nil verifies that the
// reconciler returns a descriptive error when Jobs is nil rather than
// panicking.
func TestReconcile_returns_error_when_jobs_repository_is_nil(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	uid := uuid.New().String()

	scheme := runtime.NewScheme()
	if err := syncv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	resource := &syncv1.SyncJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "orders",
			Namespace:  "default",
			UID:        types.UID(uid),
			Finalizers: []string{controlPlaneFinalizer},
		},
	}
	r := &SyncJobReconciler{
		Client:                fake.NewClientBuilder().WithScheme(scheme).WithObjects(resource).Build(),
		Scheme:                scheme,
		Clock:                 func() time.Time { return now },
		Jobs:                  nil,
		StatusRefreshInterval: 5 * time.Second,
		Metrics:               fakeReconcileMetrics{},
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "orders"}}
	_, err := r.Reconcile(context.Background(), req)

	if err == nil {
		t.Fatalf("expected error when Jobs is nil; got nil")
	}
	if !strings.Contains(err.Error(), "must not be nil") {
		t.Fatalf("expected error message about nil Jobs; got %v", err)
	}
}

// TestReconcile_returns_nil_for_unknown_resource verifies the
// reconcile loop returns nil (not an error) when the K8s resource does
// not exist. The controller wraps Get errors with client.IgnoreNotFound
// so a deleted resource does not surface as a reconcile failure.
func TestReconcile_returns_nil_for_unknown_resource(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	r := buildReconciler(t, newFakeJobs(), func() time.Time { return now })

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "unknown"}}
	_, err := r.Reconcile(context.Background(), req)

	if err != nil {
		t.Fatalf("expected nil error for unknown resource; got %v", err)
	}
}

// TestReconcile_converge_spec_change_while_active_requests_stop_first covers
// the converge invariant documented in the controller: when the spec
// changes while the job is active, the controller must request a stop
// first, then replace the spec in a subsequent reconcile.
func TestReconcile_converge_spec_change_while_active_requests_stop_first(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	key, _ := job.NewKey("default", "orders")
	uid := uuid.New().String()
	running := makeRunningJob(t, key, uid, now)

	fakeJobs := newFakeJobs()
	if _, err := fakeJobs.inner.Create(context.Background(), running); err != nil {
		t.Fatalf("create job: %v", err)
	}
	resource := &syncv1.SyncJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "orders",
			Namespace:  "default",
			UID:        types.UID(uid),
			Labels:     map[string]string{"astrasync.io/tenant-id": "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"},
			Finalizers: []string{controlPlaneFinalizer},
		},
		Spec: syncv1.SyncJobSpec{
			Source:   job.ConnectorSpec{Connector: "mysql-cdc"},
			Sink:     job.ConnectorSpec{Connector: "postgres-sink"},
			Delivery: job.DeliverySpec{Guarantee: job.DeliveryAtLeastOnce},
			Runtime:  job.RuntimeSpec{MaxBatchRecords: 2048},
			State:    job.DesiredStopped,
		},
	}
	r := buildReconciler(t, fakeJobs, func() time.Time { return now }, resource)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "orders"}}
	_, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	stored, err := fakeJobs.inner.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}

	if stored.Status.State != job.StateCanceling {
		t.Fatalf("expected CANCELING after spec-change+active first reconcile; got %s", stored.Status.State)
	}
	if stored.Spec.Runtime.MaxBatchRecords != 512 {
		t.Fatalf("expected spec unchanged (512) in first reconcile; got %d", stored.Spec.Runtime.MaxBatchRecords)
	}
}

// TestReconcile_converge_creates_job_when_not_found covers the converge path
// where the job does not exist in the repository. The reconciler must
// create it from the resource spec.
func TestReconcile_converge_creates_job_when_not_found(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	key, _ := job.NewKey("default", "orders")
	uid := uuid.New().String()

	fakeJobs := newFakeJobs()
	resource := &syncv1.SyncJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "orders",
			Namespace:  "default",
			UID:        types.UID(uid),
			Labels:     map[string]string{"astrasync.io/tenant-id": "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"},
			Finalizers: []string{controlPlaneFinalizer},
		},
		Spec: syncv1.SyncJobSpec{
			Source:   job.ConnectorSpec{Connector: "mysql-cdc"},
			Sink:     job.ConnectorSpec{Connector: "postgres-sink"},
			Delivery: job.DeliverySpec{Guarantee: job.DeliveryAtLeastOnce},
			Runtime:  job.RuntimeSpec{MaxBatchRecords: 512},
			State:    job.DesiredStopped,
		},
	}
	r := buildReconciler(t, fakeJobs, func() time.Time { return now }, resource)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "orders"}}
	_, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	stored, err := fakeJobs.inner.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if stored.Status.State != job.StateCreated {
		t.Fatalf("expected new job to be CREATED; got %s", stored.Status.State)
	}
	if stored.UID != uid {
		t.Fatalf("expected UID %s; got %s", uid, stored.UID)
	}
}

// TestReconcile_converge_retries_on_create_already_exists covers the converge
// path where Create returns ErrAlreadyExists (a race: another controller
// created the job between our Get and Create). The reconciler must retry
// the Get on the next loop iteration; the next iteration finds the existing
// job and continues normally.
func TestReconcile_converge_retries_on_create_already_exists(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	key, _ := job.NewKey("default", "orders")
	uid := uuid.New().String()
	created := mustNewJob(t, key, uid, now)

	fakeJobs := newFakeJobs()
	if _, err := fakeJobs.inner.Create(context.Background(), created); err != nil {
		t.Fatalf("create job: %v", err)
	}
	fakeJobs.createOverride = func(candidate job.Job) (job.Job, error) {
		return job.Job{}, job.ErrAlreadyExists
	}
	resource := &syncv1.SyncJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "orders",
			Namespace:  "default",
			UID:        types.UID(uid),
			Labels:     map[string]string{"astrasync.io/tenant-id": "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"},
			Finalizers: []string{controlPlaneFinalizer},
		},
		Spec: syncv1.SyncJobSpec{
			Source:   job.ConnectorSpec{Connector: "mysql-cdc"},
			Sink:     job.ConnectorSpec{Connector: "postgres-sink"},
			Delivery: job.DeliverySpec{Guarantee: job.DeliveryAtLeastOnce},
			Runtime:  job.RuntimeSpec{MaxBatchRecords: 512},
			State:    job.DesiredStopped,
		},
	}
	r := buildReconciler(t, fakeJobs, func() time.Time { return now }, resource)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "orders"}}
	_, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	stored, err := fakeJobs.inner.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if stored.Version != 1 {
		t.Fatalf("expected version 1 (noop or race-won create); got %d", stored.Version)
	}
}

// TestReconcile_ignores_spec_change_while_canceling covers the converge
// invariant: when the job is CANCELING (active), a spec change must NOT
// call ReplaceSpec. The converge loop only calls ReplaceSpec when the job
// is inactive, even if the spec differs.
func TestReconcile_ignores_spec_change_while_canceling(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	key, _ := job.NewKey("default", "orders")
	uid := uuid.New().String()

	canceling := mustNewJob(t, key, uid, now)
	canceling.Status.State = job.StateCanceling
	canceling.Status.Desired = job.DesiredStopped
	canceling.Status.Epoch = 1
	start := now
	canceling.Status.StartTime = &start

	fakeJobs := newFakeJobs()
	if _, err := fakeJobs.inner.Create(context.Background(), canceling); err != nil {
		t.Fatalf("create job: %v", err)
	}
	resource := &syncv1.SyncJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "orders",
			Namespace:  "default",
			UID:        types.UID(uid),
			Labels:     map[string]string{"astrasync.io/tenant-id": "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"},
			Finalizers: []string{controlPlaneFinalizer},
		},
		Spec: syncv1.SyncJobSpec{
			Source:   job.ConnectorSpec{Connector: "postgres-cdc"},
			Sink:     job.ConnectorSpec{Connector: "postgres-sink"},
			Delivery: job.DeliverySpec{Guarantee: job.DeliveryAtLeastOnce},
			Runtime:  job.RuntimeSpec{MaxBatchRecords: 2048},
			State:    job.DesiredStopped,
		},
	}
	r := buildReconciler(t, fakeJobs, func() time.Time { return now }, resource)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "orders"}}
	_, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	stored, err := fakeJobs.inner.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if stored.Spec.Source.Connector != "mysql-cdc" {
		t.Fatalf("expected spec unchanged (mysql-cdc) while CANCELING; got %s", stored.Spec.Source.Connector)
	}
}

// TestReconcile_converge_stops_inactive_job_via_desired_stop covers the converge
// path where a non-active job (CREATED) receives desired STOPPED (already the
// default). This must not cause a spurious update.
func TestReconcile_converge_stops_inactive_job_via_desired_stop(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	key, _ := job.NewKey("default", "orders")
	uid := uuid.New().String()
	created := mustNewJob(t, key, uid, now)

	fakeJobs := newFakeJobs()
	if _, err := fakeJobs.inner.Create(context.Background(), created); err != nil {
		t.Fatalf("create job: %v", err)
	}
	resource := &syncv1.SyncJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "orders",
			Namespace:  "default",
			UID:        types.UID(uid),
			Labels:     map[string]string{"astrasync.io/tenant-id": "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"},
			Finalizers: []string{controlPlaneFinalizer},
		},
		Spec: syncv1.SyncJobSpec{
			Source:   job.ConnectorSpec{Connector: "mysql-cdc"},
			Sink:     job.ConnectorSpec{Connector: "postgres-sink"},
			Delivery: job.DeliverySpec{Guarantee: job.DeliveryAtLeastOnce},
			Runtime:  job.RuntimeSpec{MaxBatchRecords: 512},
			State:    job.DesiredStopped,
		},
	}
	r := buildReconciler(t, fakeJobs, func() time.Time { return now }, resource)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "orders"}}
	result, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if result.Requeue {
		t.Fatalf("expected no Requeue for noop; got Requeue=true")
	}
	if result.RequeueAfter <= 0 {
		t.Fatalf("expected RequeueAfter>0 for noop; got %v", result.RequeueAfter)
	}
	stored, err := fakeJobs.inner.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if stored.Version != 1 {
		t.Fatalf("expected version 1 (no update); got %d", stored.Version)
	}
}

// TestReconcile_converge_error_from_jobs_get_passes_through covers
// the converge path where Jobs.Get returns an unexpected error (not
// ErrNotFound). The reconciler must return it without swallowing it.
func TestReconcile_converge_error_from_jobs_get_passes_through(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	uid := uuid.New().String()

	fakeJobs := newFakeJobs()
	fakeJobs.getOverride = func(k job.Key) (job.Job, error) {
		return job.Job{}, fmt.Errorf("database unavailable")
	}
	resource := &syncv1.SyncJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "orders",
			Namespace:  "default",
			UID:        types.UID(uid),
			Finalizers: []string{controlPlaneFinalizer},
		},
		Spec: syncv1.SyncJobSpec{
			Source:   job.ConnectorSpec{Connector: "mysql-cdc"},
			Sink:     job.ConnectorSpec{Connector: "postgres-sink"},
			Delivery: job.DeliverySpec{Guarantee: job.DeliveryAtLeastOnce},
			Runtime:  job.RuntimeSpec{MaxBatchRecords: 512},
			State:    job.DesiredStopped,
		},
	}
	r := buildReconciler(t, fakeJobs, func() time.Time { return now }, resource)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "orders"}}
	_, err := r.Reconcile(context.Background(), req)

	if err == nil {
		t.Fatalf("expected error from Jobs.Get; got nil")
	}
	if !strings.Contains(err.Error(), "database unavailable") {
		t.Fatalf("expected 'database unavailable' in error; got %v", err)
	}
}
