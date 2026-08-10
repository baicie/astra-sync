// Package connection owns tenant-scoped Connection identity and immutable generations.
package connection

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	MaximumSettings       = 256
	MaximumSettingValue   = 64 * 1024
	MaximumDescription    = 2048
	MaximumDisplayName    = 256
	MaximumSecretMappings = 64
)

var (
	ErrNotFound          = errors.New("connection not found")
	ErrAlreadyExists     = errors.New("connection already exists")
	ErrConflict          = errors.New("connection version conflict")
	ErrInvalidTransition = errors.New("invalid connection transition")
	ErrInUse             = errors.New("connection is in use")
	ErrIdempotencyReused = errors.New("idempotency key reused with another request")
	ErrTestNotFound      = errors.New("connection test not found")
	ErrTestLeaseLost     = errors.New("connection test lease lost")
	ErrTestLimitExceeded = errors.New("connection test admission limit exceeded")
)

var (
	dnsLabelPattern  = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)
	connectorPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?$`)
	optionPattern    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,127}$`)
	revisionPattern  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type State string

const (
	StateDisabled State = "DISABLED"
	StateActive   State = "ACTIVE"
)

type Compatibility string

const (
	CompatibilityCompatible           Compatibility = "COMPATIBLE"
	CompatibilityRevalidationRequired Compatibility = "REVALIDATION_REQUIRED"
	CompatibilityConnectorUnavailable Compatibility = "CONNECTOR_UNAVAILABLE"
)

type Sensitivity string

const (
	SensitivityPublic     Sensitivity = "PUBLIC"
	SensitivityRestricted Sensitivity = "RESTRICTED"
)

type ProviderKind string

const ProviderKubernetesSecretV1 ProviderKind = "KUBERNETES_SECRET_V1"

type Setting struct {
	Key         string      `json:"key"`
	Value       string      `json:"value"`
	Sensitivity Sensitivity `json:"sensitivity"`
}

type SecretFieldMapping struct {
	LogicalField string `json:"logicalField"`
	SecretKey    string `json:"secretKey"`
}

type SecretLocator struct {
	Provider   ProviderKind         `json:"provider"`
	SecretName string               `json:"secretName"`
	SecretUID  string               `json:"secretUid"`
	Fields     []SecretFieldMapping `json:"fields"`
}

func (l SecretLocator) Validate() error {
	if l.Provider != ProviderKubernetesSecretV1 || len(l.SecretName) == 0 || len(l.SecretName) > 63 ||
		!dnsLabelPattern.MatchString(l.SecretName) {
		return fmt.Errorf("Kubernetes Secret locator is invalid")
	}
	if _, err := uuid.Parse(l.SecretUID); err != nil {
		return fmt.Errorf("Kubernetes Secret UID must be a UUID")
	}
	if len(l.Fields) == 0 || len(l.Fields) > MaximumSecretMappings {
		return fmt.Errorf("Secret field mapping count is invalid")
	}
	previous := ""
	for _, field := range l.Fields {
		if !optionPattern.MatchString(field.LogicalField) || field.LogicalField <= previous ||
			len(field.SecretKey) == 0 || len(field.SecretKey) > 253 || strings.ContainsAny(field.SecretKey, "\r\n\x00") {
			return fmt.Errorf("Secret field mappings must be valid, unique, and ordered")
		}
		previous = field.LogicalField
	}
	return nil
}

func (l SecretLocator) Clone() SecretLocator {
	result := l
	result.Fields = append([]SecretFieldMapping(nil), l.Fields...)
	return result
}

type Generation struct {
	Number                   int64         `json:"number"`
	DescriptorRevision       string        `json:"descriptorRevision"`
	ConnectionSchemaRevision string        `json:"connectionSchemaRevision"`
	Settings                 []Setting     `json:"settings"`
	SecretLocator            SecretLocator `json:"secretLocator"`
	CreatedAt                time.Time     `json:"createdAt"`
}

func (g Generation) Validate() error {
	if g.Number <= 0 || !revisionPattern.MatchString(g.DescriptorRevision) ||
		!revisionPattern.MatchString(g.ConnectionSchemaRevision) || g.CreatedAt.IsZero() {
		return fmt.Errorf("Connection generation identity is invalid")
	}
	if len(g.Settings) > MaximumSettings {
		return fmt.Errorf("Connection setting count exceeds the supported limit")
	}
	previous := ""
	for _, setting := range g.Settings {
		if !optionPattern.MatchString(setting.Key) || setting.Key <= previous || len(setting.Value) > MaximumSettingValue ||
			(setting.Sensitivity != SensitivityPublic && setting.Sensitivity != SensitivityRestricted) {
			return fmt.Errorf("Connection settings must be bounded, unique, and ordered")
		}
		previous = setting.Key
	}
	if g.SecretLocator.Provider != "" {
		if err := g.SecretLocator.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (g Generation) Clone() Generation {
	result := g
	result.Settings = append([]Setting(nil), g.Settings...)
	result.SecretLocator = g.SecretLocator.Clone()
	return result
}

type Connection struct {
	TenantID    string     `json:"tenantId"`
	Name        string     `json:"name"`
	UID         string     `json:"uid"`
	Connector   string     `json:"connector"`
	Version     int64      `json:"version"`
	State       State      `json:"state"`
	DisplayName string     `json:"displayName"`
	Description string     `json:"description"`
	Current     Generation `json:"current"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

func New(
	tenantID, name, uid, connector, displayName, description string,
	generation Generation,
	now time.Time,
) (Connection, error) {
	result := Connection{
		TenantID: tenantID, Name: name, UID: uid, Connector: connector, Version: 1,
		State: StateDisabled, DisplayName: displayName, Description: description,
		Current: generation.Clone(), CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
	}
	if err := result.Validate(); err != nil {
		return Connection{}, err
	}
	return result, nil
}

func (c Connection) Validate() error {
	if _, err := uuid.Parse(c.TenantID); err != nil {
		return fmt.Errorf("tenant ID must be a UUID")
	}
	if len(c.Name) == 0 || len(c.Name) > 63 || !dnsLabelPattern.MatchString(c.Name) {
		return fmt.Errorf("Connection name must be a lowercase DNS label")
	}
	if _, err := uuid.Parse(c.UID); err != nil {
		return fmt.Errorf("Connection UID must be a UUID")
	}
	if len(c.Connector) == 0 || len(c.Connector) > 128 || !connectorPattern.MatchString(c.Connector) {
		return fmt.Errorf("connector name is invalid")
	}
	if c.Version <= 0 || c.Current.Number <= 0 || (c.State != StateDisabled && c.State != StateActive) {
		return fmt.Errorf("Connection version, generation, or state is invalid")
	}
	if len(c.DisplayName) > MaximumDisplayName || len(c.Description) > MaximumDescription ||
		strings.ContainsAny(c.DisplayName, "\x00") || strings.ContainsAny(c.Description, "\x00") {
		return fmt.Errorf("Connection display metadata exceeds supported bounds")
	}
	if err := c.Current.Validate(); err != nil {
		return err
	}
	if c.CreatedAt.IsZero() || c.UpdatedAt.IsZero() || c.UpdatedAt.Before(c.CreatedAt) ||
		c.Current.CreatedAt.Before(c.CreatedAt) || c.Current.CreatedAt.After(c.UpdatedAt) {
		return fmt.Errorf("Connection timestamps are invalid")
	}
	return nil
}

func (c Connection) Clone() Connection {
	result := c
	result.Current = c.Current.Clone()
	return result
}

func (c Connection) Replace(
	displayName, description string,
	settings []Setting,
	descriptorRevision, schemaRevision string,
	now time.Time,
) (Connection, bool, error) {
	next := c.Clone()
	next.DisplayName = displayName
	next.Description = description
	effectiveChanged := !equalSettings(c.Current.Settings, settings) ||
		c.Current.DescriptorRevision != descriptorRevision ||
		c.Current.ConnectionSchemaRevision != schemaRevision
	metadataChanged := c.DisplayName != displayName || c.Description != description
	if !effectiveChanged && !metadataChanged {
		return c.Clone(), false, nil
	}
	if effectiveChanged {
		if c.State != StateDisabled {
			return Connection{}, false, ErrInvalidTransition
		}
		next.Current = Generation{
			Number: c.Current.Number + 1, DescriptorRevision: descriptorRevision,
			ConnectionSchemaRevision: schemaRevision, Settings: append([]Setting(nil), settings...),
			SecretLocator: c.Current.SecretLocator.Clone(), CreatedAt: now.UTC(),
		}
	}
	next.Version++
	next.UpdatedAt = now.UTC()
	if err := next.Validate(); err != nil {
		return Connection{}, false, err
	}
	return next, true, nil
}

func (c Connection) Rotate(locator SecretLocator, descriptorRevision, schemaRevision string, now time.Time) (Connection, error) {
	if err := locator.Validate(); err != nil {
		return Connection{}, err
	}
	if equalLocator(c.Current.SecretLocator, locator) {
		return Connection{}, ErrInvalidTransition
	}
	next := c.Clone()
	next.Version++
	next.Current = Generation{
		Number: c.Current.Number + 1, DescriptorRevision: descriptorRevision,
		ConnectionSchemaRevision: schemaRevision, Settings: append([]Setting(nil), c.Current.Settings...),
		SecretLocator: locator.Clone(), CreatedAt: now.UTC(),
	}
	next.UpdatedAt = now.UTC()
	if err := next.Validate(); err != nil {
		return Connection{}, err
	}
	return next, nil
}

func (c Connection) SetState(target State, compatibility Compatibility, now time.Time) (Connection, bool, error) {
	if target != StateDisabled && target != StateActive {
		return Connection{}, false, ErrInvalidTransition
	}
	if c.State == target {
		return c.Clone(), false, nil
	}
	if target == StateActive && compatibility != CompatibilityCompatible {
		return Connection{}, false, ErrInvalidTransition
	}
	next := c.Clone()
	next.State = target
	next.Version++
	next.UpdatedAt = now.UTC()
	if err := next.Validate(); err != nil {
		return Connection{}, false, err
	}
	return next, true, nil
}

func equalSettings(left, right []Setting) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func equalLocator(left, right SecretLocator) bool {
	if left.Provider != right.Provider || left.SecretName != right.SecretName || left.SecretUID != right.SecretUID ||
		len(left.Fields) != len(right.Fields) {
		return false
	}
	for index := range left.Fields {
		if left.Fields[index] != right.Fields[index] {
			return false
		}
	}
	return true
}

func SortSettings(values []Setting) []Setting {
	result := append([]Setting(nil), values...)
	sort.Slice(result, func(left, right int) bool { return result[left].Key < result[right].Key })
	return result
}
