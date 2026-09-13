//go:build integration

package controller_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	syncv1 "io.astrasync/control-plane/controller/api/v1"
	synccontroller "io.astrasync/control-plane/controller/internal/controller"
	controllerobservability "io.astrasync/control-plane/controller/internal/metrics"
	jobmodel "io.astrasync/control-plane/job"
	jobmemory "io.astrasync/control-plane/job/memory"
)

const reconcileDurationMetric = "controller_job_controller_reconcile_duration_seconds"

func TestControllerManagerReconcileEmitsTenantMetricsIntegration(t *testing.T) {
	harness := startEnvtestHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)

	registry := prometheus.NewRegistry()
	recorder, err := controllerobservability.NewRecorder(registry)
	if err != nil {
		t.Fatalf("create controller metrics recorder: %v", err)
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
	repository := jobmemory.New()
	reconciler := &synccontroller.SyncJobReconciler{
		Client:                manager.GetClient(),
		Scheme:                manager.GetScheme(),
		Clock:                 time.Now,
		Jobs:                  repository,
		StatusRefreshInterval: time.Hour,
		Metrics:               recorder,
	}
	if err := reconciler.SetupWithManager(manager, recorder); err != nil {
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

	job := newIntegrationSyncJob("manager-metrics", tenantID)
	if err := harness.client.Create(ctx, job); err != nil {
		t.Fatalf("create SyncJob: %v", err)
	}
	waitForReconcileMetric(t, ctx, registry, tenantID)

	key, err := jobmodel.NewKey(envtestNamespace, job.Name)
	if err != nil {
		t.Fatalf("create Job key: %v", err)
	}
	stored, err := repository.Get(ctx, key)
	if err != nil {
		t.Fatalf("read reconciled Job: %v", err)
	}
	if stored.Status.State != jobmodel.StateCreated || stored.Status.Desired != jobmodel.DesiredStopped {
		t.Fatalf("reconciled Job status = %+v, want created/stopped", stored.Status)
	}

	var projected syncv1.SyncJob
	if err := harness.client.Get(ctx, types.NamespacedName{
		Namespace: envtestNamespace,
		Name:      job.Name,
	}, &projected); err != nil {
		t.Fatalf("read projected SyncJob: %v", err)
	}
	if projected.Status.State != jobmodel.StateCreated {
		t.Fatalf("projected SyncJob state = %q, want %q", projected.Status.State, jobmodel.StateCreated)
	}
	if !containsString(projected.Finalizers, syncJobFinalizer) {
		t.Fatalf("projected SyncJob finalizers = %v, want %q", projected.Finalizers, syncJobFinalizer)
	}
}

func waitForReconcileMetric(
	t *testing.T,
	ctx context.Context,
	gatherer prometheus.Gatherer,
	tenant string,
) {
	t.Helper()
	for {
		if reconcileSuccessSamples(t, gatherer, tenant) > 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for reconcile metric: %v", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func reconcileSuccessSamples(t *testing.T, gatherer prometheus.Gatherer, tenant string) uint64 {
	t.Helper()
	families, err := gatherer.Gather()
	if err != nil {
		t.Fatalf("gather controller metrics: %v", err)
	}
	for _, family := range families {
		if family.GetName() != reconcileDurationMetric {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := make(map[string]string, len(metric.GetLabel()))
			for _, label := range metric.GetLabel() {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["tenant_id"] == tenant && labels["outcome"] == "success" {
				return metric.GetHistogram().GetSampleCount()
			}
		}
	}
	return 0
}
