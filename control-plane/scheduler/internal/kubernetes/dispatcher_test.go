package kubernetes

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	appsv1 "k8s.io/api/apps/v1"
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
	"io.astrasync/control-plane/scheduler/internal/materialization"
	"io.astrasync/control-plane/scheduler/internal/scheduler"
)

func TestDispatcherLaunchesOnlyAfterCredentialMaterializationAndMountsReadOnlyEnvelope(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	candidate := testJob(t)
	candidate.Spec.Source.ConnectionRef = "orders-db"
	identity := dispatch.Identity{JobUID: candidate.UID, Epoch: 4}
	credentialName, _ := materialization.CredentialSecretName(identity)
	credentials := &fakeCredentialMaterializer{result: materialization.Result{
		Required: true, SecretName: credentialName,
		IdentityFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		CompilerRevision:    "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Roles:               []materialization.Role{materialization.RoleSource},
	}}
	configuration := testConfig()
	configuration.ConnectionMaterializationEnabled = true
	dispatcher, err := NewWithCredentialMaterializer(client, configuration, credentials)
	if err != nil {
		t.Fatalf("new credential-aware dispatcher: %v", err)
	}
	record := testRecord(identity)
	observation, err := dispatcher.Reconcile(context.Background(), candidate, record)
	if err != nil || observation.State != scheduler.ObservationPending || credentials.ensureCalls != 1 {
		t.Fatalf("credential-aware reconcile: observation=%+v calls=%d err=%v",
			observation, credentials.ensureCalls, err)
	}
	base, _ := resourceBase(identity)
	specSecret, err := client.CoreV1().Secrets("jobs").Get(
		context.Background(), base+"-spec", metav1.GetOptions{})
	if err != nil || string(specSecret.Data["job.yaml"]) == "" ||
		strings.Contains(string(specSecret.Data["job.yaml"]), "orders-db") {
		t.Fatalf("JobSpec Secret retained a Connection reference: secret=%+v err=%v", specSecret, err)
	}
	workers, err := client.AppsV1().StatefulSets("jobs").Get(
		context.Background(), base+"-worker", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("read credential-aware workers: %v", err)
	}
	container := workers.Spec.Template.Spec.Containers[0]
	if !hasEnvironment(container.Env, "ASTRASYNC_RUNTIME_CREDENTIALS", materialization.CredentialMountPath()) ||
		!hasEnvironment(container.Env, "ASTRASYNC_EXECUTION_JOB_UID", candidate.UID) ||
		!hasEnvironment(container.Env, "ASTRASYNC_EXECUTION_EPOCH", "4") ||
		!hasEnvironment(container.Env, "ASTRASYNC_EXECUTION_COMPILER_REVISION", credentials.result.CompilerRevision) ||
		!hasReadOnlyMount(container.VolumeMounts, "runtime-credentials", materialization.CredentialMountPath()) ||
		!hasSecretVolume(workers.Spec.Template.Spec.Volumes, "runtime-credentials", credentialName) {
		t.Fatalf("runtime credential envelope was not mounted safely: pod=%+v", workers.Spec.Template.Spec)
	}
}

func TestDispatcherDoesNotCreateRuntimeResourcesWhenReceiptCommitFails(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	candidate := testJob(t)
	candidate.Spec.Source.ConnectionRef = "orders-db"
	identity := dispatch.Identity{JobUID: candidate.UID, Epoch: 4}
	configuration := testConfig()
	configuration.ConnectionMaterializationEnabled = true
	dispatcher, err := NewWithCredentialMaterializer(client, configuration, &fakeCredentialMaterializer{
		err: errors.New("receipt persistence unavailable"),
	})
	if err != nil {
		t.Fatalf("new credential-aware dispatcher: %v", err)
	}
	if _, err := dispatcher.Reconcile(context.Background(), candidate, testRecord(identity)); err == nil {
		t.Fatal("expected materialization failure")
	}
	base, _ := resourceBase(identity)
	if _, err := client.CoreV1().Secrets("jobs").Get(
		context.Background(), base+"-spec", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("JobSpec Secret was created before credential receipt commit: %v", err)
	}
	if _, err := client.AppsV1().StatefulSets("jobs").Get(
		context.Background(), base+"-worker", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("Workers were created before credential receipt commit: %v", err)
	}
}

type fakeCredentialMaterializer struct {
	result       materialization.Result
	err          error
	ensureCalls  int
	cleanupCalls int
}

func (m *fakeCredentialMaterializer) Ensure(
	_ context.Context, _ dispatch.Record,
) (materialization.Result, error) {
	m.ensureCalls++
	return m.result, m.err
}

func (m *fakeCredentialMaterializer) Cleanup(_ context.Context, _ dispatch.Identity) error {
	m.cleanupCalls++
	return m.err
}

func hasEnvironment(values []corev1.EnvVar, name, expected string) bool {
	for _, value := range values {
		if value.Name == name && value.Value == expected {
			return true
		}
	}
	return false
}

func hasReadOnlyMount(values []corev1.VolumeMount, name, path string) bool {
	for _, value := range values {
		if value.Name == name && value.MountPath == path && value.ReadOnly {
			return true
		}
	}
	return false
}

func hasSecretVolume(values []corev1.Volume, name, secretName string) bool {
	for _, value := range values {
		if value.Name == name && value.Secret != nil && value.Secret.SecretName == secretName {
			return true
		}
	}
	return false
}

func TestDispatcherMaterializesOneIdempotentExecutionGroupAndObservesCompletion(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	dispatcher, err := New(client, testConfig())
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}
	candidate := testJob(t)
	identity := dispatch.Identity{JobUID: candidate.UID, Epoch: 4}
	record := testRecord(identity)

	observation, err := dispatcher.Reconcile(context.Background(), candidate, record)
	if err != nil || observation.State != scheduler.ObservationPending {
		t.Fatalf("first reconcile: observation=%+v err=%v", observation, err)
	}
	base, _ := resourceBase(identity)
	secret, err := client.CoreV1().Secrets("jobs").Get(context.Background(), base+"-spec", metav1.GetOptions{})
	if err != nil || string(secret.Data[heartbeatTokenKey]) != record.HeartbeatToken {
		t.Fatalf("execution heartbeat token was not materialized: secret=%+v err=%v", secret, err)
	}
	workers, err := client.AppsV1().StatefulSets("jobs").Get(context.Background(), base+"-worker", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get Worker StatefulSet: %v", err)
	}
	workers.Status.ReadyReplicas = 2
	if _, err := client.AppsV1().StatefulSets("jobs").UpdateStatus(
		context.Background(), workers, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update Worker readiness: %v", err)
	}

	observation, err = dispatcher.Reconcile(context.Background(), candidate, record)
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
	var heartbeatTokenRef *corev1.EnvVarSource
	for _, variable := range coordinator.Spec.Template.Spec.Containers[0].Env {
		environment[variable.Name] = variable.Value
		if variable.Name == "ASTRASYNC_COORDINATOR_HEARTBEAT_TOKEN" {
			heartbeatTokenRef = variable.ValueFrom
		}
	}
	if environment["ASTRASYNC_COORDINATOR_EXECUTION_EPOCH"] != "4" {
		t.Fatalf("Coordinator epoch environment missing: %v", environment)
	}
	if environment["ASTRASYNC_COORDINATOR_HEARTBEAT_URL"] !=
		"http://scheduler.jobs.svc:8082/v1/executions/"+candidate.UID+"/4/heartbeat" ||
		environment["ASTRASYNC_COORDINATOR_HEARTBEAT_INTERVAL_MS"] != "10000" {
		t.Fatalf("Coordinator heartbeat environment missing: %v", environment)
	}
	if heartbeatTokenRef == nil || heartbeatTokenRef.SecretKeyRef == nil ||
		heartbeatTokenRef.SecretKeyRef.Name != base+"-spec" || heartbeatTokenRef.SecretKeyRef.Key != heartbeatTokenKey {
		t.Fatalf("Coordinator heartbeat token reference missing: %+v", heartbeatTokenRef)
	}
	if environment["ASTRASYNC_COORDINATOR_WORKERS"] == "" {
		t.Fatal("Coordinator Worker endpoints are empty")
	}

	coordinator.Status.Active = 1
	if _, err := client.BatchV1().Jobs("jobs").UpdateStatus(
		context.Background(), coordinator, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update Coordinator active status: %v", err)
	}
	observation, err = dispatcher.Reconcile(context.Background(), candidate, record)
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
	observation, err = dispatcher.Reconcile(context.Background(), candidate, record)
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
	if _, err := dispatcher.Reconcile(context.Background(), candidate, testRecord(identity)); err != nil {
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

	_, err = dispatcher.Reconcile(context.Background(), candidate, testRecord(identity))
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
	_, err = dispatcher.Reconcile(
		context.Background(), candidate, testRecord(dispatch.Identity{JobUID: candidate.UID, Epoch: 1}))
	var permanent *scheduler.PermanentError
	if !errors.As(err, &permanent) {
		t.Fatalf("expected permanent dispatch rejection, got %v", err)
	}
	if actions := client.Actions(); len(actions) != 0 {
		t.Fatalf("created Kubernetes resources before permanent validation: %v", actions)
	}
}

func TestDispatcherRejectsMalformedHeartbeatURL(t *testing.T) {
	configuration := testConfig()
	configuration.HeartbeatURL = "http://"
	if _, err := New(k8sfake.NewSimpleClientset(), configuration); err == nil {
		t.Fatal("expected malformed heartbeat URL to be rejected")
	}
}

func TestDispatcherSweepsOnlyUnclaimedAuxiliaryExecutionResources(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	dispatcher, err := New(client, testConfig())
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}
	candidate := testJob(t)
	active := dispatch.Identity{JobUID: candidate.UID, Epoch: 1}
	terminal := dispatch.Identity{JobUID: candidate.UID, Epoch: 2}
	orphan := dispatch.Identity{JobUID: candidate.UID, Epoch: 3}
	if _, err := dispatcher.Reconcile(context.Background(), candidate, testRecord(active)); err != nil {
		t.Fatalf("materialize active execution: %v", err)
	}
	if _, err := dispatcher.Reconcile(context.Background(), candidate, testRecord(terminal)); err != nil {
		t.Fatalf("materialize terminal execution: %v", err)
	}
	terminalBase, _ := resourceBase(terminal)
	if _, err := client.BatchV1().Jobs("jobs").Create(context.Background(), &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: terminalBase + "-coordinator", Namespace: "jobs",
			Labels: labels(terminal, "coordinator"), Annotations: annotations("terminal"),
		},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create retained Coordinator Job: %v", err)
	}
	orphanBase, _ := resourceBase(orphan)
	if _, err := client.BatchV1().Jobs("jobs").Create(context.Background(), &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: orphanBase + "-coordinator", Namespace: "jobs",
			Labels: labels(orphan, "coordinator"), Annotations: annotations("orphan"),
		},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create orphan Coordinator Job: %v", err)
	}

	if err := dispatcher.SweepOrphans(context.Background(), []dispatch.Record{
		{Identity: active, Phase: dispatch.PhaseRunning},
		{Identity: terminal, Phase: dispatch.PhaseFailed},
	}); err != nil {
		t.Fatalf("sweep orphans: %v", err)
	}
	activeBase, _ := resourceBase(active)
	for _, check := range []struct {
		name string
		get  func() error
	}{
		{name: "active Secret", get: func() error {
			_, err := client.CoreV1().Secrets("jobs").Get(context.Background(), activeBase+"-spec", metav1.GetOptions{})
			return err
		}},
		{name: "active Service", get: func() error {
			_, err := client.CoreV1().Services("jobs").Get(context.Background(), activeBase+"-worker", metav1.GetOptions{})
			return err
		}},
		{name: "active StatefulSet", get: func() error {
			_, err := client.AppsV1().StatefulSets("jobs").Get(context.Background(), activeBase+"-worker", metav1.GetOptions{})
			return err
		}},
	} {
		if err := check.get(); err != nil {
			t.Fatalf("%s was swept: %v", check.name, err)
		}
	}
	for _, check := range []struct {
		name string
		get  func() error
	}{
		{name: "orphan Secret", get: func() error {
			_, err := client.CoreV1().Secrets("jobs").Get(context.Background(), terminalBase+"-spec", metav1.GetOptions{})
			return err
		}},
		{name: "orphan Service", get: func() error {
			_, err := client.CoreV1().Services("jobs").Get(context.Background(), terminalBase+"-worker", metav1.GetOptions{})
			return err
		}},
		{name: "orphan StatefulSet", get: func() error {
			_, err := client.AppsV1().StatefulSets("jobs").Get(context.Background(), terminalBase+"-worker", metav1.GetOptions{})
			return err
		}},
	} {
		if err := check.get(); !apierrors.IsNotFound(err) {
			t.Fatalf("%s was not swept: %v", check.name, err)
		}
	}
	if _, err := client.BatchV1().Jobs("jobs").Get(
		context.Background(), terminalBase+"-coordinator", metav1.GetOptions{}); err != nil {
		t.Fatalf("terminal Coordinator Job was swept: %v", err)
	}
	if _, err := client.BatchV1().Jobs("jobs").Get(
		context.Background(), orphanBase+"-coordinator", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("orphan Coordinator Job was not swept: %v", err)
	}
}

func TestDispatcherSweepPreservesUnmanagedAndMalformedResources(t *testing.T) {
	identity := dispatch.Identity{JobUID: uuid.NewString(), Epoch: 9}
	unmanagedLabels := labels(identity, "worker")
	delete(unmanagedLabels, "app.kubernetes.io/managed-by")
	client := k8sfake.NewSimpleClientset(
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name: "malformed-secret", Namespace: "jobs", Labels: map[string]string{
				"app.kubernetes.io/managed-by": "astrasync-scheduler",
				labelComponent:                 "job-spec", labelJobUID: "not-a-uuid", labelEpoch: "9",
			},
		}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{
			Name: "missing-epoch-service", Namespace: "jobs", Labels: map[string]string{
				"app.kubernetes.io/managed-by": "astrasync-scheduler",
				labelComponent:                 "worker-service", labelJobUID: identity.JobUID,
			},
		}},
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{
			Name: "unmanaged-workers", Namespace: "jobs", Labels: unmanagedLabels,
		}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{
			Name: "malformed-coordinator", Namespace: "jobs", Labels: map[string]string{
				"app.kubernetes.io/managed-by": "astrasync-scheduler",
				labelComponent:                 "coordinator", labelJobUID: identity.JobUID, labelEpoch: "zero",
			},
		}},
	)
	dispatcher, err := New(client, testConfig())
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}

	if err := dispatcher.SweepOrphans(context.Background(), nil); err != nil {
		t.Fatalf("sweep malformed resources: %v", err)
	}
	checks := []struct {
		name string
		get  func() error
	}{
		{name: "malformed Secret", get: func() error {
			_, err := client.CoreV1().Secrets("jobs").Get(context.Background(), "malformed-secret", metav1.GetOptions{})
			return err
		}},
		{name: "missing-epoch Service", get: func() error {
			_, err := client.CoreV1().Services("jobs").Get(context.Background(), "missing-epoch-service", metav1.GetOptions{})
			return err
		}},
		{name: "unmanaged StatefulSet", get: func() error {
			_, err := client.AppsV1().StatefulSets("jobs").Get(context.Background(), "unmanaged-workers", metav1.GetOptions{})
			return err
		}},
		{name: "malformed Coordinator Job", get: func() error {
			_, err := client.BatchV1().Jobs("jobs").Get(context.Background(), "malformed-coordinator", metav1.GetOptions{})
			return err
		}},
	}
	for _, check := range checks {
		if err := check.get(); err != nil {
			t.Fatalf("%s was deleted: %v", check.name, err)
		}
	}
}

func testConfig() Config {
	ttl := int32(3600)
	return Config{
		Namespace: "jobs", ServiceAccount: "astrasync", CoordinatorImage: "coordinator:test",
		WorkerImage: "worker:test", ImagePullPolicy: corev1.PullIfNotPresent, ProgressClaim: "progress",
		HeartbeatURL: "http://scheduler.jobs.svc:8082", HeartbeatIntervalMillis: 10000,
		WorkerReplicas: 2, WorkerPort: 50051, WorkerTimeoutMillis: 30000,
		CoordinatorMaxInFlightTasks: 1, WorkerMaxInFlightBatches: 1, WorkerMaxConcurrentTasks: 1,
		WorkerMaxConnections: 8, CoordinatorBackoffLimit: 2, TTLSecondsAfterFinished: &ttl,
	}
}

func testRecord(identity dispatch.Identity) dispatch.Record {
	return dispatch.Record{Identity: identity, HeartbeatToken: "00000000-0000-4000-8000-000000000001"}
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
