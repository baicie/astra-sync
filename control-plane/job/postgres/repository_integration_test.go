//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	authpostgres "io.astrasync/control-plane/auth/postgres"
	"io.astrasync/control-plane/job"
	jobpostgres "io.astrasync/control-plane/job/postgres"
)

func TestRepositoryPersistsLifecycleAcrossConnections(t *testing.T) {
	dataSourceName := startPostgresContainer(t)
	ctx := context.Background()
	authRepository, err := authpostgres.Open(ctx, dataSourceName)
	if err != nil {
		t.Fatalf("open auth repository: %v", err)
	}
	if err := authRepository.Migrate(ctx); err != nil {
		authRepository.Close()
		t.Fatalf("migrate auth: %v", err)
	}
	if err := authRepository.Close(); err != nil {
		t.Fatalf("close auth repository: %v", err)
	}
	repository, err := jobpostgres.Open(ctx, dataSourceName)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	if err := repository.Migrate(ctx); err != nil {
		repository.Close()
		t.Fatalf("migrate: %v", err)
	}

	namespace := "it-" + uuid.NewString()
	created := integrationJob(t, namespace)
	stored, err := repository.Create(ctx, created)
	if err != nil {
		repository.Close()
		t.Fatalf("create: %v", err)
	}
	if _, err := repository.Create(ctx, created); !errors.Is(err, job.ErrAlreadyExists) {
		repository.Close()
		t.Fatalf("expected duplicate error, got %v", err)
	}

	initializing, _, err := stored.RequestStart(stored.UpdatedAt.Add(time.Minute))
	if err != nil {
		repository.Close()
		t.Fatalf("request start: %v", err)
	}
	updated, err := repository.Update(ctx, initializing, stored.Version)
	if err != nil || updated.Version != 2 {
		repository.Close()
		t.Fatalf("update: version=%d err=%v", updated.Version, err)
	}
	if _, err := repository.Update(ctx, initializing, stored.Version); !errors.Is(err, job.ErrConflict) {
		repository.Close()
		t.Fatalf("expected stale update conflict, got %v", err)
	}
	page, err := repository.List(ctx, namespace, job.Page{Number: 1, Size: 10})
	if err != nil || page.Total != 1 || len(page.Jobs) != 1 {
		repository.Close()
		t.Fatalf("list: page=%+v err=%v", page, err)
	}
	emptyPage, err := repository.List(ctx, namespace, job.Page{Number: 2, Size: 10})
	if err != nil || emptyPage.Total != 1 || len(emptyPage.Jobs) != 0 {
		repository.Close()
		t.Fatalf("empty page: page=%+v err=%v", emptyPage, err)
	}
	if err := repository.Close(); err != nil {
		t.Fatalf("close repository: %v", err)
	}

	reopened, err := jobpostgres.Open(ctx, dataSourceName)
	if err != nil {
		t.Fatalf("reopen repository: %v", err)
	}
	defer reopened.Close()
	recovered, err := reopened.Get(ctx, created.Key)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if recovered.Status.State != job.StateInitializing || recovered.Status.Epoch != 1 || recovered.Version != 2 {
		t.Fatalf("unexpected recovered job: %+v", recovered)
	}
	if err := reopened.Delete(ctx, created.Key, 1); !errors.Is(err, job.ErrConflict) {
		t.Fatalf("expected delete conflict, got %v", err)
	}
	if err := reopened.Delete(ctx, created.Key, recovered.Version); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := reopened.Get(ctx, created.Key); !errors.Is(err, job.ErrNotFound) {
		t.Fatalf("expected deleted job to be absent, got %v", err)
	}
}

func TestRepositoryFencesStaleEpochWriter(t *testing.T) {
	dataSourceName := startPostgresContainer(t)
	ctx := context.Background()
	repository, err := jobpostgres.Open(ctx, dataSourceName)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Errorf("close repository: %v", err)
		}
	})
	if err := repository.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	namespace := "it-stale-epoch-" + uuid.NewString()
	created := integrationJob(t, namespace)
	stored, err := repository.Create(ctx, created)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	initializing, changed, err := stored.RequestStart(stored.UpdatedAt.Add(time.Minute))
	if err != nil || !changed {
		t.Fatalf("request start: changed=%v err=%v", changed, err)
	}
	stored, err = repository.Update(ctx, initializing, stored.Version)
	if err != nil {
		t.Fatalf("persist initializing: %v", err)
	}
	running, changed, err := stored.Advance(stored.Status.Epoch, job.StateRunning, nil, stored.UpdatedAt.Add(time.Minute))
	if err != nil || !changed {
		t.Fatalf("advance running: changed=%v err=%v", changed, err)
	}
	stored, err = repository.Update(ctx, running, stored.Version)
	if err != nil {
		t.Fatalf("persist running: %v", err)
	}
	staleWriter := stored
	finished, changed, err := stored.Advance(stored.Status.Epoch, job.StateFinished, nil, stored.UpdatedAt.Add(time.Minute))
	if err != nil || !changed {
		t.Fatalf("advance finished: changed=%v err=%v", changed, err)
	}
	stored, err = repository.Update(ctx, finished, stored.Version)
	if err != nil {
		t.Fatalf("persist finished: %v", err)
	}
	restarted, changed, err := stored.RequestStart(stored.UpdatedAt.Add(time.Minute))
	if err != nil || !changed {
		t.Fatalf("restart: changed=%v err=%v", changed, err)
	}
	current, err := repository.Update(ctx, restarted, stored.Version)
	if err != nil {
		t.Fatalf("persist restart: %v", err)
	}

	if _, err := repository.Update(ctx, staleWriter, current.Version); !errors.Is(err, job.ErrStaleEpoch) {
		t.Fatalf("stale writer update error = %v, want ErrStaleEpoch", err)
	}
	recovered, err := repository.Get(ctx, created.Key)
	if err != nil {
		t.Fatalf("get after stale write: %v", err)
	}
	if recovered.Status.Epoch != 2 || recovered.Status.State != job.StateInitializing || recovered.Version != current.Version {
		t.Fatalf("stale writer changed durable state: %+v", recovered)
	}
}

func integrationJob(t *testing.T, namespace string) job.Job {
	t.Helper()
	spec := job.Spec{
		Source:   job.ConnectorSpec{Connector: "postgres-cdc", Options: map[string]string{"table": "public.orders"}},
		Sink:     job.ConnectorSpec{Connector: "jdbc", Options: map[string]string{"table": "orders"}},
		Delivery: job.DeliverySpec{Guarantee: job.DeliveryExactlyOnce},
		Runtime:  job.RuntimeSpec{MaxBatchRecords: 256},
	}
	created, err := job.New(
		job.Key{Namespace: namespace, Name: "orders"},
		uuid.NewString(),
		spec,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("new integration job: %v", err)
	}
	return created
}
