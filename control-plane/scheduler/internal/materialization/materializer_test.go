package materialization

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"io.astrasync/control-plane/connection"
	"io.astrasync/control-plane/scheduler/internal/dispatch"
)

func TestKubernetesProviderAndMaterializerCreateAndReuseEpochSecret(t *testing.T) {
	now := time.Date(2026, 8, 8, 18, 0, 0, 0, time.UTC)
	binding := testBinding()
	immutable := true
	providerSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: binding.Locator.SecretName, Namespace: binding.TenantNamespace,
			UID: types.UID(binding.Locator.SecretUID), ResourceVersion: "provider-rv-7",
			Labels: map[string]string{TenantOwnershipLabel: binding.TenantID},
		},
		Immutable: &immutable,
		Data: map[string][]byte{
			"password": []byte("password-sentinel-42"), "username": []byte("orders-user"),
		},
	}
	client := k8sfake.NewSimpleClientset(providerSecret)
	generatedUID := uuid.NewString()
	client.PrependReactor("create", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		create := action.(k8stesting.CreateAction)
		secret := create.GetObject().(*corev1.Secret)
		if secret.Namespace == "jobs" {
			secret.UID = types.UID(generatedUID)
			secret.ResourceVersion = "generated-rv-1"
		}
		return false, nil, nil
	})
	provider, err := NewKubernetesSecretProvider(client)
	if err != nil {
		t.Fatalf("create Kubernetes provider: %v", err)
	}
	store := &fakeMaterializationStore{bindings: []Binding{binding}}
	materializer, err := NewMaterializer(store, provider, client, "jobs", func() time.Time { return now })
	if err != nil {
		t.Fatalf("create materializer: %v", err)
	}
	record := testDispatch(binding.JobUID, binding.Epoch)
	result, err := materializer.Ensure(context.Background(), record)
	if err != nil || !result.Required || len(result.Roles) != 1 || result.Roles[0] != RoleSource {
		t.Fatalf("materialize epoch credentials: result=%+v err=%v", result, err)
	}
	if store.claims != 1 || store.commits != 1 || len(store.receipts) != 1 {
		t.Fatalf("durable materialization boundaries were not committed: %+v", store)
	}
	generated, err := client.CoreV1().Secrets("jobs").Get(
		context.Background(), result.SecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("read generated credential Secret: %v", err)
	}
	if generated.Immutable == nil || !*generated.Immutable || string(generated.UID) != generatedUID ||
		generated.Labels[componentLabel] != credentialComponent ||
		strings.Contains(generated.String(), binding.Locator.SecretName) ||
		strings.Contains(generated.String(), binding.Locator.SecretUID) {
		t.Fatalf("generated Secret metadata is unsafe or incomplete: %+v", generated.ObjectMeta)
	}
	var envelope runtimeEnvelope
	if err := json.Unmarshal(generated.Data["source.json"], &envelope); err != nil {
		t.Fatalf("decode generated envelope: %v", err)
	}
	if envelope.JobUID != binding.JobUID || envelope.Epoch != binding.Epoch ||
		envelope.ConnectionUID != binding.ConnectionUID || envelope.Generation != binding.Generation ||
		len(envelope.Options) != 4 {
		t.Fatalf("unexpected runtime envelope: %+v", envelope)
	}

	if err := client.CoreV1().Secrets(binding.TenantNamespace).Delete(
		context.Background(), binding.Locator.SecretName, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("remove external provider fixture: %v", err)
	}
	reused, err := materializer.Ensure(context.Background(), record)
	if err != nil || reused.SecretName != result.SecretName || store.commits != 1 {
		t.Fatalf("durable receipt did not allow exact retry reuse: result=%+v err=%v", reused, err)
	}
	if err := materializer.Cleanup(context.Background(), record.Identity); err != nil {
		t.Fatalf("cleanup epoch credentials: %v", err)
	}
	if store.cleanups != 1 {
		t.Fatalf("cleanup obligation was not completed: %+v", store)
	}
}

func TestKubernetesProviderRejectsMutableMismatchedAndExtraData(t *testing.T) {
	binding := testBinding()
	immutable := true
	valid := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: binding.Locator.SecretName, Namespace: binding.TenantNamespace,
			UID: types.UID(binding.Locator.SecretUID), ResourceVersion: "rv-1",
			Labels: map[string]string{TenantOwnershipLabel: binding.TenantID},
		}, Immutable: &immutable,
		Data: map[string][]byte{"password": []byte("secret"), "username": []byte("user")},
	}
	for name, mutate := range map[string]func(*corev1.Secret){
		"mutable":      func(secret *corev1.Secret) { secret.Immutable = nil },
		"wrong uid":    func(secret *corev1.Secret) { secret.UID = types.UID(uuid.NewString()) },
		"wrong tenant": func(secret *corev1.Secret) { secret.Labels[TenantOwnershipLabel] = uuid.NewString() },
		"extra key":    func(secret *corev1.Secret) { secret.Data["token"] = []byte("extra") },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid.DeepCopy()
			mutate(candidate)
			provider, err := NewKubernetesSecretProvider(k8sfake.NewSimpleClientset(candidate))
			if err != nil {
				t.Fatalf("create provider: %v", err)
			}
			if _, err := provider.Materialize(context.Background(), binding); !errors.Is(err, ErrProviderPolicy) {
				t.Fatalf("expected provider policy rejection, got %v", err)
			}
		})
	}
}

func TestMaterializerFailsClosedOnEpochIdentityCollision(t *testing.T) {
	binding := testBinding()
	immutable := true
	name, _ := CredentialSecretName(dispatch.Identity{JobUID: binding.JobUID, Epoch: binding.Epoch})
	client := k8sfake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: "jobs", UID: types.UID(uuid.NewString()), ResourceVersion: "rv-1",
			Labels:      credentialLabels(dispatch.Identity{JobUID: binding.JobUID, Epoch: binding.Epoch}),
			Annotations: map[string]string{materializationAnnotation: "another-generation"},
		}, Immutable: &immutable, Data: map[string][]byte{"source.json": []byte("other")},
	})
	provider, _ := NewKubernetesSecretProvider(client)
	store := &fakeMaterializationStore{bindings: []Binding{binding}}
	materializer, _ := NewMaterializer(store, provider, client, "jobs", time.Now)
	if _, err := materializer.Ensure(context.Background(), testDispatch(binding.JobUID, binding.Epoch)); !errors.Is(err, ErrReceiptConflict) {
		t.Fatalf("expected immutable epoch identity collision, got %v", err)
	}
}

func TestCredentialValuesAreZeroedAndClosed(t *testing.T) {
	values, err := NewValues(map[string][]byte{"password": []byte("secret-sentinel")}, ProviderReceipt{
		Kind: connection.ProviderKubernetesSecretV1, ObjectUID: uuid.NewString(), VersionToken: "rv-1",
	})
	if err != nil {
		t.Fatalf("create values: %v", err)
	}
	backing := values.fields["password"]
	if err := values.Close(); err != nil {
		t.Fatalf("close values: %v", err)
	}
	for _, value := range backing {
		if value != 0 {
			t.Fatalf("credential buffer was not zeroed: %v", backing)
		}
	}
	if _, err := values.Fields(); err == nil {
		t.Fatal("closed values remained readable")
	}
}

func TestMaterializerDoesNotCompleteCleanupWhileEpochSecretStillExists(t *testing.T) {
	binding := testBinding()
	identity := dispatch.Identity{JobUID: binding.JobUID, Epoch: binding.Epoch}
	name, err := CredentialSecretName(identity)
	if err != nil {
		t.Fatalf("credential Secret name: %v", err)
	}
	immutable := true
	client := k8sfake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: "jobs", UID: types.UID(uuid.NewString()),
			Labels: credentialLabels(identity),
		},
		Immutable: &immutable,
	})
	client.PrependReactor("delete", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, nil
	})
	store := &fakeMaterializationStore{}
	provider, _ := NewKubernetesSecretProvider(client)
	materializer, _ := NewMaterializer(store, provider, client, "jobs", time.Now)

	if err := materializer.Cleanup(context.Background(), identity); err == nil ||
		!strings.Contains(err.Error(), "still terminating") {
		t.Fatalf("expected retriable terminating cleanup result, got %v", err)
	}
	if store.cleanups != 0 {
		t.Fatalf("cleanup obligation completed before Secret disappearance: %+v", store)
	}
}

type fakeMaterializationStore struct {
	bindings []Binding
	receipts []Receipt
	claims   int
	commits  int
	cleanups int
}

func (s *fakeMaterializationStore) Load(
	_ context.Context, _ dispatch.Record, _ time.Time,
) ([]Binding, error) {
	return append([]Binding(nil), s.bindings...), nil
}

func (s *fakeMaterializationStore) ClaimCleanup(
	_ context.Context, _ dispatch.Record, _ []Binding, _ time.Time,
) error {
	s.claims++
	return nil
}

func (s *fakeMaterializationStore) Receipts(
	_ context.Context, _ dispatch.Identity,
) ([]Receipt, error) {
	return append([]Receipt(nil), s.receipts...), nil
}

func (s *fakeMaterializationStore) CommitReceipts(
	_ context.Context, _ dispatch.Record, receipts []Receipt, _ time.Time,
) error {
	s.commits++
	s.receipts = append([]Receipt(nil), receipts...)
	return nil
}

func (s *fakeMaterializationStore) CompleteCleanup(
	_ context.Context, _ dispatch.Identity, _ time.Time,
) error {
	s.cleanups++
	return nil
}

func testBinding() Binding {
	tenantID := uuid.NewString()
	jobUID := uuid.NewString()
	connectionUID := uuid.NewString()
	providerUID := uuid.NewString()
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	return Binding{
		TenantID: tenantID, TenantNamespace: "tenant-a", Role: RoleSource,
		JobUID: jobUID, Epoch: 7, ConnectionUID: connectionUID, Generation: 3,
		Connector: "mysql-cdc", DescriptorRevision: digest, CompilerRevision: digest,
		ConnectionSchemaRevision: digest,
		Settings: []connection.Setting{
			{Key: "hostname", Value: "db.internal", Sensitivity: connection.SensitivityRestricted},
			{Key: "sslMode", Value: "required", Sensitivity: connection.SensitivityPublic},
		},
		Locator: connection.SecretLocator{
			Provider:   connection.ProviderKubernetesSecretV1,
			SecretName: "orders-v3", SecretUID: providerUID,
			Fields: []connection.SecretFieldMapping{
				{LogicalField: "password", SecretKey: "password"},
				{LogicalField: "username", SecretKey: "username"},
			},
		},
	}
}

func testDispatch(jobUID string, epoch int64) dispatch.Record {
	return dispatch.Record{
		Identity: dispatch.Identity{JobUID: jobUID, Epoch: epoch}, OwnerID: "scheduler-a",
		Phase: dispatch.PhaseStarting, LeaseExpiresAt: time.Now().Add(time.Minute),
	}
}
