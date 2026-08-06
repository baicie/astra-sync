package controller

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	syncv1 "io.astrasync/control-plane/controller/api/v1"
	"io.astrasync/control-plane/job"
	"io.astrasync/control-plane/job/memory"
	jobpostgres "io.astrasync/control-plane/job/postgres"
)

func TestReconcilerConvergesSyncJobWithPostgres(t *testing.T) {
	dataSourceName := os.Getenv("ASTRASYNC_TEST_POSTGRES_URL")
	if dataSourceName == "" {
		t.Skip("ASTRASYNC_TEST_POSTGRES_URL is not configured")
	}
	ctx := context.Background()
	database, err := sql.Open("pgx", dataSourceName)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	defer database.Close()
	repository := jobpostgres.New(database)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatalf("migrate jobs: %v", err)
	}
	namespace := "controller-it-" + uuid.NewString()[:8]
	defer func() {
		if _, cleanupErr := database.ExecContext(
			context.Background(), `DELETE FROM astrasync_control_jobs WHERE namespace = $1`, namespace,
		); cleanupErr != nil {
			t.Errorf("clean integration Job: %v", cleanupErr)
		}
	}()
	resource := testSyncJob(uuid.NewString(), job.DesiredRunning)
	resource.Namespace = namespace
	scheme := runtime.NewScheme()
	if err := syncv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	kubernetesClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&syncv1.SyncJob{}).
		WithObjects(resource).Build()
	now := time.Date(2026, 8, 5, 4, 0, 0, 0, time.UTC)
	reconciler := &SyncJobReconciler{
		Client: kubernetesClient, Scheme: scheme, Jobs: repository,
		Clock: func() time.Time { return now }, StatusRefreshInterval: time.Second,
	}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: namespace, Name: resource.Name}}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("add finalizer: %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("converge PostgreSQL: %v", err)
	}
	stored, err := repository.Get(ctx, job.Key{Namespace: namespace, Name: resource.Name})
	if err != nil {
		t.Fatalf("get durable Job: %v", err)
	}
	if stored.Status.State != job.StateInitializing || stored.Status.Epoch != 1 {
		t.Fatalf("unexpected durable lifecycle: %+v", stored.Status)
	}
	running, _, err := stored.Advance(stored.Status.Epoch, job.StateRunning, nil, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("simulate Scheduler transition: %v", err)
	}
	if _, err := repository.Update(ctx, running, stored.Version); err != nil {
		t.Fatalf("persist Scheduler transition: %v", err)
	}
	replacement := &SyncJobReconciler{
		Client: kubernetesClient, Scheme: scheme, Jobs: repository,
		Clock: func() time.Time { return now.Add(2 * time.Minute) }, StatusRefreshInterval: time.Second,
	}
	if _, err := replacement.Reconcile(ctx, request); err != nil {
		t.Fatalf("replacement Controller project PostgreSQL status: %v", err)
	}
	projected := &syncv1.SyncJob{}
	if err := kubernetesClient.Get(ctx, request.NamespacedName, projected); err != nil {
		t.Fatalf("get projected resource: %v", err)
	}
	if projected.Status.State != job.StateRunning || projected.Status.Epoch != 1 {
		t.Fatalf("PostgreSQL status was not projected: %+v", projected.Status)
	}
}

func TestReconcileConvergesPostgresAndProjectsSchedulerStatus(t *testing.T) {
	now := time.Date(2026, 8, 5, 5, 0, 0, 0, time.UTC)
	resource := testSyncJob(uuid.NewString(), job.DesiredRunning)
	scheme := runtime.NewScheme()
	if err := syncv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&syncv1.SyncJob{}).
		WithObjects(resource).Build()
	repository := memory.New()
	reconciler := &SyncJobReconciler{
		Client: client, Scheme: scheme, Jobs: repository, Clock: func() time.Time { return now },
		StatusRefreshInterval: time.Minute,
	}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "orders"}}
	if result, err := reconciler.Reconcile(context.Background(), request); err != nil || !result.Requeue {
		t.Fatalf("add finalizer: result=%+v err=%v", result, err)
	}
	result, err := reconciler.Reconcile(context.Background(), request)
	if err != nil || result.RequeueAfter != time.Minute {
		t.Fatalf("converge desired state: result=%+v err=%v", result, err)
	}
	stored, err := repository.Get(context.Background(), job.Key{Namespace: "default", Name: "orders"})
	if err != nil {
		t.Fatalf("get converged job: %v", err)
	}
	if stored.Status.State != job.StateInitializing || stored.Status.Epoch != 1 ||
		stored.Status.Desired != job.DesiredRunning {
		t.Fatalf("unexpected PostgreSQL state: %+v", stored.Status)
	}
	projected := &syncv1.SyncJob{}
	if err := client.Get(context.Background(), request.NamespacedName, projected); err != nil {
		t.Fatalf("get projected SyncJob: %v", err)
	}
	if projected.Status.State != job.StateInitializing || projected.Status.Epoch != 1 ||
		len(projected.Finalizers) != 1 || projected.Finalizers[0] != controlPlaneFinalizer {
		t.Fatalf("unexpected projected resource: status=%+v finalizers=%v", projected.Status, projected.Finalizers)
	}

	running, _, err := stored.Advance(stored.Status.Epoch, job.StateRunning, nil, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("advance running: %v", err)
	}
	if _, err := repository.Update(context.Background(), running, stored.Version); err != nil {
		t.Fatalf("persist running: %v", err)
	}
	replacement := &SyncJobReconciler{
		Client: client, Scheme: scheme, Jobs: repository, Clock: func() time.Time { return now.Add(2 * time.Minute) },
		StatusRefreshInterval: time.Minute,
	}
	if _, err := replacement.Reconcile(context.Background(), request); err != nil {
		t.Fatalf("replacement Controller refresh status: %v", err)
	}
	if err := client.Get(context.Background(), request.NamespacedName, projected); err != nil {
		t.Fatalf("get refreshed SyncJob: %v", err)
	}
	if projected.Status.State != job.StateRunning {
		t.Fatalf("scheduler state was not projected: %+v", projected.Status)
	}
}

func TestReconcileSchedulesPostgresRefreshForInactiveJob(t *testing.T) {
	resource := testSyncJob(uuid.NewString(), job.DesiredStopped)
	scheme := runtime.NewScheme()
	if err := syncv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	kubernetesClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&syncv1.SyncJob{}).
		WithObjects(resource).Build()
	reconciler := &SyncJobReconciler{
		Client: kubernetesClient, Scheme: scheme, Jobs: memory.New(),
		Clock: time.Now, StatusRefreshInterval: 30 * time.Second,
	}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "orders"}}
	if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatalf("add finalizer: %v", err)
	}
	result, err := reconciler.Reconcile(context.Background(), request)
	if err != nil || result.RequeueAfter != 30*time.Second {
		t.Fatalf("inactive PostgreSQL refresh was not scheduled: result=%+v err=%v", result, err)
	}
}

func TestStatusFromJobProjectsCompleteDurableStatus(t *testing.T) {
	started := time.Date(2026, 8, 5, 5, 0, 0, 0, time.UTC)
	ended := started.Add(5 * time.Minute)
	checkpointTime := started.Add(4 * time.Minute)
	failureTime := ended.Add(-time.Second)
	projected := statusFromJob(job.Status{
		Desired: job.DesiredRunning, State: job.StateFailed, Epoch: 3, RestartCount: 2,
		StartTime: &started, EndTime: &ended,
		LastCheckpoint: &job.Checkpoint{
			ID: 17, Timestamp: checkpointTime, StateSize: 4096, DurationMS: 250,
		},
		Failure: &job.Failure{
			Reason: "HeartbeatTimeout", RootCause: "Coordinator heartbeat stopped",
			Timestamp: failureTime, Host: "scheduler-1",
		},
	})

	if projected.Desired != job.DesiredRunning || projected.State != job.StateFailed ||
		projected.Epoch != 3 || projected.RestartCount != 2 || projected.StartTime == nil ||
		projected.EndTime == nil || !projected.StartTime.Time.Equal(started) ||
		!projected.EndTime.Time.Equal(ended) || projected.LastCheckpoint == nil ||
		projected.LastCheckpoint.ID != 17 || !projected.LastCheckpoint.Timestamp.Time.Equal(checkpointTime) ||
		projected.LastCheckpoint.StateSize != 4096 || projected.LastCheckpoint.DurationMS != 250 ||
		projected.Failure == nil || projected.Failure.Reason != "HeartbeatTimeout" ||
		projected.Failure.RootCause != "Coordinator heartbeat stopped" ||
		!projected.Failure.Timestamp.Time.Equal(failureTime) || projected.Failure.Host != "scheduler-1" {
		t.Fatalf("durable status was not completely projected: %+v", projected)
	}
}

func TestReconcileDeletionStopsBeforeRemovingFinalizerAndRow(t *testing.T) {
	now := time.Date(2026, 8, 5, 6, 0, 0, 0, time.UTC)
	resource := testSyncJob(uuid.NewString(), job.DesiredStopped)
	resource.Finalizers = []string{controlPlaneFinalizer}
	deletion := metav1.NewTime(now)
	resource.DeletionTimestamp = &deletion
	scheme := runtime.NewScheme()
	if err := syncv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&syncv1.SyncJob{}).
		WithObjects(resource).Build()
	repository := memory.New()
	created, err := job.New(job.Key{Namespace: "default", Name: "orders"}, uuid.NewString(), resourceSpecForTest(), now)
	if err != nil {
		t.Fatalf("new job: %v", err)
	}
	started, _, err := created.RequestStart(now)
	if err != nil {
		t.Fatalf("start job: %v", err)
	}
	if _, err := repository.Create(context.Background(), started); err != nil {
		t.Fatalf("create job: %v", err)
	}
	reconciler := &SyncJobReconciler{Client: client, Scheme: scheme, Jobs: repository, Clock: func() time.Time { return now }}
	if result, err := reconciler.reconcileDeletion(context.Background(), resource, started.Key); err != nil || result.RequeueAfter <= 0 {
		t.Fatalf("request deletion stop: result=%+v err=%v", result, err)
	}
	stopping, err := repository.Get(context.Background(), started.Key)
	if err != nil || stopping.Status.State != job.StateCanceling {
		t.Fatalf("expected canceling row: job=%+v err=%v", stopping.Status, err)
	}
	canceled, _, err := stopping.Advance(stopping.Status.Epoch, job.StateCanceled, nil, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("advance canceled: %v", err)
	}
	if _, err := repository.Update(context.Background(), canceled, stopping.Version); err != nil {
		t.Fatalf("persist canceled: %v", err)
	}
	projected := &syncv1.SyncJob{}
	if err := client.Get(context.Background(), clientKey(resource), projected); err != nil {
		t.Fatalf("get deleting resource: %v", err)
	}
	if _, err := reconciler.reconcileDeletion(context.Background(), projected, canceled.Key); err != nil {
		t.Fatalf("finish deletion: %v", err)
	}
	if _, err := repository.Get(context.Background(), canceled.Key); !errors.Is(err, job.ErrNotFound) {
		t.Fatalf("durable row was not deleted: %v", err)
	}
	if err := client.Get(context.Background(), clientKey(resource), projected); err == nil &&
		controllerutil.ContainsFinalizer(projected, controlPlaneFinalizer) {
		t.Fatal("control-plane finalizer was not removed")
	}
}

func TestConvergeRetriesOptimisticConflict(t *testing.T) {
	now := time.Date(2026, 8, 5, 7, 0, 0, 0, time.UTC)
	resource := testSyncJob(uuid.NewString(), job.DesiredRunning)
	repository := &conflictOnceRepository{Repository: memory.New()}
	reconciler := &SyncJobReconciler{Jobs: repository, Clock: func() time.Time { return now }}
	spec, desired, err := resourceSpec(resource)
	if err != nil {
		t.Fatalf("resource spec: %v", err)
	}
	stored, err := reconciler.converge(
		context.Background(), resource, job.Key{Namespace: "default", Name: "orders"}, spec, desired,
	)
	if err != nil {
		t.Fatalf("converge after conflict: %v", err)
	}
	if repository.conflicts != 1 || stored.Status.State != job.StateInitializing || stored.Version != 2 {
		t.Fatalf("conflict was not retried safely: conflicts=%d job=%+v", repository.conflicts, stored)
	}
}

func TestConvergeStopsActiveJobBeforeImportingChangedSpecAndRestarts(t *testing.T) {
	now := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	repository := memory.New()
	resource := testSyncJob(uuid.NewString(), job.DesiredRunning)
	created, err := job.New(
		job.Key{Namespace: "default", Name: "orders"}, uuid.NewString(), resourceSpecForTest(), now,
	)
	if err != nil {
		t.Fatalf("new job: %v", err)
	}
	started, _, err := created.RequestStart(now)
	if err != nil {
		t.Fatalf("start job: %v", err)
	}
	if _, err := repository.Create(context.Background(), started); err != nil {
		t.Fatalf("create active job: %v", err)
	}
	resource.Spec.Runtime.MaxBatchRecords = 256
	spec, desired, err := resourceSpec(resource)
	if err != nil {
		t.Fatalf("changed resource spec: %v", err)
	}
	reconciler := &SyncJobReconciler{Jobs: repository, Clock: func() time.Time { return now.Add(time.Minute) }}
	stopping, err := reconciler.converge(context.Background(), resource, created.Key, spec, desired)
	if err != nil {
		t.Fatalf("converge stop before spec: %v", err)
	}
	if stopping.Status.State != job.StateCanceling || stopping.Spec.Runtime.MaxBatchRecords != 128 {
		t.Fatalf("active spec changed before stop completed: %+v", stopping)
	}
	canceled, _, err := stopping.Advance(stopping.Status.Epoch, job.StateCanceled, nil, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("finish old execution: %v", err)
	}
	if _, err := repository.Update(context.Background(), canceled, stopping.Version); err != nil {
		t.Fatalf("persist old execution cancellation: %v", err)
	}
	restarted, err := reconciler.converge(context.Background(), resource, created.Key, spec, desired)
	if err != nil {
		t.Fatalf("import spec and restart: %v", err)
	}
	if restarted.Spec.Runtime.MaxBatchRecords != 256 || restarted.Status.State != job.StateInitializing ||
		restarted.Status.Desired != job.DesiredRunning || restarted.Status.Epoch != 2 {
		t.Fatalf("changed spec was not restarted as a new epoch: %+v", restarted)
	}
}

func testSyncJob(uid string, desired job.DesiredState) *syncv1.SyncJob {
	return &syncv1.SyncJob{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "orders", UID: types.UID(uid)},
		Spec: syncv1.SyncJobSpec{
			Source:   job.ConnectorSpec{Connector: "jdbc", Options: map[string]string{"url": "jdbc:source"}},
			Sink:     job.ConnectorSpec{Connector: "jdbc", Options: map[string]string{"url": "jdbc:sink"}},
			Delivery: job.DeliverySpec{Guarantee: job.DeliveryAtLeastOnce},
			Runtime:  job.RuntimeSpec{MaxBatchRecords: 128}, State: desired,
		},
	}
}

func resourceSpecForTest() job.Spec {
	return job.Spec{
		Source:   job.ConnectorSpec{Connector: "jdbc", Options: map[string]string{"url": "jdbc:source"}},
		Sink:     job.ConnectorSpec{Connector: "jdbc", Options: map[string]string{"url": "jdbc:sink"}},
		Delivery: job.DeliverySpec{Guarantee: job.DeliveryAtLeastOnce}, Runtime: job.RuntimeSpec{MaxBatchRecords: 128},
	}
}

func clientKey(resource *syncv1.SyncJob) client.ObjectKey {
	return client.ObjectKey{Namespace: resource.Namespace, Name: resource.Name}
}

type conflictOnceRepository struct {
	job.Repository
	conflicts int
}

func (r *conflictOnceRepository) Update(
	ctx context.Context, candidate job.Job, expectedVersion int64,
) (job.Job, error) {
	if r.conflicts == 0 {
		r.conflicts++
		return job.Job{}, job.ErrConflict
	}
	return r.Repository.Update(ctx, candidate, expectedVersion)
}
