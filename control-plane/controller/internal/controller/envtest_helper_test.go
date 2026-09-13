//go:build integration

package controller_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	syncv1 "io.astrasync/control-plane/controller/api/v1"
	jobmodel "io.astrasync/control-plane/job"
)

const (
	envtestNamespace = "controller-envtest"
	tenantID         = "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"
)

func startEnvtest(t *testing.T) client.Client {
	t.Helper()

	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Fatal("KUBEBUILDER_ASSETS must point to setup-envtest binaries")
	}

	testEnvironment := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join(repositoryRoot(t), "deployment", "operator", "config", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,
	}

	config, err := testEnvironment.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	t.Cleanup(func() {
		if err := testEnvironment.Stop(); err != nil {
			t.Errorf("stop envtest: %v", err)
		}
	})

	scheme := k8sruntime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("register core scheme: %v", err)
	}
	if err := syncv1.AddToScheme(scheme); err != nil {
		t.Fatalf("register SyncJob scheme: %v", err)
	}
	if err := admissionregistrationv1.AddToScheme(scheme); err != nil {
		t.Fatalf("register admissionregistration scheme: %v", err)
	}

	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("create envtest client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: envtestNamespace}}
	if err := k8sClient.Create(ctx, namespace); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("create envtest namespace: %v", err)
	}
	installAdmissionPolicy(t, k8sClient)

	return k8sClient
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve envtest helper path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", ".."))
}

func installAdmissionPolicy(t *testing.T, k8sClient client.Client) {
	t.Helper()

	manifestPath := filepath.Join(
		repositoryRoot(t),
		"deployment",
		"operator",
		"config",
		"admission",
		"syncjob-tenant-id-validating-admission-policy.yaml",
	)
	manifest, err := os.Open(manifestPath)
	if err != nil {
		t.Fatalf("open admission policy manifest: %v", err)
	}
	t.Cleanup(func() {
		_ = manifest.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	decoder := utilyaml.NewYAMLOrJSONDecoder(manifest, 4096)
	for {
		object := &unstructured.Unstructured{}
		err := decoder.Decode(object)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("decode admission policy manifest: %v", err)
		}
		if len(object.Object) == 0 {
			continue
		}
		if err := k8sClient.Create(ctx, object); err != nil && !apierrors.IsAlreadyExists(err) {
			t.Fatalf("create %s/%s: %v", object.GetKind(), object.GetName(), err)
		}
	}

}

func newIntegrationSyncJob(name, tenant string) *syncv1.SyncJob {
	labels := map[string]string(nil)
	if tenant != "" {
		labels = map[string]string{syncv1.TenantIDLabel: tenant}
	}
	return &syncv1.SyncJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: envtestNamespace,
			Labels:    labels,
		},
		Spec: syncv1.SyncJobSpec{
			Source: jobmodel.ConnectorSpec{Connector: "mysql-cdc"},
			Sink:   jobmodel.ConnectorSpec{Connector: "jdbc"},
			Delivery: jobmodel.DeliverySpec{
				Guarantee: jobmodel.DeliveryAtLeastOnce,
			},
			Runtime: jobmodel.RuntimeSpec{MaxBatchRecords: 512},
			State:   jobmodel.DesiredStopped,
		},
	}
}
