package scheduler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"io.astrasync/control-plane/job"
	"io.astrasync/control-plane/job/memory"
	"io.astrasync/control-plane/scheduler/internal/dispatch"
)

func TestReconcilerCreatesRunningStateAndFencesCompletionToClaimedEpoch(t *testing.T) {
	clock := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	repository := memory.New()
	created := createRunningJob(t, repository, clock)
	store := newFakeStore(dispatch.Record{
		Identity: dispatch.Identity{JobUID: created.UID, Epoch: 1},
		Key:      created.Key,
		OwnerID:  "scheduler-a",
		Phase:    dispatch.PhaseClaimed,
	})
	dispatcher := &fakeDispatcher{observation: Observation{State: ObservationSucceeded}}
	reconciler := newTestReconciler(t, store, repository, dispatcher, clock)

	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	stored, err := repository.Get(context.Background(), created.Key)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if stored.Status.State != job.StateFinished || stored.Status.Desired != job.DesiredStopped {
		t.Fatalf("unexpected terminal status: %+v", stored.Status)
	}
	if store.completed.Phase != dispatch.PhaseSucceeded {
		t.Fatalf("dispatch was not completed successfully: %+v", store.completed)
	}
	if dispatcher.reconcileCalls != 1 {
		t.Fatalf("expected one Coordinator reconciliation, got %d", dispatcher.reconcileCalls)
	}
}

func TestReconcilerWritesCoordinatorFailureAndKeepsFailureDetails(t *testing.T) {
	clock := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	repository := memory.New()
	created := createRunningJob(t, repository, clock)
	store := newFakeStore(dispatch.Record{
		Identity: dispatch.Identity{JobUID: created.UID, Epoch: 1},
		Key:      created.Key,
		OwnerID:  "scheduler-a",
		Phase:    dispatch.PhaseStarting,
	})
	dispatcher := &fakeDispatcher{observation: Observation{
		State: ObservationFailed, Reason: "JobFailed", Message: "image pull failed",
	}}
	reconciler := newTestReconciler(t, store, repository, dispatcher, clock)

	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	stored, err := repository.Get(context.Background(), created.Key)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if stored.Status.State != job.StateFailed || stored.Status.Failure == nil {
		t.Fatalf("expected failed status: %+v", stored.Status)
	}
	if stored.Status.Failure.Reason != "JobFailed" || stored.Status.Failure.RootCause != "image pull failed" {
		t.Fatalf("unexpected failure: %+v", stored.Status.Failure)
	}
	if store.completed.Phase != dispatch.PhaseFailed {
		t.Fatalf("dispatch was not marked failed: %+v", store.completed)
	}
}

func TestReconcilerStopsCoordinatorBeforeMarkingCanceled(t *testing.T) {
	clock := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	repository := memory.New()
	created := createRunningJob(t, repository, clock)
	current, changed, err := created.RequestStop(clock.Add(time.Minute))
	if err != nil || !changed {
		t.Fatalf("request stop: changed=%v err=%v", changed, err)
	}
	if _, err := repository.Update(context.Background(), current, created.Version); err != nil {
		t.Fatalf("persist stop: %v", err)
	}
	store := newFakeStore(dispatch.Record{
		Identity: dispatch.Identity{JobUID: created.UID, Epoch: 1},
		Key:      created.Key,
		OwnerID:  "scheduler-a",
		Phase:    dispatch.PhaseRunning,
	})
	dispatcher := &fakeDispatcher{stopResult: true}
	reconciler := newTestReconciler(t, store, repository, dispatcher, clock)

	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("reconcile stop: %v", err)
	}
	stored, err := repository.Get(context.Background(), created.Key)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if stored.Status.State != job.StateCanceled || dispatcher.stopCalls != 1 {
		t.Fatalf("unexpected cancellation: status=%+v stopCalls=%d", stored.Status, dispatcher.stopCalls)
	}
	if store.completed.Phase != dispatch.PhaseCanceled {
		t.Fatalf("dispatch was not canceled: %+v", store.completed)
	}
}

func TestReconcilerRejectsPermanentDispatchWithoutRetryingTheSameExecution(t *testing.T) {
	clock := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	repository := memory.New()
	created := createRunningJob(t, repository, clock)
	store := newFakeStore(dispatch.Record{
		Identity: dispatch.Identity{JobUID: created.UID, Epoch: 1},
		Key:      created.Key,
		OwnerID:  "scheduler-a",
		Phase:    dispatch.PhaseClaimed,
	})
	dispatcher := &fakeDispatcher{err: Permanent(errors.New("connectionRef cannot be resolved"))}
	reconciler := newTestReconciler(t, store, repository, dispatcher, clock)

	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("reconcile permanent error: %v", err)
	}
	stored, err := repository.Get(context.Background(), created.Key)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if stored.Status.State != job.StateFailed || store.completed.Phase != dispatch.PhaseFailed {
		t.Fatalf("permanent dispatch was not fenced: status=%+v dispatch=%+v", stored.Status, store.completed)
	}
	if dispatcher.reconcileCalls != 1 {
		t.Fatalf("permanent dispatch was retried in the same tick: %d", dispatcher.reconcileCalls)
	}
}

func TestReconcilerKeepsStartingPhaseWhenDispatchFailsTransiently(t *testing.T) {
	clock := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	repository := memory.New()
	created := createRunningJob(t, repository, clock)
	store := newFakeStore(dispatch.Record{
		Identity: dispatch.Identity{JobUID: created.UID, Epoch: 1},
		Key:      created.Key,
		OwnerID:  "scheduler-a",
		Phase:    dispatch.PhaseClaimed,
	})
	dispatcher := &fakeDispatcher{err: errors.New("Kubernetes API unavailable")}
	reconciler := newTestReconciler(t, store, repository, dispatcher, clock)

	if err := reconciler.Tick(context.Background()); err == nil {
		t.Fatal("expected transient reconciliation failure")
	}
	if store.record.Phase != dispatch.PhaseStarting ||
		store.record.LastError != "Kubernetes API unavailable" {
		t.Fatalf("transient failure regressed durable phase: %+v", store.record)
	}
}

func TestReconcilerKeepsStoppingPhaseWhenStopFailsTransiently(t *testing.T) {
	clock := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	repository := memory.New()
	created := createRunningJob(t, repository, clock)
	current, changed, err := created.RequestStop(clock.Add(time.Minute))
	if err != nil || !changed {
		t.Fatalf("request stop: changed=%v err=%v", changed, err)
	}
	if _, err := repository.Update(context.Background(), current, created.Version); err != nil {
		t.Fatalf("persist stop: %v", err)
	}
	store := newFakeStore(dispatch.Record{
		Identity: dispatch.Identity{JobUID: created.UID, Epoch: 1},
		Key:      created.Key,
		OwnerID:  "scheduler-a",
		Phase:    dispatch.PhaseRunning,
	})
	dispatcher := &fakeDispatcher{stopErr: errors.New("delete timed out")}
	reconciler := newTestReconciler(t, store, repository, dispatcher, clock)

	if err := reconciler.Tick(context.Background()); err == nil {
		t.Fatal("expected transient stop failure")
	}
	if store.record.Phase != dispatch.PhaseStopping || store.record.LastError != "delete timed out" {
		t.Fatalf("transient stop regressed durable phase: %+v", store.record)
	}
}

func TestReconcilerFailsExecutionAfterHeartbeatTimeout(t *testing.T) {
	clock := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	repository := memory.New()
	created := createRunningJob(t, repository, clock)
	store := newFakeStore(dispatch.Record{
		Identity: dispatch.Identity{JobUID: created.UID, Epoch: 1},
		Key:      created.Key, OwnerID: "scheduler-a", Phase: dispatch.PhaseRunning,
		LastHeartbeatAt: clock.Add(-3 * time.Minute),
	})
	dispatcher := &fakeDispatcher{observation: Observation{State: ObservationPending}, stopResult: true}
	reconciler := newTestReconciler(t, store, repository, dispatcher, clock)

	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("heartbeat timeout reconciliation: %v", err)
	}
	stored, err := repository.Get(context.Background(), created.Key)
	if err != nil {
		t.Fatalf("get failed job: %v", err)
	}
	if stored.Status.State != job.StateFailed || stored.Status.Failure == nil ||
		stored.Status.Failure.Reason != "HeartbeatTimeout" {
		t.Fatalf("unexpected heartbeat failure: %+v", stored.Status)
	}
	if dispatcher.stopCalls != 1 || store.completed.Phase != dispatch.PhaseFailed {
		t.Fatalf("stale execution was not fenced and completed: stops=%d dispatch=%+v", dispatcher.stopCalls, store.completed)
	}
}

func TestReconcilerDoesNotFailExecutionWhenHeartbeatWinsFenceRace(t *testing.T) {
	clock := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	repository := memory.New()
	created := createRunningJob(t, repository, clock)
	store := newFakeStore(dispatch.Record{
		Identity: dispatch.Identity{JobUID: created.UID, Epoch: 1},
		Key:      created.Key, OwnerID: "scheduler-a", Phase: dispatch.PhaseRunning,
		LastHeartbeatAt: clock.Add(-3 * time.Minute),
	})
	dispatcher := &fakeDispatcher{
		observation: Observation{State: ObservationRunning},
		reconcileHook: func() {
			store.mu.Lock()
			defer store.mu.Unlock()
			store.record.LastHeartbeatAt = clock
		},
	}
	reconciler := newTestReconciler(t, store, repository, dispatcher, clock)

	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("heartbeat race reconciliation: %v", err)
	}
	stored, err := repository.Get(context.Background(), created.Key)
	if err != nil {
		t.Fatalf("get running job: %v", err)
	}
	if stored.Status.State == job.StateFailed || dispatcher.stopCalls != 0 ||
		store.record.Phase == dispatch.PhaseStopping {
		t.Fatalf("fresh heartbeat lost fencing race: job=%+v dispatch=%+v stops=%d",
			stored.Status, store.record, dispatcher.stopCalls)
	}
}

func TestReconcilerResumesHeartbeatFailureAfterFenceOwnerCrashes(t *testing.T) {
	clock := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	repository := memory.New()
	created := createRunningJob(t, repository, clock)
	store := newFakeStore(dispatch.Record{
		Identity: dispatch.Identity{JobUID: created.UID, Epoch: 1},
		Key:      created.Key, OwnerID: "scheduler-a", Phase: dispatch.PhaseStopping,
		LastError: "Coordinator remained active without a heartbeat", UpdatedAt: clock.Add(-time.Minute),
	})
	dispatcher := &fakeDispatcher{stopResult: true}
	reconciler := newTestReconciler(t, store, repository, dispatcher, clock)

	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("resume fenced heartbeat failure: %v", err)
	}
	stored, err := repository.Get(context.Background(), created.Key)
	if err != nil {
		t.Fatalf("get failed job: %v", err)
	}
	if stored.Status.State != job.StateFailed || stored.Status.Failure == nil ||
		stored.Status.Failure.Reason != "HeartbeatTimeout" || dispatcher.reconcileCalls != 0 ||
		dispatcher.stopCalls != 1 || store.completed.Phase != dispatch.PhaseFailed {
		t.Fatalf("fenced failure was not resumed: job=%+v dispatch=%+v reconciles=%d stops=%d",
			stored.Status, store.completed, dispatcher.reconcileCalls, dispatcher.stopCalls)
	}
}

func TestReconcilerDoesNotForgeHeartbeatFromKubernetesObservation(t *testing.T) {
	clock := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	heartbeat := clock.Add(-time.Minute)
	repository := memory.New()
	created := createRunningJob(t, repository, clock)
	store := newFakeStore(dispatch.Record{
		Identity: dispatch.Identity{JobUID: created.UID, Epoch: 1},
		Key:      created.Key, OwnerID: "scheduler-a", Phase: dispatch.PhaseClaimed,
		LastHeartbeatAt: heartbeat,
	})
	dispatcher := &fakeDispatcher{observation: Observation{State: ObservationRunning}}
	reconciler := newTestReconciler(t, store, repository, dispatcher, clock)

	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("running reconciliation: %v", err)
	}
	if !store.record.LastHeartbeatAt.Equal(heartbeat) || store.record.Phase != dispatch.PhaseRunning {
		t.Fatalf("Kubernetes observation changed execution heartbeat: %+v", store.record)
	}
}

func createRunningJob(t *testing.T, repository *memory.Repository, now time.Time) job.Job {
	t.Helper()
	spec := job.Spec{
		Source:   job.ConnectorSpec{Connector: "jdbc", Options: map[string]string{"url": "jdbc:test"}},
		Sink:     job.ConnectorSpec{Connector: "jdbc", Options: map[string]string{"url": "jdbc:test"}},
		Delivery: job.DeliverySpec{Guarantee: job.DeliveryAtLeastOnce},
		Runtime:  job.RuntimeSpec{MaxBatchRecords: 32},
	}
	created, err := job.New(job.Key{Namespace: "default", Name: "orders"}, uuid.NewString(), spec, now)
	if err != nil {
		t.Fatalf("new job: %v", err)
	}
	started, changed, err := created.RequestStart(now.Add(time.Minute))
	if err != nil || !changed {
		t.Fatalf("start job: changed=%v err=%v", changed, err)
	}
	if _, err := repository.Create(context.Background(), created); err != nil {
		t.Fatalf("create job: %v", err)
	}
	stored, err := repository.Get(context.Background(), created.Key)
	if err != nil {
		t.Fatalf("read created job: %v", err)
	}
	updated, err := repository.Update(context.Background(), started, stored.Version)
	if err != nil {
		t.Fatalf("persist start: %v", err)
	}
	return updated
}

func newTestReconciler(
	t *testing.T,
	store *fakeStore,
	repository *memory.Repository,
	dispatcher *fakeDispatcher,
	clock time.Time,
) *Reconciler {
	t.Helper()
	reconciler, err := New(
		Config{
			OwnerID: "scheduler-a", MaximumActive: 2,
			LeaseDuration: 10 * time.Minute, HeartbeatTimeout: 2 * time.Minute,
			ReconcileEvery: time.Minute, OperationTimeout: time.Second,
		},
		store,
		repository,
		dispatcher,
		func() time.Time { return clock },
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("new reconciler: %v", err)
	}
	return reconciler
}

type fakeStore struct {
	mu        sync.Mutex
	record    dispatch.Record
	completed dispatch.Record
}

func newFakeStore(record dispatch.Record) *fakeStore {
	record.LeaseExpiresAt = time.Now().Add(time.Hour)
	return &fakeStore{record: record}
}

func (s *fakeStore) Migrate(context.Context) error { return nil }

func (s *fakeStore) Claim(
	_ context.Context, owner string, _ int, _ time.Duration, _ time.Duration, _ time.Time,
) ([]dispatch.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.record.OwnerID = owner
	return []dispatch.Record{s.record}, nil
}

func (s *fakeStore) List(context.Context) ([]dispatch.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return []dispatch.Record{s.record}, nil
}

func (s *fakeStore) Update(
	_ context.Context, identity dispatch.Identity, owner string, phase dispatch.Phase,
	lastError string, _ time.Duration, now time.Time,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record.Identity != identity || s.record.OwnerID != owner {
		return dispatch.ErrLeaseLost
	}
	s.record.Phase = phase
	s.record.LastError = lastError
	s.record.LeaseExpiresAt = now.Add(time.Hour)
	return nil
}

func (s *fakeStore) RecordHeartbeat(
	_ context.Context, identity dispatch.Identity, token string, now time.Time,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record.Identity != identity || s.record.HeartbeatToken != token {
		return dispatch.ErrLeaseLost
	}
	s.record.LastHeartbeatAt = now
	return nil
}

func (s *fakeStore) FenceExpiredHeartbeat(
	_ context.Context,
	identity dispatch.Identity,
	owner string,
	lastError string,
	timeout time.Duration,
	leaseDuration time.Duration,
	now time.Time,
) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record.Identity != identity || s.record.OwnerID != owner ||
		!s.record.LeaseExpiresAt.After(now) || !heartbeatFenceable(s.record.Phase) {
		return false, nil
	}
	if !s.record.LastHeartbeatAt.IsZero() && s.record.LastHeartbeatAt.After(now.Add(-timeout)) {
		return false, nil
	}
	s.record.Phase = dispatch.PhaseStopping
	s.record.LastError = lastError
	s.record.LeaseExpiresAt = now.Add(leaseDuration)
	s.record.UpdatedAt = now
	return true, nil
}

func heartbeatFenceable(phase dispatch.Phase) bool {
	return phase == dispatch.PhaseClaimed ||
		phase == dispatch.PhaseStarting ||
		phase == dispatch.PhaseRunning
}

func (s *fakeStore) Complete(
	_ context.Context, identity dispatch.Identity, owner string, phase dispatch.Phase,
	lastError string, now time.Time,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record.Identity != identity || s.record.OwnerID != owner {
		return dispatch.ErrLeaseLost
	}
	s.record.Phase = phase
	s.record.LastError = lastError
	s.record.UpdatedAt = now
	s.completed = s.record
	return nil
}

type fakeDispatcher struct {
	observation    Observation
	err            error
	stopResult     bool
	stopErr        error
	reconcileHook  func()
	reconcileCalls int
	stopCalls      int
}

func (d *fakeDispatcher) Reconcile(context.Context, job.Job, dispatch.Record) (Observation, error) {
	d.reconcileCalls++
	if d.reconcileHook != nil {
		d.reconcileHook()
	}
	return d.observation, d.err
}

func (d *fakeDispatcher) Stop(context.Context, dispatch.Identity) (bool, error) {
	d.stopCalls++
	return d.stopResult, d.stopErr
}

func (d *fakeDispatcher) Cleanup(context.Context, dispatch.Identity) error {
	return nil
}

func (d *fakeDispatcher) SweepOrphans(context.Context, []dispatch.Record) error {
	return nil
}
