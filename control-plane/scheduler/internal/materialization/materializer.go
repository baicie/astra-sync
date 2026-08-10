package materialization

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"io.astrasync/control-plane/scheduler/internal/dispatch"
)

const (
	credentialMountPath        = "/etc/astrasync/credentials"
	credentialComponent        = "execution-credentials"
	materializationAnnotation  = "sync.astrasync.io/materialization-identity"
	materializationJobUIDLabel = "sync.astrasync.io/job-uid"
	materializationEpochLabel  = "sync.astrasync.io/execution-epoch"
	managedByLabel             = "app.kubernetes.io/managed-by"
	componentLabel             = "app.kubernetes.io/component"
	maximumCredentialEnvelope  = 512 * 1024
)

type Result struct {
	Required            bool
	SecretName          string
	IdentityFingerprint string
	CompilerRevision    string
	Roles               []Role
}

type Materializer struct {
	store     Store
	provider  Provider
	client    kubernetes.Interface
	namespace string
	clock     func() time.Time
}

func NewMaterializer(
	store Store,
	provider Provider,
	client kubernetes.Interface,
	namespace string,
	clock func() time.Time,
) (*Materializer, error) {
	if store == nil || provider == nil || client == nil || strings.TrimSpace(namespace) == "" || clock == nil {
		return nil, fmt.Errorf("credential materializer dependencies must not be nil or blank")
	}
	return &Materializer{store: store, provider: provider, client: client, namespace: namespace, clock: clock}, nil
}

func (m *Materializer) Ensure(ctx context.Context, record dispatch.Record) (Result, error) {
	now := m.clock().UTC()
	bindings, err := m.store.Load(ctx, record, now)
	if err != nil {
		return Result{}, err
	}
	bindings = SortBindings(bindings)
	if len(bindings) == 0 {
		return Result{}, nil
	}
	if len(bindings) > 2 {
		return Result{}, ErrRevisionMismatch
	}
	for index, binding := range bindings {
		if err := binding.Validate(); err != nil || binding.JobUID != record.Identity.JobUID ||
			binding.Epoch != record.Identity.Epoch || index > 0 && binding.Role == bindings[index-1].Role ||
			index > 0 && binding.CompilerRevision != bindings[0].CompilerRevision {
			return Result{}, ErrRevisionMismatch
		}
	}
	if err := m.store.ClaimCleanup(ctx, record, bindings, now); err != nil {
		return Result{}, err
	}
	secretName, err := CredentialSecretName(record.Identity)
	if err != nil {
		return Result{}, err
	}
	fingerprint := bindingIdentityFingerprint(bindings)
	result := Result{
		Required: true, SecretName: secretName, IdentityFingerprint: fingerprint,
		CompilerRevision: bindings[0].CompilerRevision,
		Roles:            make([]Role, len(bindings)),
	}
	for index, binding := range bindings {
		result.Roles[index] = binding.Role
	}

	existing, getErr := m.client.CoreV1().Secrets(m.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if getErr != nil && !apierrors.IsNotFound(getErr) {
		return Result{}, fmt.Errorf("get execution credential Secret: %w", getErr)
	}
	receipts, err := m.store.Receipts(ctx, record.Identity)
	if err != nil {
		return Result{}, err
	}
	if getErr == nil {
		if err := verifyCredentialSecret(existing, record.Identity, fingerprint); err != nil {
			return Result{}, err
		}
		if completeReceiptSet(receipts, bindings, existing) {
			return result, nil
		}
	}

	data, providerReceipts, closeValues, err := m.buildEnvelopes(ctx, bindings)
	if closeValues != nil {
		defer closeValues()
	}
	if err != nil {
		return Result{}, err
	}
	if getErr == nil {
		if !sameSecretData(existing.Data, data) {
			return Result{}, ErrReceiptConflict
		}
	} else {
		immutable := true
		existing, err = m.client.CoreV1().Secrets(m.namespace).Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: secretName, Namespace: m.namespace,
				Labels:      credentialLabels(record.Identity),
				Annotations: map[string]string{materializationAnnotation: fingerprint},
			},
			Immutable: &immutable, Type: corev1.SecretTypeOpaque, Data: data,
		}, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			existing, err = m.client.CoreV1().Secrets(m.namespace).Get(ctx, secretName, metav1.GetOptions{})
			if err == nil {
				err = verifyCredentialSecret(existing, record.Identity, fingerprint)
			}
			if err == nil && !sameSecretData(existing.Data, data) {
				err = ErrReceiptConflict
			}
		}
		if err != nil {
			return Result{}, fmt.Errorf("create execution credential Secret: %w", err)
		}
	}
	if _, err := uuid.Parse(string(existing.UID)); err != nil || existing.ResourceVersion == "" {
		return Result{}, ErrReceiptConflict
	}
	committed := make([]Receipt, len(bindings))
	for index, binding := range bindings {
		committed[index] = Receipt{
			TenantID: binding.TenantID, JobUID: binding.JobUID, Epoch: binding.Epoch, Role: binding.Role,
			ConnectionUID: binding.ConnectionUID, Generation: binding.Generation,
			DescriptorRevision: binding.DescriptorRevision, Provider: providerReceipts[index],
			GeneratedSecretName: secretName, GeneratedSecretUID: string(existing.UID),
			GeneratedResourceVersion: existing.ResourceVersion, CreatedAt: now,
		}
	}
	if err := m.store.CommitReceipts(ctx, record, committed, now); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (m *Materializer) Cleanup(ctx context.Context, identity dispatch.Identity) error {
	name, err := CredentialSecretName(identity)
	if err != nil {
		return err
	}
	secret, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		if secret.Labels[materializationJobUIDLabel] != identity.JobUID ||
			secret.Labels[materializationEpochLabel] != strconv.FormatInt(identity.Epoch, 10) ||
			secret.Labels[componentLabel] != credentialComponent ||
			secret.Labels[managedByLabel] != "astrasync-scheduler" {
			return ErrReceiptConflict
		}
		if err := m.client.CoreV1().Secrets(m.namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil &&
			!apierrors.IsNotFound(err) {
			return fmt.Errorf("delete execution credential Secret: %w", err)
		}
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get execution credential Secret for cleanup: %w", err)
	}
	if _, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, name, metav1.GetOptions{}); err == nil {
		return fmt.Errorf("execution credential Secret is still terminating")
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("confirm execution credential Secret cleanup: %w", err)
	}
	return m.store.CompleteCleanup(ctx, identity, m.clock().UTC())
}

func (m *Materializer) buildEnvelopes(
	ctx context.Context, bindings []Binding,
) (map[string][]byte, []ProviderReceipt, func(), error) {
	valuesByGeneration := make(map[string]*Values)
	closeValues := func() {
		for key, values := range valuesByGeneration {
			_ = values.Close()
			delete(valuesByGeneration, key)
		}
	}
	data := make(map[string][]byte, len(bindings))
	receipts := make([]ProviderReceipt, len(bindings))
	for index, binding := range bindings {
		fields := map[string][]byte{}
		providerReceipt := ProviderReceipt{
			Kind: ProviderNone, ObjectUID: binding.ConnectionUID,
			VersionToken: "generation:" + strconv.FormatInt(binding.Generation, 10),
		}
		if binding.Locator.Provider != "" {
			cacheKey := binding.ConnectionUID + "/" + strconv.FormatInt(binding.Generation, 10)
			values := valuesByGeneration[cacheKey]
			if values == nil {
				var err error
				values, err = m.provider.Materialize(ctx, binding)
				if err != nil {
					closeValues()
					return nil, nil, nil, err
				}
				valuesByGeneration[cacheKey] = values
			}
			var err error
			fields, err = values.Fields()
			if err != nil {
				closeValues()
				return nil, nil, nil, err
			}
			providerReceipt, err = values.Receipt()
			if err != nil {
				closeValues()
				return nil, nil, nil, err
			}
		}
		envelope, err := encodeEnvelope(binding, fields, providerReceipt)
		for key, value := range fields {
			clear(value)
			delete(fields, key)
		}
		if err != nil {
			closeValues()
			return nil, nil, nil, err
		}
		data[strings.ToLower(string(binding.Role))+".json"] = envelope
		receipts[index] = providerReceipt
	}
	return data, receipts, closeValues, nil
}

type envelopeOption struct {
	Key    string `json:"key"`
	Source string `json:"source"`
	Value  []byte `json:"value"`
}

type runtimeEnvelope struct {
	SchemaVersion            int              `json:"schemaVersion"`
	JobUID                   string           `json:"jobUid"`
	Epoch                    int64            `json:"epoch"`
	Role                     Role             `json:"role"`
	ConnectionUID            string           `json:"connectionUid"`
	Generation               int64            `json:"generation"`
	DescriptorRevision       string           `json:"descriptorRevision"`
	CompilerRevision         string           `json:"compilerRevision"`
	ConnectionSchemaRevision string           `json:"connectionSchemaRevision"`
	ProviderKind             string           `json:"providerKind"`
	ProviderObjectUID        string           `json:"providerObjectUid"`
	ProviderVersionToken     string           `json:"providerVersionToken"`
	Options                  []envelopeOption `json:"options"`
}

func encodeEnvelope(
	binding Binding, secretFields map[string][]byte, providerReceipt ProviderReceipt,
) ([]byte, error) {
	options := make(map[string]envelopeOption, len(binding.Settings)+len(secretFields))
	for _, setting := range binding.Settings {
		if _, duplicate := options[setting.Key]; duplicate {
			return nil, ErrRevisionMismatch
		}
		options[setting.Key] = envelopeOption{
			Key: setting.Key, Source: "CONNECTION_SETTING", Value: []byte(setting.Value),
		}
	}
	for key, value := range secretFields {
		if _, duplicate := options[key]; duplicate {
			return nil, ErrRevisionMismatch
		}
		options[key] = envelopeOption{Key: key, Source: "PROVIDER", Value: append([]byte(nil), value...)}
	}
	keys := make([]string, 0, len(options))
	for key := range options {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	envelope := runtimeEnvelope{
		SchemaVersion: 1, JobUID: binding.JobUID, Epoch: binding.Epoch, Role: binding.Role,
		ConnectionUID: binding.ConnectionUID, Generation: binding.Generation,
		DescriptorRevision: binding.DescriptorRevision, CompilerRevision: binding.CompilerRevision,
		ConnectionSchemaRevision: binding.ConnectionSchemaRevision,
		ProviderKind:             string(providerReceipt.Kind), ProviderObjectUID: providerReceipt.ObjectUID,
		ProviderVersionToken: providerReceipt.VersionToken,
		Options:              make([]envelopeOption, len(keys)),
	}
	for index, key := range keys {
		envelope.Options[index] = options[key]
	}
	encoded, err := json.Marshal(envelope)
	for key, option := range options {
		clear(option.Value)
		delete(options, key)
	}
	if err != nil || len(encoded) > maximumCredentialEnvelope {
		return nil, fmt.Errorf("credential envelope exceeds supported bounds")
	}
	return encoded, nil
}

func completeReceiptSet(receipts []Receipt, bindings []Binding, secret *corev1.Secret) bool {
	if len(receipts) != len(bindings) {
		return false
	}
	byRole := make(map[Role]Receipt, len(receipts))
	for _, receipt := range receipts {
		byRole[receipt.Role] = receipt
	}
	for _, binding := range bindings {
		receipt, found := byRole[binding.Role]
		if !found || receipt.TenantID != binding.TenantID || receipt.ConnectionUID != binding.ConnectionUID ||
			receipt.Generation != binding.Generation || receipt.DescriptorRevision != binding.DescriptorRevision ||
			receipt.GeneratedSecretName != secret.Name || receipt.GeneratedSecretUID != string(secret.UID) ||
			receipt.GeneratedResourceVersion != secret.ResourceVersion {
			return false
		}
	}
	return true
}

func sameSecretData(left, right map[string][]byte) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if !bytes.Equal(value, right[key]) {
			return false
		}
	}
	return true
}

func verifyCredentialSecret(secret *corev1.Secret, identity dispatch.Identity, fingerprint string) error {
	if secret.Immutable == nil || !*secret.Immutable ||
		secret.Labels[materializationJobUIDLabel] != identity.JobUID ||
		secret.Labels[materializationEpochLabel] != strconv.FormatInt(identity.Epoch, 10) ||
		secret.Labels[componentLabel] != credentialComponent ||
		secret.Labels[managedByLabel] != "astrasync-scheduler" ||
		secret.Annotations[materializationAnnotation] != fingerprint {
		return ErrReceiptConflict
	}
	return nil
}

func CredentialSecretName(identity dispatch.Identity) (string, error) {
	if err := identity.Validate(); err != nil {
		return "", err
	}
	if _, err := uuid.Parse(identity.JobUID); err != nil {
		return "", fmt.Errorf("job UID must be a UUID")
	}
	return "job-" + strings.ReplaceAll(strings.ToLower(identity.JobUID), "-", "") +
		"-e" + strconv.FormatInt(identity.Epoch, 36) + "-credentials", nil
}

func bindingIdentityFingerprint(bindings []Binding) string {
	parts := make([]string, len(bindings))
	for index, binding := range bindings {
		parts[index] = strings.Join([]string{
			string(binding.Role), binding.TenantID, binding.ConnectionUID,
			strconv.FormatInt(binding.Generation, 10), binding.DescriptorRevision,
			binding.CompilerRevision, binding.ConnectionSchemaRevision,
		}, "|")
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(digest[:])
}

func credentialLabels(identity dispatch.Identity) map[string]string {
	return map[string]string{
		materializationJobUIDLabel: identity.JobUID,
		materializationEpochLabel:  strconv.FormatInt(identity.Epoch, 10),
		componentLabel:             credentialComponent, managedByLabel: "astrasync-scheduler",
	}
}

func CredentialMountPath() string { return credentialMountPath }
