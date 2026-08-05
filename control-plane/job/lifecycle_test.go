package job_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"io.astrasync/control-plane/job"
)

func TestLifecycleCommandsAreIdempotentAndEpochFenced(t *testing.T) {
	start := time.Date(2026, 8, 5, 5, 0, 0, 0, time.UTC)
	current := newTestJob(t, start)

	initializing, changed, err := current.RequestStart(start.Add(time.Minute))
	if err != nil || !changed {
		t.Fatalf("request start: changed=%v err=%v", changed, err)
	}
	if initializing.Status.State != job.StateInitializing || initializing.Status.Epoch != 1 {
		t.Fatalf("unexpected initializing status: %+v", initializing.Status)
	}

	repeated, changed, err := initializing.RequestStart(start.Add(2 * time.Minute))
	if err != nil || changed || repeated.Status.Epoch != 1 {
		t.Fatalf("repeated start was not idempotent: changed=%v err=%v status=%+v", changed, err, repeated.Status)
	}

	running, changed, err := initializing.Advance(1, job.StateRunning, nil, start.Add(3*time.Minute))
	if err != nil || !changed || running.Status.State != job.StateRunning {
		t.Fatalf("advance running: changed=%v err=%v status=%+v", changed, err, running.Status)
	}
	if _, _, err := running.Advance(0, job.StateFinished, nil, start.Add(4*time.Minute)); !errors.Is(err, job.ErrStaleEpoch) {
		t.Fatalf("expected stale epoch error, got %v", err)
	}

	canceling, changed, err := running.RequestStop(start.Add(5 * time.Minute))
	if err != nil || !changed || canceling.Status.State != job.StateCanceling {
		t.Fatalf("request stop: changed=%v err=%v status=%+v", changed, err, canceling.Status)
	}
	repeated, changed, err = canceling.RequestStop(start.Add(6 * time.Minute))
	if err != nil || changed {
		t.Fatalf("repeated stop was not idempotent: changed=%v err=%v", changed, err)
	}
	canceled, changed, err := canceling.Advance(1, job.StateCanceled, nil, start.Add(7*time.Minute))
	if err != nil || !changed || canceled.Status.Desired != job.DesiredStopped || canceled.Status.EndTime == nil {
		t.Fatalf("advance canceled: changed=%v err=%v status=%+v", changed, err, canceled.Status)
	}
}

func TestFailedExecutionRequiresFailureAndCanRestart(t *testing.T) {
	start := time.Date(2026, 8, 5, 5, 0, 0, 0, time.UTC)
	created := newTestJob(t, start)
	initializing, _, _ := created.RequestStart(start.Add(time.Minute))

	if _, _, err := initializing.Advance(1, job.StateFailed, nil, start.Add(2*time.Minute)); !errors.Is(err, job.ErrInvalidTransition) {
		t.Fatalf("expected required failure details, got %v", err)
	}
	failure := &job.Failure{Reason: "worker unavailable", Timestamp: start.Add(2 * time.Minute)}
	failed, changed, err := initializing.Advance(1, job.StateFailed, failure, start.Add(2*time.Minute))
	if err != nil || !changed || failed.Status.Failure == nil {
		t.Fatalf("advance failed: changed=%v err=%v status=%+v", changed, err, failed.Status)
	}

	restarted, changed, err := failed.RequestStart(start.Add(3 * time.Minute))
	if err != nil || !changed || restarted.Status.Epoch != 2 || restarted.Status.RestartCount != 1 {
		t.Fatalf("restart failed job: changed=%v err=%v status=%+v", changed, err, restarted.Status)
	}
}

func TestActiveJobRejectsSpecMutationAndDeletion(t *testing.T) {
	start := time.Date(2026, 8, 5, 5, 0, 0, 0, time.UTC)
	created := newTestJob(t, start)
	initializing, _, _ := created.RequestStart(start.Add(time.Minute))

	if _, err := initializing.ReplaceSpec(validSpec(), start.Add(2*time.Minute)); !errors.Is(err, job.ErrInvalidTransition) {
		t.Fatalf("expected active spec update rejection, got %v", err)
	}
	if err := initializing.Deletable(); !errors.Is(err, job.ErrInvalidTransition) {
		t.Fatalf("expected active delete rejection, got %v", err)
	}
	updated, err := created.ReplaceSpec(validSpec(), start.Add(2*time.Minute))
	if err != nil || !updated.UpdatedAt.Equal(start.Add(2*time.Minute)) {
		t.Fatalf("replace inactive spec: updated=%+v err=%v", updated, err)
	}
}

func newTestJob(t *testing.T, now time.Time) job.Job {
	t.Helper()
	created, err := job.New(job.Key{Namespace: "default", Name: "orders"}, uuid.NewString(), validSpec(), now)
	if err != nil {
		t.Fatalf("new job: %v", err)
	}
	return created
}
