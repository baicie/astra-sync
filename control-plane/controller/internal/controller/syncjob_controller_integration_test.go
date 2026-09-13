//go:build integration

package controller_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	syncv1 "io.astrasync/control-plane/controller/api/v1"
	jobmodel "io.astrasync/control-plane/job"
)

const syncJobFinalizer = "sync.astrasync.io/control-plane-finalizer"

func TestSyncJobCRDValidationIntegration(t *testing.T) {
	k8sClient := startEnvtest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	tests := []struct {
		name       string
		valid      bool
		objectName string
		mutate     func(*syncv1.SyncJob)
	}{
		{
			name:       "missing_delivery_guarantee_is_rejected",
			objectName: "missing-delivery",
			mutate: func(job *syncv1.SyncJob) {
				job.Spec.Delivery.Guarantee = ""
			},
		},
		{
			name:       "malformed_connector_name_is_rejected",
			objectName: "malformed-connector",
			mutate: func(job *syncv1.SyncJob) {
				job.Spec.Source.Connector = "invalid connector"
			},
		},
		{
			name:       "valid_spec_is_accepted",
			valid:      true,
			objectName: "valid-spec",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			job := newIntegrationSyncJob(test.objectName, tenantID)
			if test.mutate != nil {
				test.mutate(job)
			}
			err := k8sClient.Create(ctx, job)
			if test.valid {
				if err != nil {
					t.Fatalf("create valid SyncJob: %v", err)
				}
				return
			}
			if !apierrors.IsInvalid(err) {
				t.Fatalf("expected CRD validation error, got %v", err)
			}
		})
	}
}

func TestSyncJobTenantLabelAdmissionIntegration(t *testing.T) {
	k8sClient := startEnvtest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	waitForTenantLabelAdmissionPolicy(t, ctx, k8sClient)

	tests := []struct {
		name       string
		tenant     string
		valid      bool
		objectName string
	}{
		{
			name:       "missing_tenant_label_is_denied",
			objectName: "admission-missing-tenant",
		},
		{
			name:       "malformed_tenant_label_is_denied",
			tenant:     "ALICE@acme.example",
			objectName: "admission-malformed-tenant",
		},
		{
			name:       "canonical_tenant_label_is_accepted",
			tenant:     tenantID,
			valid:      true,
			objectName: "admission-canonical-tenant",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			job := newIntegrationSyncJob(test.objectName, test.tenant)
			err := k8sClient.Create(ctx, job)
			if test.valid {
				if err != nil {
					t.Fatalf("create valid SyncJob: %v", err)
				}
				return
			}
			if !apierrors.IsInvalid(err) && !apierrors.IsForbidden(err) {
				t.Fatalf("expected tenant-label admission denial, got %v", err)
			}
		})
	}
}

func waitForTenantLabelAdmissionPolicy(t *testing.T, ctx context.Context, k8sClient client.Client) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for attempt := 0; time.Now().Before(deadline); attempt++ {
		probe := newIntegrationSyncJob(fmt.Sprintf("admission-probe-%d", attempt), "")
		err := k8sClient.Create(ctx, probe)
		if isAdmissionDenial(err) {
			return
		}
		if err != nil {
			t.Fatalf("create admission probe: %v", err)
		}
		if err := k8sClient.Delete(ctx, probe); err != nil && !apierrors.IsNotFound(err) {
			t.Fatalf("delete admission probe: %v", err)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for tenant-label admission policy: %v", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("tenant-label admission policy did not become active")
}

func isAdmissionDenial(err error) bool {
	return apierrors.IsInvalid(err) || apierrors.IsForbidden(err)
}

func TestSyncJobStatusSubresourceIntegration(t *testing.T) {
	k8sClient := startEnvtest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	job := newIntegrationSyncJob("status-subresource", tenantID)
	if err := k8sClient.Create(ctx, job); err != nil {
		t.Fatalf("create SyncJob: %v", err)
	}
	createdGeneration := job.Generation

	job.Status.State = jobmodel.StateRunning
	if err := k8sClient.Status().Update(ctx, job); err != nil {
		t.Fatalf("update status subresource: %v", err)
	}

	var afterStatus syncv1.SyncJob
	key := types.NamespacedName{Namespace: envtestNamespace, Name: job.Name}
	if err := k8sClient.Get(ctx, key, &afterStatus); err != nil {
		t.Fatalf("get after status update: %v", err)
	}
	if afterStatus.Status.State != jobmodel.StateRunning {
		t.Fatalf("status state = %q, want %q", afterStatus.Status.State, jobmodel.StateRunning)
	}
	if afterStatus.Generation != createdGeneration {
		t.Fatalf("status update changed generation from %d to %d", createdGeneration, afterStatus.Generation)
	}

	afterStatus.Spec.Runtime.MaxBatchRecords = 2048
	afterStatus.Status.State = jobmodel.StateFailed
	if err := k8sClient.Update(ctx, &afterStatus); err != nil {
		t.Fatalf("update spec: %v", err)
	}

	var afterSpec syncv1.SyncJob
	if err := k8sClient.Get(ctx, key, &afterSpec); err != nil {
		t.Fatalf("get after spec update: %v", err)
	}
	if afterSpec.Spec.Runtime.MaxBatchRecords != 2048 {
		t.Fatalf("maxBatchRecords = %d, want 2048", afterSpec.Spec.Runtime.MaxBatchRecords)
	}
	if afterSpec.Status.State != jobmodel.StateRunning {
		t.Fatalf("spec update changed status to %q", afterSpec.Status.State)
	}
	if afterSpec.Generation <= createdGeneration {
		t.Fatalf("spec update did not advance generation from %d", createdGeneration)
	}
}

func TestSyncJobOptimisticLockingIntegration(t *testing.T) {
	k8sClient := startEnvtest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	job := newIntegrationSyncJob("optimistic-locking", tenantID)
	if err := k8sClient.Create(ctx, job); err != nil {
		t.Fatalf("create SyncJob: %v", err)
	}

	key := types.NamespacedName{Namespace: envtestNamespace, Name: job.Name}
	var first syncv1.SyncJob
	if err := k8sClient.Get(ctx, key, &first); err != nil {
		t.Fatalf("get first resource: %v", err)
	}
	stale := first.DeepCopy()

	first.Spec.Runtime.MaxBatchRecords = 1024
	if err := k8sClient.Update(ctx, &first); err != nil {
		t.Fatalf("first update: %v", err)
	}

	stale.Spec.Runtime.MaxBatchRecords = 2048
	err := k8sClient.Update(ctx, stale)
	if !apierrors.IsConflict(err) {
		t.Fatalf("expected resourceVersion conflict, got %v", err)
	}
}

func TestSyncJobFinalizerBlocksDeletionIntegration(t *testing.T) {
	k8sClient := startEnvtest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	job := newIntegrationSyncJob("finalizer-cleanup", tenantID)
	job.Finalizers = []string{syncJobFinalizer}
	if err := k8sClient.Create(ctx, job); err != nil {
		t.Fatalf("create SyncJob: %v", err)
	}

	if err := k8sClient.Delete(ctx, job); err != nil {
		t.Fatalf("delete SyncJob: %v", err)
	}

	var deleting syncv1.SyncJob
	key := types.NamespacedName{Namespace: envtestNamespace, Name: job.Name}
	if err := k8sClient.Get(ctx, key, &deleting); err != nil {
		t.Fatalf("get deleting SyncJob: %v", err)
	}
	if deleting.DeletionTimestamp == nil {
		t.Fatal("expected deletionTimestamp to be set")
	}
	if !containsString(deleting.Finalizers, syncJobFinalizer) {
		t.Fatalf("finalizer %q missing while deletion is pending", syncJobFinalizer)
	}

	deleting.Finalizers = nil
	if err := k8sClient.Update(ctx, &deleting); err != nil {
		t.Fatalf("remove finalizer: %v", err)
	}

	deleted := &syncv1.SyncJob{}
	err := k8sClient.Get(ctx, key, deleted)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected SyncJob deletion after finalizer removal, got %v", err)
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

var _ client.Object = (*syncv1.SyncJob)(nil)
