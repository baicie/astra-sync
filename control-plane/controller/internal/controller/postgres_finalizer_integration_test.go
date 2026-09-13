//go:build integration

package controller_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	syncv1 "io.astrasync/control-plane/controller/api/v1"
	synccontroller "io.astrasync/control-plane/controller/internal/controller"
	jobmodel "io.astrasync/control-plane/job"
	jobpostgres "io.astrasync/control-plane/job/postgres"
)

func TestControllerManagerFinalizerCleanupAcrossPostgresIntegration(t *testing.T) {
	dataSourceName := os.Getenv("ASTRASYNC_TEST_POSTGRES_URL")
	if dataSourceName == "" {
		t.Fatal("ASTRASYNC_TEST_POSTGRES_URL must point to PostgreSQL for controller integration tests")
	}
	harness := startEnvtestHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	repository, err := jobpostgres.Open(ctx, dataSourceName)
	if err != nil {
		t.Fatalf("open PostgreSQL repository: %v", err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Errorf("close PostgreSQL repository: %v", err)
		}
	})
	if err := repository.Migrate(ctx); err != nil {
		t.Fatalf("migrate PostgreSQL repository: %v", err)
	}

	manager, err := ctrl.NewManager(harness.config, ctrl.Options{
		Scheme:                 harness.scheme,
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
		LeaderElection:         false,
	})
	if err != nil {
		t.Fatalf("create controller manager: %v", err)
	}
	reconciler := &synccontroller.SyncJobReconciler{
		Client:                manager.GetClient(),
		Scheme:                manager.GetScheme(),
		Clock:                 time.Now,
		Jobs:                  repository,
		StatusRefreshInterval: 100 * time.Millisecond,
	}
	if err := reconciler.SetupWithManager(manager, nil); err != nil {
		t.Fatalf("setup controller with manager: %v", err)
	}

	managerCtx, stopManager := context.WithCancel(context.Background())
	managerDone := make(chan error, 1)
	go func() {
		managerDone <- manager.Start(managerCtx)
	}()
	t.Cleanup(func() {
		stopManager()
		select {
		case err := <-managerDone:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("stop controller manager: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("controller manager did not stop within 10s")
		}
	})

	syncCtx, stopSync := context.WithTimeout(ctx, 15*time.Second)
	defer stopSync()
	if !manager.GetCache().WaitForCacheSync(syncCtx) {
		t.Fatal("controller manager cache did not sync")
	}

	resource := newIntegrationSyncJob("pg-finalizer-"+uuid.NewString(), tenantID)
	resource.Spec.State = jobmodel.DesiredRunning
	if err := harness.client.Create(ctx, resource); err != nil {
		t.Fatalf("create SyncJob: %v", err)
	}
	key, err := jobmodel.NewKey(resource.Namespace, resource.Name)
	if err != nil {
		t.Fatalf("create Job key: %v", err)
	}

	waitForCondition(t, ctx, "PostgreSQL Job initializing", func(ctx context.Context) (bool, error) {
		stored, err := repository.Get(ctx, key)
		if errors.Is(err, jobmodel.ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return stored.Status.State == jobmodel.StateInitializing, nil
	})
	waitForCondition(t, ctx, "projected SyncJob initializing", func(ctx context.Context) (bool, error) {
		projected := &syncv1.SyncJob{}
		if err := harness.client.Get(ctx, types.NamespacedName{
			Namespace: resource.Namespace,
			Name:      resource.Name,
		}, projected); err != nil {
			return false, err
		}
		return projected.Status.State == jobmodel.StateInitializing &&
			containsString(projected.Finalizers, syncJobFinalizer), nil
	})

	stored, err := repository.Get(ctx, key)
	if err != nil {
		t.Fatalf("read initializing Job: %v", err)
	}
	running, changed, err := stored.Advance(stored.Status.Epoch, jobmodel.StateRunning, nil, time.Now())
	if err != nil || !changed {
		t.Fatalf("advance Job to running: changed=%v err=%v", changed, err)
	}
	if _, err := repository.Update(ctx, running, stored.Version); err != nil {
		t.Fatalf("persist running Job: %v", err)
	}
	waitForCondition(t, ctx, "PostgreSQL Job running", func(ctx context.Context) (bool, error) {
		stored, err := repository.Get(ctx, key)
		if err != nil {
			return false, err
		}
		return stored.Status.State == jobmodel.StateRunning, nil
	})

	if err := harness.client.Delete(ctx, resource); err != nil {
		t.Fatalf("delete SyncJob: %v", err)
	}
	waitForCondition(t, ctx, "active Job cancellation", func(ctx context.Context) (bool, error) {
		stored, err := repository.Get(ctx, key)
		if err != nil {
			return false, err
		}
		projected := &syncv1.SyncJob{}
		if err := harness.client.Get(ctx, types.NamespacedName{
			Namespace: resource.Namespace,
			Name:      resource.Name,
		}, projected); err != nil {
			return false, err
		}
		return stored.Status.State == jobmodel.StateCanceling &&
			projected.DeletionTimestamp != nil &&
			containsString(projected.Finalizers, syncJobFinalizer), nil
	})

	stored, err = repository.Get(ctx, key)
	if err != nil {
		t.Fatalf("read canceling Job: %v", err)
	}
	canceled, changed, err := stored.Advance(stored.Status.Epoch, jobmodel.StateCanceled, nil, time.Now())
	if err != nil || !changed {
		t.Fatalf("advance Job to canceled: changed=%v err=%v", changed, err)
	}
	if _, err := repository.Update(ctx, canceled, stored.Version); err != nil {
		t.Fatalf("persist canceled Job: %v", err)
	}

	waitForCondition(t, ctx, "Kubernetes and PostgreSQL cleanup", func(ctx context.Context) (bool, error) {
		_, postgresErr := repository.Get(ctx, key)
		if postgresErr != nil && !errors.Is(postgresErr, jobmodel.ErrNotFound) {
			return false, postgresErr
		}
		projected := &syncv1.SyncJob{}
		kubeErr := harness.client.Get(ctx, types.NamespacedName{
			Namespace: resource.Namespace,
			Name:      resource.Name,
		}, projected)
		if kubeErr != nil && !apierrors.IsNotFound(kubeErr) {
			return false, kubeErr
		}
		return errors.Is(postgresErr, jobmodel.ErrNotFound) && apierrors.IsNotFound(kubeErr), nil
	})
}

func waitForCondition(
	t *testing.T,
	ctx context.Context,
	description string,
	condition func(context.Context) (bool, error),
) {
	t.Helper()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		matched, err := condition(ctx)
		if err != nil {
			t.Fatalf("wait for %s: %v", description, err)
		}
		if matched {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for %s: %v", description, ctx.Err())
		case <-ticker.C:
		}
	}
}
