package service_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	jobv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/api-server/internal/service"
	"io.astrasync/control-plane/job"
	"io.astrasync/control-plane/job/memory"
)

func TestJobServiceRunsIdempotentLifecycle(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 5, 5, 0, 0, 0, time.UTC)
	jobService, err := service.NewJobService(memory.New(), func() time.Time { return now }, func() string {
		return "c2131d55-c662-4757-a590-89080fc65ebd"
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	created, err := jobService.CreateJob(ctx, &jobv1.CreateJobRequest{
		Namespace: "default", Name: "orders", Spec: validProtoSpec(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.GetVersion() != 1 || created.GetStatus().GetState() != jobv1.JobState_JOB_STATE_CREATED {
		t.Fatalf("unexpected created job: %+v", created)
	}
	if _, err := jobService.CreateJob(ctx, &jobv1.CreateJobRequest{
		Namespace: "default", Name: "orders", Spec: validProtoSpec(),
	}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("expected duplicate status, got %v", err)
	}

	now = now.Add(time.Minute)
	started, err := jobService.StartJob(ctx, &jobv1.StartJobRequest{
		Namespace: "default", Name: "orders", ExpectedVersion: 1,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if started.GetVersion() != 2 || started.GetStatus().GetEpoch() != 1 ||
		started.GetStatus().GetState() != jobv1.JobState_JOB_STATE_INITIALIZING {
		t.Fatalf("unexpected started job: %+v", started)
	}
	repeatedStart, err := jobService.StartJob(ctx, &jobv1.StartJobRequest{
		Namespace: "default", Name: "orders", ExpectedVersion: 1,
	})
	if err != nil || repeatedStart.GetVersion() != 2 {
		t.Fatalf("start retry was not idempotent: job=%+v err=%v", repeatedStart, err)
	}
	if _, err := jobService.UpdateJob(ctx, &jobv1.UpdateJobRequest{
		Namespace: "default", Name: "orders", ExpectedVersion: 2, Spec: validProtoSpec(),
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expected active update rejection, got %v", err)
	}

	now = now.Add(time.Minute)
	stopping, err := jobService.StopJob(ctx, &jobv1.StopJobRequest{
		Namespace: "default", Name: "orders", ExpectedVersion: 2,
	})
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if stopping.GetVersion() != 3 || stopping.GetStatus().GetState() != jobv1.JobState_JOB_STATE_CANCELING {
		t.Fatalf("unexpected stopping job: %+v", stopping)
	}
	repeatedStop, err := jobService.StopJob(ctx, &jobv1.StopJobRequest{
		Namespace: "default", Name: "orders", ExpectedVersion: 2,
	})
	if err != nil || repeatedStop.GetVersion() != 3 {
		t.Fatalf("stop retry was not idempotent: job=%+v err=%v", repeatedStop, err)
	}

	listed, err := jobService.ListJobs(ctx, &jobv1.ListJobsRequest{Namespace: "default"})
	if err != nil || listed.GetTotal() != 1 || len(listed.GetItems()) != 1 {
		t.Fatalf("list: response=%+v err=%v", listed, err)
	}
	jobStatus, err := jobService.GetJobStatus(ctx, &jobv1.GetJobStatusRequest{
		Namespace: "default", Name: "orders",
	})
	if err != nil || jobStatus.GetDesiredState() != jobv1.DesiredState_DESIRED_STATE_STOPPED {
		t.Fatalf("status: response=%+v err=%v", jobStatus, err)
	}
}

func TestJobServiceUpdatesAndDeletesWithOptimisticVersions(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 5, 5, 0, 0, 0, time.UTC)
	jobService, err := service.NewJobService(memory.New(), func() time.Time { return now }, func() string {
		return "2c327ac8-0d4e-49b2-8cee-4f4e0df4785c"
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	created, err := jobService.CreateJob(ctx, &jobv1.CreateJobRequest{
		Namespace: "default", Name: "mutable", Spec: validProtoSpec(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updatedSpec := validProtoSpec()
	updatedSpec.Runtime.MaxBatchRecords = 512
	now = now.Add(time.Minute)
	updated, err := jobService.UpdateJob(ctx, &jobv1.UpdateJobRequest{
		Namespace: "default", Name: "mutable", ExpectedVersion: created.GetVersion(), Spec: updatedSpec,
	})
	if err != nil || updated.GetVersion() != 2 || updated.GetSpec().GetRuntime().GetMaxBatchRecords() != 512 {
		t.Fatalf("update: job=%+v err=%v", updated, err)
	}
	if _, err := jobService.UpdateJob(ctx, &jobv1.UpdateJobRequest{
		Namespace: "default", Name: "mutable", ExpectedVersion: 1, Spec: updatedSpec,
	}); status.Code(err) != codes.Aborted {
		t.Fatalf("expected stale update conflict, got %v", err)
	}
	if _, err := jobService.DeleteJob(ctx, &jobv1.DeleteJobRequest{
		Namespace: "default", Name: "mutable", ExpectedVersion: 1,
	}); status.Code(err) != codes.Aborted {
		t.Fatalf("expected stale delete conflict, got %v", err)
	}
	if _, err := jobService.DeleteJob(ctx, &jobv1.DeleteJobRequest{
		Namespace: "default", Name: "mutable", ExpectedVersion: 2,
	}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := jobService.GetJob(ctx, &jobv1.GetJobRequest{
		Namespace: "default", Name: "mutable",
	}); status.Code(err) != codes.NotFound {
		t.Fatalf("expected missing job, got %v", err)
	}
}

func TestJobServiceValidatesRequestsAndPagination(t *testing.T) {
	jobService, err := service.NewJobService(memory.New(), time.Now, func() string { return "uid" })
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	ctx := context.Background()
	for _, test := range []struct {
		name string
		call func() error
	}{
		{name: "nil create", call: func() error { _, err := jobService.CreateJob(ctx, nil); return err }},
		{name: "invalid name", call: func() error {
			_, err := jobService.GetJob(ctx, &jobv1.GetJobRequest{Namespace: "default", Name: "Orders"})
			return err
		}},
		{name: "oversized page", call: func() error {
			_, err := jobService.ListJobs(ctx, &jobv1.ListJobsRequest{Namespace: "default", PageSize: 101})
			return err
		}},
		{name: "missing delivery", call: func() error {
			spec := validProtoSpec()
			spec.Delivery = nil
			_, err := jobService.CreateJob(ctx, &jobv1.CreateJobRequest{
				Namespace: "default", Name: "invalid", Spec: spec,
			})
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if code := status.Code(test.call()); code != codes.InvalidArgument {
				t.Fatalf("expected invalid argument, got %s", code)
			}
		})
	}
}

func TestJobServiceAcceptsConcurrentMatchingStartAsIdempotent(t *testing.T) {
	repository := &conflictAfterCommitRepository{Repository: memory.New()}
	jobService, err := service.NewJobService(
		repository,
		func() time.Time { return time.Date(2026, 8, 5, 5, 0, 0, 0, time.UTC) },
		func() string { return "7417e7a4-53ba-454d-a724-146009d7854f" },
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	ctx := context.Background()
	if _, err := jobService.CreateJob(ctx, &jobv1.CreateJobRequest{
		Namespace: "default", Name: "racing-start", Spec: validProtoSpec(),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	started, err := jobService.StartJob(ctx, &jobv1.StartJobRequest{
		Namespace: "default", Name: "racing-start", ExpectedVersion: 1,
	})
	if err != nil || started.GetVersion() != 2 || started.GetStatus().GetEpoch() != 1 {
		t.Fatalf("concurrent matching start: job=%+v err=%v", started, err)
	}
}

type conflictAfterCommitRepository struct {
	job.Repository
	once sync.Once
}

func (r *conflictAfterCommitRepository) Update(
	ctx context.Context, candidate job.Job, expectedVersion int64,
) (job.Job, error) {
	var committed bool
	var commitErr error
	r.once.Do(func() {
		_, commitErr = r.Repository.Update(ctx, candidate, expectedVersion)
		committed = true
	})
	if committed {
		if commitErr != nil {
			return job.Job{}, commitErr
		}
		return job.Job{}, job.ErrConflict
	}
	return r.Repository.Update(ctx, candidate, expectedVersion)
}

var _ job.Repository = (*conflictAfterCommitRepository)(nil)

func TestJobServiceDefaultsEmptyRuntimeConfig(t *testing.T) {
	jobService, err := service.NewJobService(
		memory.New(), time.Now, func() string { return "1cf9e8a0-a9f4-44b1-a627-b5d0171d9a4d" },
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	spec := validProtoSpec()
	spec.Runtime = &jobv1.RuntimeConfig{}
	created, err := jobService.CreateJob(context.Background(), &jobv1.CreateJobRequest{
		Namespace: "default", Name: "runtime-default", Spec: spec,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := created.GetSpec().GetRuntime().GetMaxBatchRecords(); got != job.DefaultMaxBatchRecords {
		t.Fatalf("expected runtime default %d, got %d", job.DefaultMaxBatchRecords, got)
	}
}

func validProtoSpec() *jobv1.JobSpec {
	return &jobv1.JobSpec{
		Source: &jobv1.ConnectorConfig{
			Connector: "mysql-cdc", Options: map[string]string{"table": "shop.orders"},
		},
		Sink: &jobv1.ConnectorConfig{
			Connector: "jdbc", Options: map[string]string{"table": "orders"},
		},
		Delivery: &jobv1.DeliveryConfig{
			Guarantee: jobv1.DeliveryGuarantee_DELIVERY_GUARANTEE_EXACTLY_ONCE,
		},
		Runtime: &jobv1.RuntimeConfig{MaxBatchRecords: 128},
	}
}
