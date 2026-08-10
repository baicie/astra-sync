package materialization

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"io.astrasync/control-plane/connection"
)

const (
	TenantOwnershipLabel = "sync.astrasync.io/tenant-id"
	maximumProviderBytes = 256 * 1024
)

type KubernetesSecretProvider struct {
	client kubernetes.Interface
}

func NewKubernetesSecretProvider(client kubernetes.Interface) (*KubernetesSecretProvider, error) {
	if client == nil {
		return nil, fmt.Errorf("Kubernetes Secret provider client must not be nil")
	}
	return &KubernetesSecretProvider{client: client}, nil
}

func (p *KubernetesSecretProvider) Materialize(
	ctx context.Context, binding Binding,
) (*Values, error) {
	if err := binding.Validate(); err != nil ||
		binding.Locator.Provider != connection.ProviderKubernetesSecretV1 {
		return nil, ErrProviderPolicy
	}
	return p.Resolve(ctx, binding.CredentialRequest())
}

func (p *KubernetesSecretProvider) Resolve(
	ctx context.Context, request CredentialRequest,
) (*Values, error) {
	if err := request.Validate(); err != nil ||
		request.Locator.Provider != connection.ProviderKubernetesSecretV1 {
		return nil, ErrProviderPolicy
	}
	secret, err := p.client.CoreV1().Secrets(request.TenantNamespace).Get(
		ctx, request.Locator.SecretName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) || apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
			return nil, ErrProviderUnavailable
		}
		return nil, errors.Join(ErrProviderUnavailable, errors.New("provider request failed"))
	}
	if secret.Immutable == nil || !*secret.Immutable || string(secret.UID) != request.Locator.SecretUID ||
		secret.Labels[TenantOwnershipLabel] != request.TenantID || secret.ResourceVersion == "" {
		return nil, ErrProviderPolicy
	}
	expectedKeys := make(map[string]struct{}, len(request.Locator.Fields))
	logicalValues := make(map[string][]byte, len(request.Locator.Fields))
	defer func() {
		for key, value := range logicalValues {
			clear(value)
			delete(logicalValues, key)
		}
	}()
	totalBytes := 0
	for _, mapping := range request.Locator.Fields {
		if _, duplicate := expectedKeys[mapping.SecretKey]; duplicate {
			return nil, ErrProviderPolicy
		}
		expectedKeys[mapping.SecretKey] = struct{}{}
		value, found := secret.Data[mapping.SecretKey]
		if !found || len(value) == 0 || len(value) > connection.MaximumSettingValue {
			return nil, ErrProviderPolicy
		}
		totalBytes += len(value)
		if totalBytes > maximumProviderBytes {
			return nil, ErrProviderPolicy
		}
		logicalValues[mapping.LogicalField] = append([]byte(nil), value...)
	}
	if len(secret.Data) != len(expectedKeys) {
		return nil, ErrProviderPolicy
	}
	values, err := NewValues(logicalValues, ProviderReceipt{
		Kind:      connection.ProviderKubernetesSecretV1,
		ObjectUID: string(secret.UID), VersionToken: secret.ResourceVersion,
	})
	if err != nil {
		return nil, ErrProviderPolicy
	}
	return values, nil
}
