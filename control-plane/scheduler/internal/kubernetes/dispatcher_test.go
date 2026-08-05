package kubernetes

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"io.astrasync/control-plane/job"
	"io.astrasync/control-plane/scheduler/internal/dispatch"
	"io.astrasync/control-plane/scheduler/internal/scheduler"
)

func TestDispatcherMaterializesOneIdempotentExecutionGroupAndObservesCompletion(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	dispatcher, err := New(client, testConfig())
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}
	candidate := testJob(t)
	identity := dispatch.Identity{JobUID: candidate.UID, Epoch: 4}

	observation, err := dispatcher.Reconcile(context.Background(), candidate, identity.Epoch)
	if err != nil || observation.State != scheduler.ObservationPending {
		t.Fatalf("first reconcile: observation=%+v err=%v", observation, err)
	}
	base, _ := resourceBase(identity)
	workers, err := client.AppsV1().StatefulSets("jobs").Get(context.Background(), base+"-worker", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get Worker StatefulSet: %v", err)
	}
	workers.Status.ReadyReplicas = 2
	if _, err := client.AppsV1().StatefulSets("jobs").UpdateStatus(
		context.Background(), workers, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update Worker readiness: %v", err)
	}

	observation, err = dispatcher.Reconcile(context.Background(), candidate, identity.Epoch)
	if err != nil || observation.State != scheduler.ObservationPending {
		t.Fatalf("Coordinator creation reconcile: observation=%+v err=%v", observation, err)
	}
	coordinator, err := client.BatchV1().Jobs("jobs").Get(
		context.Background(), base+"-coordinator", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get Coordinator Job: %v", err)
	}
	if coordinator.Labels[labelJobUID] != candidate.UID || coordinator.Labels[labelEpoch] != "4" {
		t.Fatalf("Coordinator identity labels were not fenced: %v", coordinator.Labels)
	}
	environment := make(map[string]string)
	for _, variable := range coordinator.Spec.Template.Spec.Containers[0].Env {
		environment[variable.Name] = variable.Value
	}
	if environment["ASTRASYNC_COORDINATOR_EXECUTION_EPOCH"] != "4" {
		t.Fatalf("Coordinator epoch environment missing: %v", environment)
	}
	if environment["ASTRASYNC_COORDINATOR_WORKERS"] == "" {
		t.Fatal("Coordinator Worker endpoints are empty")
	}

	coordinator.Status.Active = 1
	if _, err := client.BatchV1().Jobs("jobs").UpdateStatus(
		context.Background(), coordinator, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update Coordinator active status: %v", err)
	}
	observation, err = dispatcher.Reconcile(context.Background(), candidate, identity.Epoch)
	if err != nil || observation.State != scheduler.ObservationRunning {
		t.Fatalf("running reconcile: observation=%+v err=%v", observation, err)
	}

	coordinator.Status.Active = 0
	coordinator.Status.Succeeded = 1
	coordinator.Status.Conditions = []batchv1.JobCondition{{
		Type: batchv1.JobComplete, Status: corev1.ConditionTrue, LastTransitionTime: metav1.Now(),
	}}
	if _, err := client.BatchV1().Jobs("jobs").UpdateStatus(
		context.Background(), coordinator, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update Coordinator completion: %v", err)
	}
	observation, err = dispatcher.Reconcile(context.Background(), candidate, identity.Epoch)
	if err != nil || observation.State != scheduler.ObservationSucceeded {
		t.Fatalf("completed reconcile: observation=%+v err=%v", observation, err)
	}
	if _, err := client.CoreV1().Secrets("jobs").Get(
		context.Background(), base+"-spec", metav1.GetOptions{}); err == nil {
		t.Fatal("JobSpec Secret was not cleaned after terminal execution")
	}
	if _, err := client.AppsV1().StatefulSets("jobs").Get(
		context.Background(), base+"-worker", metav1.GetOptions{}); err == nil {
		t.Fatal("Worker StatefulSet was not cleaned after terminal execution")
	}
	if _, err := client.BatchV1().Jobs("jobs").Get(
		context.Background(), base+"-coordinator", metav1.GetOptions{}); err != nil {
		t.Fatalf("Coordinator Job should remain for TTL/log inspection: %v", err)
	}
}

func TestDispatcherStopDeletesEveryResourceBeforeReportingCanceled(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	dispatcher, err := New(client, testConfig())
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}
	candidate := testJob(t)
	identity := dispatch.Identity{JobUID: candidate.UID, Epoch: 1}
	if _, err := dispatcher.Reconcile(context.Background(), candidate, identity.Epoch); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	base, _ := resourceBase(identity)
	completed, err := dispatcher.Stop(context.Background(), identity)
	if err != nil || !completed {
		t.Fatalf("stop: completed=%v err=%v", completed, err)
	}
	checks := []func() error{
		func() error {
			_, err := client.CoreV1().Secrets("jobs").Get(context.Background(), base+"-spec", metav1.GetOptions{})
			return err
		},
		func() error {
			_, err := client.CoreV1().Services("jobs").Get(context.Background(), base+"-worker", metav1.GetOptions{})
			return err
		},
		func() error {
			_, err := client.AppsV1().StatefulSets("jobs").Get(context.Background(), base+"-worker", metav1.GetOptions{})
			return err
		},
	}
	for _, check := range checks {
		if err := check(); err == nil {
			t.Fatal("stop left an execution resource behind")
		}
	}
	foregroundDeletes := 0
	for _, action := range client.Actions() {
		if action.GetVerb() != "delete" ||
			(action.GetResource().Resource != "jobs" && action.GetResource().Resource != "statefulsets") {
			continue
		}
		deleteAction := action.(k8stesting.DeleteAction)
		policy := deleteAction.GetDeleteOptions().PropagationPolicy
		if policy == nil || *policy != metav1.DeletePropagationForeground {
			t.Fatalf("%s was not deleted in the foreground", action.GetResource().Resource)
		}
		foregroundDeletes++
	}
	if foregroundDeletes != 2 {
		t.Fatalf("expected foreground deletion for Coordinator and Workers, got %d", foregroundDeletes)
	}
}

func TestDispatcherRejectsAConcurrentJobSpecSecretCollision(t *testing.T) {
	candidate := testJob(t)
	identity := dispatch.Identity{JobUID: candidate.UID, Epoch: 1}
	base, _ := resourceBase(identity)
	secretName := base + "-spec"
	client := k8sfake.NewSimpleClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name: secretName, Namespace: "jobs",
	}})
	firstSecretGet := true
	client.Fake.PrependReactor("get", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if firstSecretGet && action.(k8stesting.GetAction).GetName() == secretName {
			firstSecretGet = false
			return true, nil, apierrors.NewNotFound(
				schema.GroupResource{Resource: "secrets"}, secretName)
		}
		return false, nil, nil
	})
	dispatcher, err := New(client, testConfig())
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}

	_, err = dispatcher.Reconcile(context.Background(), candidate, identity.Epoch)
	var permanent *scheduler.PermanentError
	if !errors.As(err, &permanent) {
		t.Fatalf("expected permanent concurrent collision, got %v", err)
	}
	services, err := client.CoreV1().Services("jobs").List(context.Background(), metav1.ListOptions{})
	if err != nil || len(services.Items) != 0 {
		t.Fatalf("collision created Worker Services: items=%d err=%v", len(services.Items), err)
	}
	workers, err := client.AppsV1().StatefulSets("jobs").List(context.Background(), metav1.ListOptions{})
	if err != nil || len(workers.Items) != 0 {
		t.Fatalf("collision created Worker StatefulSets: items=%d err=%v", len(workers.Items), err)
	}
}

func TestDispatcherRejectsUnresolvedConnectionReferencesBeforeCreatingSecrets(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	dispatcher, err := New(client, testConfig())
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}
	candidate := testJob(t)
	candidate.Spec.Source.ConnectionRef = "warehouse-secret"
	_, err = dispatcher.Reconcile(context.Background(), candidate, 1)
	var permanent *scheduler.PermanentError
	if !errors.As(err, &permanent) {
		t.Fatalf("expected permanent dispatch rejection, got %v", err)
	}
	if actions := client.Actions(); len(actions) != 0 {
		t.Fatalf("created Kubernetes resources before permanent validation: %v", actions)
	}
}

func testConfig() Config {
	ttl := int32(3600)
	return Config{
		Namespace: "jobs", ServiceAccount: "astrasync", CoordinatorImage: "coordinator:test",
		WorkerImage: "worker:test", ImagePullPolicy: corev1.PullIfNotPresent, ProgressClaim: "progress",
		WorkerReplicas: 2, WorkerPort: 50051, WorkerTimeoutMillis: 30000,
		CoordinatorMaxInFlightTasks: 1, WorkerMaxInFlightBatches: 1, WorkerMaxConcurrentTasks: 1,
		WorkerMaxConnections: 8, CoordinatorBackoffLimit: 2, TTLSecondsAfterFinished: &ttl,
	}
}

func testJob(t *testing.T) job.Job {
	t.Helper()
	spec := job.Spec{
		Source: job.ConnectorSpec{Connector: "jdbc", Options: map[string]string{
			"url": "jdbc:postgresql://db/source", "table": "source_data",
		}},
		Sink: job.ConnectorSpec{Connector: "jdbc", Options: map[string]string{
			"url": "jdbc:postgresql://db/target", "table": "target_data",
		}},
		Delivery: job.DeliverySpec{Guarantee: job.DeliveryAtLeastOnce},
		Runtime:  job.RuntimeSpec{MaxBatchRecords: 128},
	}
	candidate, err := job.New(
		job.Key{Namespace: "logical", Name: "orders"}, uuid.NewString(), spec, time.Now().UTC())
	if err != nil {
		t.Fatalf("new test job: %v", err)
	}
	return candidate
}

func TestJobDocumentUsesStableRuntimeIdentity(t *testing.T) {
	candidate := testJob(t)
	encoded, _, err := jobDocument(candidate)
	if err != nil {
		t.Fatalf("encode job: %v", err)
	}
	var document struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Spec job.Spec `json:"spec"`
	}
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("decode generated JobSpec: %v", err)
	}
	if document.Metadata.Name != runtimeID(candidate.UID) {
		t.Fatalf("runtime identity is not stable: %q", document.Metadata.Name)
	}
	if document.Spec.Runtime.MaxBatchRecords != candidate.Spec.Runtime.MaxBatchRecords {
		t.Fatalf("runtime configuration changed: %+v", document.Spec.Runtime)
	}
}
