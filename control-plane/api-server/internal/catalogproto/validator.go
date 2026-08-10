// Package catalogproto validates and projects the deployment connector protobuf inventory.
package catalogproto

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/catalog"
)

const (
	inventorySchemaVersion  = 1
	descriptorSchemaVersion = 1
)

var (
	revisionPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	namePattern     = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]{0,126}[a-z0-9])?$`)
	optionPattern   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,127}$`)
)

type Validator struct{}

func (Validator) Validate(payload []byte, activatedAt time.Time) (catalog.Snapshot, error) {
	if len(payload) == 0 || len(payload) > 4*1024*1024 {
		return catalog.Snapshot{}, fmt.Errorf("inventory payload is outside the supported size range")
	}
	inventory := &controlv1.ConnectorInventory{}
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(payload, inventory); err != nil {
		return catalog.Snapshot{}, fmt.Errorf("decode inventory: %w", err)
	}
	if len(inventory.ProtoReflect().GetUnknown()) != 0 {
		return catalog.Snapshot{}, fmt.Errorf("inventory contains unknown fields")
	}
	canonical, err := deterministic(inventory)
	if err != nil {
		return catalog.Snapshot{}, err
	}
	if !bytes.Equal(canonical, payload) {
		return catalog.Snapshot{}, fmt.Errorf("inventory is not in canonical deterministic protobuf form")
	}
	if inventory.GetInventorySchemaVersion() != inventorySchemaVersion {
		return catalog.Snapshot{}, fmt.Errorf("unsupported inventory schema version")
	}
	if strings.TrimSpace(inventory.GetExecutionProfile()) == "" ||
		strings.TrimSpace(inventory.GetJobSpecSchemaRevision()) == "" ||
		strings.TrimSpace(inventory.GetCompilerBuild()) == "" {
		return catalog.Snapshot{}, fmt.Errorf("inventory compiler identity is incomplete")
	}
	if len(inventory.GetDescriptors()) == 0 || len(inventory.GetDescriptors()) > 256 {
		return catalog.Snapshot{}, fmt.Errorf("inventory descriptor count is outside the supported range")
	}

	snapshot := catalog.Snapshot{
		InventoryRevision: inventory.GetInventoryRevision(),
		CompilerRevision:  inventory.GetCompilerRevision(),
		ExecutionProfile:  inventory.GetExecutionProfile(),
		Payload:           bytes.Clone(payload),
		ActivatedAt:       activatedAt.UTC(),
	}
	previousName := ""
	identity := &controlv1.ConnectorInventoryIdentity{}
	for _, descriptor := range inventory.GetDescriptors() {
		if err := validateDescriptor(descriptor); err != nil {
			return catalog.Snapshot{}, fmt.Errorf("descriptor %q: %w", descriptor.GetName(), err)
		}
		if descriptor.GetName() <= previousName {
			return catalog.Snapshot{}, fmt.Errorf("descriptors must be ordered by unique canonical name")
		}
		previousName = descriptor.GetName()
		descriptorPayload, err := deterministic(descriptor)
		if err != nil {
			return catalog.Snapshot{}, err
		}
		snapshot.Descriptors = append(snapshot.Descriptors, catalog.DescriptorSnapshot{
			Revision:        descriptor.GetDescriptorRevision(),
			Name:            descriptor.GetName(),
			ArtifactVersion: descriptor.GetArtifactVersion(),
			Payload:         descriptorPayload,
		})
		identity.Entries = append(identity.Entries, &controlv1.ConnectorInventoryEntry{
			Name:               descriptor.GetName(),
			ArtifactVersion:    descriptor.GetArtifactVersion(),
			DescriptorRevision: descriptor.GetDescriptorRevision(),
		})
	}
	if got, err := digest(identity); err != nil || got != inventory.GetInventoryRevision() {
		return catalog.Snapshot{}, fmt.Errorf("inventory revision does not match canonical content")
	}
	compilerIdentity := &controlv1.ConnectorCompilerIdentity{
		InventoryRevision:     inventory.GetInventoryRevision(),
		JobSpecSchemaRevision: inventory.GetJobSpecSchemaRevision(),
		CompilerBuild:         inventory.GetCompilerBuild(),
		ExecutionProfile:      inventory.GetExecutionProfile(),
	}
	if got, err := digest(compilerIdentity); err != nil || got != inventory.GetCompilerRevision() {
		return catalog.Snapshot{}, fmt.Errorf("compiler revision does not match canonical identity")
	}
	if err := snapshot.Validate(); err != nil {
		return catalog.Snapshot{}, err
	}
	return snapshot, nil
}

func ParseSnapshot(snapshot catalog.Snapshot) (*controlv1.ConnectorInventory, error) {
	if err := snapshot.Validate(); err != nil {
		return nil, err
	}
	inventory := &controlv1.ConnectorInventory{}
	if err := proto.Unmarshal(snapshot.Payload, inventory); err != nil {
		return nil, fmt.Errorf("decode stored connector inventory: %w", err)
	}
	if inventory.GetInventoryRevision() != snapshot.InventoryRevision ||
		inventory.GetCompilerRevision() != snapshot.CompilerRevision ||
		inventory.GetExecutionProfile() != snapshot.ExecutionProfile {
		return nil, fmt.Errorf("stored connector inventory metadata mismatch")
	}
	return inventory, nil
}

func validateDescriptor(descriptor *controlv1.ConnectorDescriptor) error {
	if descriptor == nil || len(descriptor.ProtoReflect().GetUnknown()) != 0 {
		return fmt.Errorf("descriptor is nil or contains unknown fields")
	}
	if descriptor.GetDescriptorSchemaVersion() != descriptorSchemaVersion ||
		!namePattern.MatchString(descriptor.GetName()) ||
		strings.TrimSpace(descriptor.GetArtifactVersion()) == "" ||
		strings.TrimSpace(descriptor.GetDisplayName()) == "" ||
		strings.TrimSpace(descriptor.GetDescriptionKey()) == "" {
		return fmt.Errorf("descriptor identity is invalid")
	}
	if !revisionPattern.MatchString(descriptor.GetDescriptorRevision()) ||
		!revisionPattern.MatchString(descriptor.GetConnectionSchemaRevision()) {
		return fmt.Errorf("descriptor revisions are invalid")
	}
	if err := validateOrderedEnumSet(descriptor.GetRoles(), controlv1.ConnectorRole_CONNECTOR_ROLE_UNSPECIFIED); err != nil {
		return fmt.Errorf("roles: %w", err)
	}
	if len(descriptor.GetRoles()) == 0 {
		return fmt.Errorf("roles must not be empty")
	}
	if err := validateOrderedEnumSet(
		descriptor.GetCapabilities(), controlv1.ConnectorCapability_CONNECTOR_CAPABILITY_UNSPECIFIED,
	); err != nil {
		return fmt.Errorf("capabilities: %w", err)
	}
	if err := validateDerivedFields(descriptor); err != nil {
		return err
	}
	if err := validateOptions(descriptor); err != nil {
		return err
	}
	if err := validateRequirements(descriptor); err != nil {
		return err
	}
	if !sort.StringsAreSorted(descriptor.GetAcceptedConnectionSchemaRevisions()) ||
		!containsString(descriptor.GetAcceptedConnectionSchemaRevisions(), descriptor.GetConnectionSchemaRevision()) {
		return fmt.Errorf("accepted Connection schema revisions are not canonical")
	}
	for _, revision := range descriptor.GetAcceptedConnectionSchemaRevisions() {
		if !revisionPattern.MatchString(revision) {
			return fmt.Errorf("accepted Connection schema revision is invalid")
		}
	}

	schema := &controlv1.ConnectorConnectionSchemaIdentity{ConnectorName: descriptor.GetName()}
	for _, option := range descriptor.GetOptions() {
		if option.GetOwner() == controlv1.ConnectorOptionOwner_CONNECTOR_OPTION_OWNER_CONNECTION {
			schema.Options = append(schema.Options, proto.Clone(option).(*controlv1.ConnectorOptionDefinition))
		}
	}
	for _, prefix := range descriptor.GetOptionPrefixes() {
		if prefix.GetOwner() == controlv1.ConnectorOptionOwner_CONNECTOR_OPTION_OWNER_CONNECTION {
			schema.OptionPrefixes = append(schema.OptionPrefixes, proto.Clone(prefix).(*controlv1.ConnectorOptionPrefix))
		}
	}
	for _, requirement := range descriptor.GetConnectionRequirements() {
		schema.ConnectionRequirements = append(
			schema.ConnectionRequirements,
			proto.Clone(requirement).(*controlv1.ConnectorRoleConnectionRequirement),
		)
	}
	if got, err := digest(schema); err != nil || got != descriptor.GetConnectionSchemaRevision() {
		return fmt.Errorf("Connection schema revision does not match canonical content")
	}
	canonicalDescriptor := proto.Clone(descriptor).(*controlv1.ConnectorDescriptor)
	canonicalDescriptor.DescriptorRevision = ""
	if got, err := digest(canonicalDescriptor); err != nil || got != descriptor.GetDescriptorRevision() {
		return fmt.Errorf("descriptor revision does not match canonical content")
	}
	return nil
}

func validateOptions(descriptor *controlv1.ConnectorDescriptor) error {
	previous := ""
	roles := enumSet(descriptor.GetRoles())
	for _, option := range descriptor.GetOptions() {
		if option == nil || len(option.ProtoReflect().GetUnknown()) != 0 ||
			!optionPattern.MatchString(option.GetKey()) || option.GetKey() <= previous {
			return fmt.Errorf("options must use unique canonical key ordering")
		}
		previous = option.GetKey()
		if err := validateOrderedEnumSet(option.GetRoles(), controlv1.ConnectorRole_CONNECTOR_ROLE_UNSPECIFIED); err != nil ||
			len(option.GetRoles()) == 0 || !subset(enumSet(option.GetRoles()), roles) {
			return fmt.Errorf("option %q roles are invalid", option.GetKey())
		}
		if option.GetOwner() == controlv1.ConnectorOptionOwner_CONNECTOR_OPTION_OWNER_UNSPECIFIED ||
			option.GetValueType() == controlv1.ConnectorOptionType_CONNECTOR_OPTION_TYPE_UNSPECIFIED ||
			option.GetSensitivity() == controlv1.ConnectorOptionSensitivity_CONNECTOR_OPTION_SENSITIVITY_UNSPECIFIED {
			return fmt.Errorf("option %q has unspecified policy", option.GetKey())
		}
		if option.GetSensitivity() != controlv1.ConnectorOptionSensitivity_CONNECTOR_OPTION_SENSITIVITY_PUBLIC &&
			(option.GetOwner() != controlv1.ConnectorOptionOwner_CONNECTOR_OPTION_OWNER_CONNECTION || option.DefaultValue != nil) {
			return fmt.Errorf("option %q has unsafe sensitive ownership or default", option.GetKey())
		}
		if option.GetValueType() == controlv1.ConnectorOptionType_CONNECTOR_OPTION_TYPE_ENUM {
			if len(option.GetEnumValues()) == 0 || hasBlankOrDuplicate(option.GetEnumValues()) {
				return fmt.Errorf("option %q has invalid enum values", option.GetKey())
			}
		} else if len(option.GetEnumValues()) != 0 {
			return fmt.Errorf("option %q has enum values for a non-enum type", option.GetKey())
		}
		if option.Minimum != nil && option.Maximum != nil && option.GetMinimum() > option.GetMaximum() ||
			option.MinLength != nil && option.MaxLength != nil && option.GetMinLength() > option.GetMaxLength() {
			return fmt.Errorf("option %q has inverted bounds", option.GetKey())
		}
	}
	previous = ""
	for _, prefix := range descriptor.GetOptionPrefixes() {
		if prefix == nil || len(prefix.ProtoReflect().GetUnknown()) != 0 ||
			!strings.HasSuffix(prefix.GetPrefix(), ".") || prefix.GetPrefix() <= previous ||
			prefix.GetMaxEntries() <= 0 || prefix.GetMaxEntries() > 256 ||
			prefix.GetMaxValueLength() <= 0 || prefix.GetMaxValueLength() > 65536 {
			return fmt.Errorf("option prefixes are invalid or not canonically ordered")
		}
		previous = prefix.GetPrefix()
		if prefix.GetSensitivity() != controlv1.ConnectorOptionSensitivity_CONNECTOR_OPTION_SENSITIVITY_PUBLIC &&
			prefix.GetOwner() != controlv1.ConnectorOptionOwner_CONNECTOR_OPTION_OWNER_CONNECTION {
			return fmt.Errorf("option prefix %q has unsafe sensitive ownership", prefix.GetPrefix())
		}
		for _, option := range descriptor.GetOptions() {
			if strings.HasPrefix(option.GetKey(), prefix.GetPrefix()) {
				return fmt.Errorf("option prefix %q overlaps an exact key", prefix.GetPrefix())
			}
		}
	}
	return nil
}

func validateRequirements(descriptor *controlv1.ConnectorDescriptor) error {
	requirements := descriptor.GetConnectionRequirements()
	if len(requirements) != 2 ||
		requirements[0].GetRole() != controlv1.ConnectorRole_CONNECTOR_ROLE_SOURCE ||
		requirements[1].GetRole() != controlv1.ConnectorRole_CONNECTOR_ROLE_SINK {
		return fmt.Errorf("Connection requirements must contain SOURCE and SINK in enum order")
	}
	for _, requirement := range requirements {
		if requirement.GetRequirement() == controlv1.ConnectionRequirement_CONNECTION_REQUIREMENT_UNSPECIFIED {
			return fmt.Errorf("Connection requirement is unspecified")
		}
	}
	return nil
}

func validateDerivedFields(descriptor *controlv1.ConnectorDescriptor) error {
	capabilities := enumSet(descriptor.GetCapabilities())
	expectedModes := []controlv1.ConnectorExecutionMode{}
	if capabilities[int32(controlv1.ConnectorCapability_CONNECTOR_CAPABILITY_BATCH_READ)] ||
		capabilities[int32(controlv1.ConnectorCapability_CONNECTOR_CAPABILITY_BATCH_WRITE)] {
		expectedModes = append(expectedModes, controlv1.ConnectorExecutionMode_CONNECTOR_EXECUTION_MODE_BATCH)
	}
	if capabilities[int32(controlv1.ConnectorCapability_CONNECTOR_CAPABILITY_CHANGE_DATA_CAPTURE)] ||
		(capabilities[int32(controlv1.ConnectorCapability_CONNECTOR_CAPABILITY_UPSERT)] &&
			capabilities[int32(controlv1.ConnectorCapability_CONNECTOR_CAPABILITY_DELETE)]) {
		expectedModes = append(expectedModes, controlv1.ConnectorExecutionMode_CONNECTOR_EXECUTION_MODE_CDC)
	}
	if !equalEnums(descriptor.GetExecutionModes(), expectedModes) {
		return fmt.Errorf("execution modes do not match capabilities")
	}
	expectedDelivery := []controlv1.ConnectorDeliveryConstraint{
		controlv1.ConnectorDeliveryConstraint_CONNECTOR_DELIVERY_CONSTRAINT_AT_MOST_ONCE,
	}
	for capability, constraint := range map[controlv1.ConnectorCapability]controlv1.ConnectorDeliveryConstraint{
		controlv1.ConnectorCapability_CONNECTOR_CAPABILITY_REPLAYABLE_OFFSET:    controlv1.ConnectorDeliveryConstraint_CONNECTOR_DELIVERY_CONSTRAINT_REPLAYABLE_SOURCE,
		controlv1.ConnectorCapability_CONNECTOR_CAPABILITY_EXACTLY_ONCE_SOURCE:  controlv1.ConnectorDeliveryConstraint_CONNECTOR_DELIVERY_CONSTRAINT_EXACTLY_ONCE_SOURCE,
		controlv1.ConnectorCapability_CONNECTOR_CAPABILITY_IDEMPOTENT_WRITE:     controlv1.ConnectorDeliveryConstraint_CONNECTOR_DELIVERY_CONSTRAINT_IDEMPOTENT_SINK,
		controlv1.ConnectorCapability_CONNECTOR_CAPABILITY_TRANSACTIONAL_COMMIT: controlv1.ConnectorDeliveryConstraint_CONNECTOR_DELIVERY_CONSTRAINT_TRANSACTIONAL_SINK,
	} {
		if capabilities[int32(capability)] {
			expectedDelivery = append(expectedDelivery, constraint)
		}
	}
	sort.Slice(expectedDelivery, func(left, right int) bool { return expectedDelivery[left] < expectedDelivery[right] })
	if !equalEnums(descriptor.GetDeliveryConstraints(), expectedDelivery) {
		return fmt.Errorf("delivery constraints do not match capabilities")
	}
	return nil
}

func deterministic(message proto.Message) ([]byte, error) {
	result, err := (proto.MarshalOptions{Deterministic: true}).Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("serialize canonical connector metadata: %w", err)
	}
	return result, nil
}

func digest(message proto.Message) (string, error) {
	payload, err := deterministic(message)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("sha256:%x", sum), nil
}

type integerEnum interface{ ~int32 }

func validateOrderedEnumSet[E integerEnum](values []E, unspecified E) error {
	previous := E(-1)
	for _, value := range values {
		if value == unspecified || value <= previous {
			return fmt.Errorf("values must be specified, unique, and in enum order")
		}
		previous = value
	}
	return nil
}

func enumSet[E integerEnum](values []E) map[int32]bool {
	result := make(map[int32]bool, len(values))
	for _, value := range values {
		result[int32(value)] = true
	}
	return result
}

func subset(values, superset map[int32]bool) bool {
	for value := range values {
		if !superset[value] {
			return false
		}
	}
	return true
}

func equalEnums[E integerEnum](left, right []E) bool {
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

func hasBlankOrDuplicate(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return true
		}
		if _, duplicate := seen[value]; duplicate {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
