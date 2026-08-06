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

func TestStoreSerializesCapacityAndFencesLeaseAndHeartbeatTakeover(t *testing.T) {
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
	defer func() {
		if _, cleanupErr := databaseA.ExecContext(
			context.Background(), `DELETE FROM astrasync_control_jobs WHERE namespace = $1`, namespace,
		); cleanupErr != nil {
			t.Errorf("clean integration Jobs: %v", cleanupErr)
		}
	}()
	first := createIntegrationJob(t, ctx, jobs, namespace, "first", now)
	second := createIntegrationJob(t, ctx, jobs, namespace, "second", now.Add(time.Second))
	lease := 10 * time.Second
	heartbeatTimeout := 5 * time.Second
	claimedA, err := storeA.Claim(ctx, "scheduler-a", 1, lease, heartbeatTimeout, now.Add(2*time.Second))
	if err != nil || len(claimedA) != 1 {
		t.Fatalf("scheduler A claim: records=%+v err=%v", claimedA, err)
	}
	if claimedA[0].Identity.JobUID != first.UID || claimedA[0].Attempt != 1 {
		t.Fatalf("unexpected first admission: %+v", claimedA[0])
	}
	heartbeatAt := now.Add(3 * time.Second)
	if err := storeA.RecordHeartbeat(
		ctx, claimedA[0].Identity, claimedA[0].HeartbeatToken, heartbeatAt,
	); err != nil {
		t.Fatalf("record execution heartbeat: %v", err)
	}
	if err := storeA.RecordHeartbeat(
		ctx, claimedA[0].Identity, uuid.NewString(), heartbeatAt,
	); !errors.Is(err, dispatch.ErrLeaseLost) {
		t.Fatalf("incorrect heartbeat token was accepted: %v", err)
	}
	claimedB, err := storeB.Claim(ctx, "scheduler-b", 1, lease, heartbeatTimeout, now.Add(4*time.Second))
	if err != nil || len(claimedB) != 0 {
		t.Fatalf("capacity was over-admitted before heartbeat timeout: records=%+v err=%v", claimedB, err)
	}
	if renewed, renewErr := storeA.Claim(
		ctx, "scheduler-a", 1, lease, heartbeatTimeout, now.Add(6*time.Second),
	); renewErr != nil || len(renewed) != 1 || !renewed[0].LastHeartbeatAt.Equal(heartbeatAt) {
		t.Fatalf("renew owner lease without heartbeat: records=%+v err=%v", renewed, renewErr)
	}

	// The owner lease is valid until second 16, but its independent heartbeat is stale at second 9.
	takeoverTime := now.Add(9 * time.Second)
	claimedB, err = storeB.Claim(ctx, "scheduler-b", 1, lease, heartbeatTimeout, takeoverTime)
	if err != nil || len(claimedB) != 1 {
		t.Fatalf("stale heartbeat takeover: records=%+v err=%v", claimedB, err)
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
	if fenced, fenceErr := storeA.FenceExpiredHeartbeat(
		ctx, claimedA[0].Identity, "scheduler-a", "stale owner", heartbeatTimeout, lease, takeoverTime,
	); fenceErr != nil || fenced {
		t.Fatalf("stale owner fenced the execution: fenced=%v err=%v", fenced, fenceErr)
	}
	if err := storeA.RecordHeartbeat(
		ctx, claimedB[0].Identity, claimedB[0].HeartbeatToken, takeoverTime,
	); err != nil {
		t.Fatalf("record heartbeat during fence race: %v", err)
	}
	if fenced, fenceErr := storeB.FenceExpiredHeartbeat(
		ctx, claimedB[0].Identity, "scheduler-b", "premature timeout", heartbeatTimeout, lease, takeoverTime,
	); fenceErr != nil || fenced {
		t.Fatalf("fresh heartbeat was fenced: fenced=%v err=%v", fenced, fenceErr)
	}
	fenceTime := takeoverTime.Add(heartbeatTimeout)
	if fenced, fenceErr := storeB.FenceExpiredHeartbeat(
		ctx, claimedB[0].Identity, "scheduler-b", "heartbeat timeout", heartbeatTimeout, lease, fenceTime,
	); fenceErr != nil || !fenced {
		t.Fatalf("expired heartbeat was not fenced atomically: fenced=%v err=%v", fenced, fenceErr)
	}

	finishIntegrationJob(t, ctx, jobs, claimedB[0].Key, fenceTime)
	if err := storeB.Complete(
		ctx,
		claimedB[0].Identity,
		"scheduler-b",
		dispatch.PhaseSucceeded,
		"",
		fenceTime.Add(time.Second),
	); err != nil {
		t.Fatalf("complete first dispatch: %v", err)
	}
	if err := storeA.RecordHeartbeat(
		ctx, claimedB[0].Identity, claimedB[0].HeartbeatToken, fenceTime.Add(2*time.Second),
	); !errors.Is(err, dispatch.ErrLeaseLost) {
		t.Fatalf("terminal execution heartbeat was accepted: %v", err)
	}
	secondClaimTime := fenceTime.Add(2 * time.Second)
	claimedB, err = storeB.Claim(ctx, "scheduler-b", 1, lease, heartbeatTimeout, secondClaimTime)
	if err != nil || len(claimedB) != 1 {
		t.Fatalf("claim after capacity release: records=%+v err=%v", claimedB, err)
	}
	if claimedB[0].Identity.JobUID != second.UID || claimedB[0].Identity.Epoch != 1 {
		t.Fatalf("second execution was not admitted after release: %+v", claimedB[0])
	}
	secondClaim := claimedB[0]
	if err := storeB.RecordHeartbeat(
		ctx, secondClaim.Identity, secondClaim.HeartbeatToken, secondClaimTime.Add(lease),
	); err != nil {
		t.Fatalf("refresh heartbeat before lease takeover: %v", err)
	}
	leaseTakeoverTime := secondClaimTime.Add(lease + time.Second)
	claimedC, err := storeA.Claim(
		ctx, "scheduler-c", 1, lease, heartbeatTimeout, leaseTakeoverTime,
	)
	if err != nil || len(claimedC) != 1 {
		t.Fatalf("expired lease takeover: records=%+v err=%v", claimedC, err)
	}
	if claimedC[0].Identity != secondClaim.Identity || claimedC[0].Attempt != 2 {
		t.Fatalf("lease takeover changed execution identity: before=%+v after=%+v", secondClaim, claimedC[0])
	}
	records, err := storeA.List(ctx)
	if err != nil || len(records) < 2 {
		t.Fatalf("list dispatch history: records=%+v err=%v", records, err)
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
