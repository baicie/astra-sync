package materialization

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"io.astrasync/control-plane/connection"
	"io.astrasync/control-plane/scheduler/internal/dispatch"
)

const (
	RoleSource   Role                    = "SOURCE"
	RoleSink     Role                    = "SINK"
	ProviderNone connection.ProviderKind = "NONE"
)

var (
	ErrLeaseLost           = errors.New("materialization dispatch lease lost")
	ErrRevisionMismatch    = errors.New("materialization revision mismatch")
	ErrReceiptConflict     = errors.New("materialization receipt conflict")
	ErrProviderPolicy      = errors.New("credential provider policy rejected captured generation")
	ErrProviderUnavailable = errors.New("credential provider is unavailable")
)

var revisionPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type Role string

type Binding struct {
	TenantID                 string
	TenantNamespace          string
	Role                     Role
	JobUID                   string
	Epoch                    int64
	ConnectionUID            string
	Generation               int64
	Connector                string
	DescriptorRevision       string
	CompilerRevision         string
	ConnectionSchemaRevision string
	Settings                 []connection.Setting
	Locator                  connection.SecretLocator
}

type CredentialRequest struct {
	TenantID        string
	TenantNamespace string
	Locator         connection.SecretLocator
}

func (r CredentialRequest) Validate() error {
	if _, err := uuid.Parse(r.TenantID); err != nil {
		return fmt.Errorf("credential request tenant ID must be a UUID")
	}
	if strings.TrimSpace(r.TenantNamespace) == "" || len(r.TenantNamespace) > 63 {
		return fmt.Errorf("credential request tenant namespace is invalid")
	}
	if err := r.Locator.Validate(); err != nil {
		return err
	}
	return nil
}

func (b Binding) CredentialRequest() CredentialRequest {
	return CredentialRequest{
		TenantID: b.TenantID, TenantNamespace: b.TenantNamespace, Locator: b.Locator.Clone(),
	}
}

func (b Binding) Validate() error {
	if _, err := uuid.Parse(b.TenantID); err != nil {
		return fmt.Errorf("materialization tenant ID must be a UUID")
	}
	if strings.TrimSpace(b.TenantNamespace) == "" || len(b.TenantNamespace) > 63 ||
		(b.Role != RoleSource && b.Role != RoleSink) {
		return fmt.Errorf("materialization tenant namespace or role is invalid")
	}
	if _, err := uuid.Parse(b.JobUID); err != nil || b.Epoch <= 0 {
		return fmt.Errorf("materialization execution identity is invalid")
	}
	if _, err := uuid.Parse(b.ConnectionUID); err != nil || b.Generation <= 0 ||
		strings.TrimSpace(b.Connector) == "" || len(b.Connector) > 128 {
		return fmt.Errorf("materialization Connection identity is invalid")
	}
	if !revisionPattern.MatchString(b.DescriptorRevision) ||
		!revisionPattern.MatchString(b.CompilerRevision) ||
		!revisionPattern.MatchString(b.ConnectionSchemaRevision) {
		return fmt.Errorf("materialization revision metadata is invalid")
	}
	if b.Locator.Provider != "" {
		if err := b.Locator.Validate(); err != nil {
			return err
		}
	}
	previous := ""
	for _, setting := range b.Settings {
		if setting.Key <= previous {
			return fmt.Errorf("materialization settings must be unique and ordered")
		}
		previous = setting.Key
	}
	return nil
}

type ProviderReceipt struct {
	Kind         connection.ProviderKind
	ObjectUID    string
	VersionToken string
}

func (r ProviderReceipt) Validate() error {
	if r.Kind != connection.ProviderKubernetesSecretV1 && r.Kind != ProviderNone {
		return fmt.Errorf("provider receipt kind is invalid")
	}
	if strings.TrimSpace(r.ObjectUID) == "" ||
		len(r.ObjectUID) > 256 || strings.TrimSpace(r.VersionToken) == "" || len(r.VersionToken) > 256 {
		return fmt.Errorf("provider receipt is invalid")
	}
	return nil
}

type Values struct {
	fields  map[string][]byte
	receipt ProviderReceipt
	closed  bool
}

func NewValues(fields map[string][]byte, receipt ProviderReceipt) (*Values, error) {
	if err := receipt.Validate(); err != nil {
		return nil, err
	}
	if len(fields) == 0 || len(fields) > connection.MaximumSecretMappings {
		return nil, fmt.Errorf("provider value count is invalid")
	}
	copyFields := make(map[string][]byte, len(fields))
	for key, value := range fields {
		if strings.TrimSpace(key) == "" || len(value) == 0 || len(value) > connection.MaximumSettingValue {
			return nil, fmt.Errorf("provider value is invalid")
		}
		copyFields[key] = append([]byte(nil), value...)
	}
	return &Values{fields: copyFields, receipt: receipt}, nil
}

func (v *Values) Fields() (map[string][]byte, error) {
	if v == nil || v.closed {
		return nil, fmt.Errorf("provider values are closed")
	}
	result := make(map[string][]byte, len(v.fields))
	for key, value := range v.fields {
		result[key] = append([]byte(nil), value...)
	}
	return result, nil
}

func (v *Values) Receipt() (ProviderReceipt, error) {
	if v == nil || v.closed {
		return ProviderReceipt{}, fmt.Errorf("provider values are closed")
	}
	return v.receipt, nil
}

func (v *Values) Close() error {
	if v == nil || v.closed {
		return nil
	}
	for key, value := range v.fields {
		clear(value)
		delete(v.fields, key)
	}
	v.closed = true
	return nil
}

type Provider interface {
	Materialize(context.Context, Binding) (*Values, error)
}

type CredentialProvider interface {
	Resolve(context.Context, CredentialRequest) (*Values, error)
}

type Receipt struct {
	TenantID                 string
	JobUID                   string
	Epoch                    int64
	Role                     Role
	ConnectionUID            string
	Generation               int64
	DescriptorRevision       string
	Provider                 ProviderReceipt
	GeneratedSecretName      string
	GeneratedSecretUID       string
	GeneratedResourceVersion string
	CreatedAt                time.Time
}

func (r Receipt) Validate() error {
	if _, err := uuid.Parse(r.TenantID); err != nil {
		return fmt.Errorf("receipt tenant ID must be a UUID")
	}
	if _, err := uuid.Parse(r.JobUID); err != nil || r.Epoch <= 0 ||
		(r.Role != RoleSource && r.Role != RoleSink) {
		return fmt.Errorf("receipt execution identity is invalid")
	}
	if _, err := uuid.Parse(r.ConnectionUID); err != nil || r.Generation <= 0 ||
		!revisionPattern.MatchString(r.DescriptorRevision) {
		return fmt.Errorf("receipt Connection identity is invalid")
	}
	if err := r.Provider.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(r.GeneratedSecretName) == "" || len(r.GeneratedSecretName) > 253 ||
		strings.TrimSpace(r.GeneratedResourceVersion) == "" || len(r.GeneratedResourceVersion) > 256 ||
		r.CreatedAt.IsZero() {
		return fmt.Errorf("generated Secret receipt is invalid")
	}
	if _, err := uuid.Parse(r.GeneratedSecretUID); err != nil {
		return fmt.Errorf("generated Secret UID must be a UUID")
	}
	return nil
}

type Store interface {
	Load(context.Context, dispatch.Record, time.Time) ([]Binding, error)
	ClaimCleanup(context.Context, dispatch.Record, []Binding, time.Time) error
	Receipts(context.Context, dispatch.Identity) ([]Receipt, error)
	CommitReceipts(context.Context, dispatch.Record, []Receipt, time.Time) error
	CompleteCleanup(context.Context, dispatch.Identity, time.Time) error
}

func SortBindings(values []Binding) []Binding {
	result := append([]Binding(nil), values...)
	sort.Slice(result, func(left, right int) bool { return result[left].Role < result[right].Role })
	return result
}
