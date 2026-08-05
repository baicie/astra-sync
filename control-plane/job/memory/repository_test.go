package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"io.astrasync/control-plane/job"
	"io.astrasync/control-plane/job/memory"
)

func TestRepositoryEnforcesVersionsAndReturnsCopies(t *testing.T) {
	ctx := context.Background()
	repository := memory.New()
	created := testJob(t, "orders")

	stored, err := repository.Create(ctx, created)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := repository.Create(ctx, created); !errors.Is(err, job.ErrAlreadyExists) {
		t.Fatalf("expected duplicate error, got %v", err)
	}

	stored.Spec.Source.Options["table"] = "mutated"
	loaded, err := repository.Get(ctx, created.Key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if loaded.Spec.Source.Options["table"] != "orders" {
		t.Fatal("repository returned shared option map")
	}

	loaded.Spec.Runtime.MaxBatchRecords = 256
	loaded.UpdatedAt = loaded.UpdatedAt.Add(time.Minute)
	updated, err := repository.Update(ctx, loaded, 1)
	if err != nil || updated.Version != 2 {
		t.Fatalf("update: version=%d err=%v", updated.Version, err)
	}
	if _, err := repository.Update(ctx, loaded, 1); !errors.Is(err, job.ErrConflict) {
		t.Fatalf("expected update conflict, got %v", err)
	}
	if err := repository.Delete(ctx, created.Key, 1); !errors.Is(err, job.ErrConflict) {
		t.Fatalf("expected delete conflict, got %v", err)
	}
	if err := repository.Delete(ctx, created.Key, 2); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repository.Get(ctx, created.Key); !errors.Is(err, job.ErrNotFound) {
		t.Fatalf("expected missing job, got %v", err)
	}
}

func TestRepositoryListsStablePagesWithinNamespace(t *testing.T) {
	ctx := context.Background()
	repository := memory.New()
	for _, name := range []string{"charlie", "alpha", "bravo"} {
		if _, err := repository.Create(ctx, testJob(t, name)); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	other := testJob(t, "other")
	other.Key.Namespace = "another"
	if _, err := repository.Create(ctx, other); err != nil {
		t.Fatalf("create other namespace: %v", err)
	}

	page, err := repository.List(ctx, "default", job.Page{Number: 2, Size: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Total != 3 || len(page.Jobs) != 1 || page.Jobs[0].Key.Name != "charlie" {
		t.Fatalf("unexpected page: %+v", page)
	}
}

func TestRepositoryRejectsInvalidPage(t *testing.T) {
	if _, err := memory.New().List(context.Background(), "default", job.Page{}); err == nil {
		t.Fatal("expected invalid page failure")
	}
}

func testJob(t *testing.T, name string) job.Job {
	t.Helper()
	spec := job.Spec{
		Source:   job.ConnectorSpec{Connector: "mysql-cdc", Options: map[string]string{"table": "orders"}},
		Sink:     job.ConnectorSpec{Connector: "jdbc", Options: map[string]string{"table": "orders"}},
		Delivery: job.DeliverySpec{Guarantee: job.DeliveryExactlyOnce},
		Runtime:  job.RuntimeSpec{MaxBatchRecords: 128},
	}
	created, err := job.New(
		job.Key{Namespace: "default", Name: name},
		uuid.NewString(),
		spec,
		time.Date(2026, 8, 5, 5, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("new job: %v", err)
	}
	return created
}
