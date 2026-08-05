package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"io.astrasync/control-plane/job"
	jobpostgres "io.astrasync/control-plane/job/postgres"
	"io.astrasync/control-plane/scheduler/internal/dispatch"
	dispatchpostgres "io.astrasync/control-plane/scheduler/internal/dispatch/postgres"
)

func TestStoreSerializesCapacityAndTakesOverOnlyTheExpiredEpoch(t *testing.T) {
	dataSourceName := os.Getenv("ASTRASYNC_TEST_POSTGRES_URL")
	if dataSourceName == "" {
		t.Skip("ASTRASYNC_TEST_POSTGRES_URL is not configured")
	}
	ctx := context.Background()
	databaseA, err := sql.Open("pgx", dataSourceName)
	if err != nil {
		t.Fatalf("open database A: %v", err)
	}
	defer databaseA.Close()
	databaseB, err := sql.Open("pgx", dataSourceName)
	if err != nil {
		t.Fatalf("open database B: %v", err)
	}
	defer databaseB.Close()
	jobs := jobpostgres.New(databaseA)
	if err := jobs.Migrate(ctx); err != nil {
		t.Fatalf("migrate jobs: %v", err)
	}
	storeA := dispatchpostgres.New(databaseA)
	storeB := dispatchpostgres.New(databaseB)
	if err := storeA.Migrate(ctx); err != nil {
		t.Fatalf("migrate dispatches: %v", err)
	}

	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	namespace := "scheduler-it-" + uuid.NewString()
	first := createIntegrationJob(t, ctx, jobs, namespace, "first", now)
	second := createIntegrationJob(t, ctx, jobs, namespace, "second", now.Add(time.Second))
	lease := 10 * time.Second

	claimedA, err := storeA.Claim(ctx, "scheduler-a", 1, lease, now.Add(2*time.Second))
	if err != nil || len(claimedA) != 1 {
		t.Fatalf("scheduler A claim: records=%+v err=%v", claimedA, err)
	}
	if claimedA[0].Identity.JobUID != first.UID || claimedA[0].Attempt != 1 {
		t.Fatalf("unexpected first admission: %+v", claimedA[0])
	}
	claimedB, err := storeB.Claim(ctx, "scheduler-b", 1, lease, now.Add(3*time.Second))
	if err != nil || len(claimedB) != 0 {
		t.Fatalf("capacity was over-admitted before lease expiry: records=%+v err=%v", claimedB, err)
	}

	takeoverTime := now.Add(13 * time.Second)
	claimedB, err = storeB.Claim(ctx, "scheduler-b", 1, lease, takeoverTime)
	if err != nil || len(claimedB) != 1 {
		t.Fatalf("expired lease takeover: records=%+v err=%v", claimedB, err)
	}
	if claimedB[0].Identity != claimedA[0].Identity || claimedB[0].Attempt != 2 {
		t.Fatalf("takeover allocated a different execution: before=%+v after=%+v", claimedA[0], claimedB[0])
	}
	if err := storeA.Update(
		ctx,
		claimedA[0].Identity,
		"scheduler-a",
		dispatch.PhaseRunning,
		"",
		lease,
		takeoverTime,
	); !errors.Is(err, dispatch.ErrLeaseLost) {
		t.Fatalf("stale owner retained dispatch authority: %v", err)
	}

	finishIntegrationJob(t, ctx, jobs, claimedB[0].Key, takeoverTime)
	if err := storeB.Complete(
		ctx,
		claimedB[0].Identity,
		"scheduler-b",
		dispatch.PhaseSucceeded,
		"",
		takeoverTime.Add(time.Second),
	); err != nil {
		t.Fatalf("complete first dispatch: %v", err)
	}
	claimedB, err = storeB.Claim(ctx, "scheduler-b", 1, lease, takeoverTime.Add(2*time.Second))
	if err != nil || len(claimedB) != 1 {
		t.Fatalf("claim after capacity release: records=%+v err=%v", claimedB, err)
	}
	if claimedB[0].Identity.JobUID != second.UID || claimedB[0].Identity.Epoch != 1 {
		t.Fatalf("second execution was not admitted after release: %+v", claimedB[0])
	}
}

func createIntegrationJob(
	t *testing.T,
	ctx context.Context,
	repository *jobpostgres.Repository,
	namespace string,
	name string,
	now time.Time,
) job.Job {
	t.Helper()
	spec := job.Spec{
		Source:   job.ConnectorSpec{Connector: "jdbc", Options: map[string]string{"table": "source"}},
		Sink:     job.ConnectorSpec{Connector: "jdbc", Options: map[string]string{"table": "target"}},
		Delivery: job.DeliverySpec{Guarantee: job.DeliveryAtLeastOnce},
		Runtime:  job.RuntimeSpec{MaxBatchRecords: 128},
	}
	created, err := job.New(job.Key{Namespace: namespace, Name: name}, uuid.NewString(), spec, now)
	if err != nil {
		t.Fatalf("new job: %v", err)
	}
	stored, err := repository.Create(ctx, created)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	started, _, err := stored.RequestStart(now.Add(time.Millisecond))
	if err != nil {
		t.Fatalf("start job: %v", err)
	}
	updated, err := repository.Update(ctx, started, stored.Version)
	if err != nil {
		t.Fatalf("persist job start: %v", err)
	}
	return updated
}

func finishIntegrationJob(
	t *testing.T,
	ctx context.Context,
	repository *jobpostgres.Repository,
	key job.Key,
	now time.Time,
) {
	t.Helper()
	current, err := repository.Get(ctx, key)
	if err != nil {
		t.Fatalf("get claimed job: %v", err)
	}
	running, _, err := current.Advance(current.Status.Epoch, job.StateRunning, nil, now)
	if err != nil {
		t.Fatalf("advance running: %v", err)
	}
	running, err = repository.Update(ctx, running, current.Version)
	if err != nil {
		t.Fatalf("persist running: %v", err)
	}
	finished, _, err := running.Advance(running.Status.Epoch, job.StateFinished, nil, now.Add(time.Millisecond))
	if err != nil {
		t.Fatalf("advance finished: %v", err)
	}
	if _, err := repository.Update(ctx, finished, running.Version); err != nil {
		t.Fatalf("persist finished: %v", err)
	}
}
