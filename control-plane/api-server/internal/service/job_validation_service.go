package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	compilerv1 "io.astrasync/control-plane/api-server/gen/go/compiler/v1"
	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/api-server/internal/catalogproto"
	"io.astrasync/control-plane/auth"
	"io.astrasync/control-plane/catalog"
	"io.astrasync/control-plane/connection"
	"io.astrasync/control-plane/job"
)

const maximumValidationIssues = 32

type JobCompilerValidator interface {
	Validate(context.Context, *compilerv1.ValidateRequest) (*compilerv1.ValidateResponse, error)
}

type JobValidationService struct {
	controlv1.UnimplementedJobValidationServiceServer
	jobs                     job.Repository
	connections              connection.Repository
	catalog                  catalog.Repository
	authorizer               auth.Authorizer
	compiler                 JobCompilerValidator
	executionProfile         string
	validationID             func() string
	connectionRuntimeEnabled bool
}

type MutationValidation struct {
	Result *controlv1.JobValidationResult
	Fence  job.ValidationFence
}

type JobValidationServiceOption func(*JobValidationService) error

func WithConnectionRuntimeEnabled(enabled bool) JobValidationServiceOption {
	return func(service *JobValidationService) error {
		service.connectionRuntimeEnabled = enabled
		return nil
	}
}

func NewJobValidationService(
	jobs job.Repository,
	connections connection.Repository,
	catalogRepository catalog.Repository,
	authorizer auth.Authorizer,
	compiler JobCompilerValidator,
	executionProfile string,
	validationID func() string,
	options ...JobValidationServiceOption,
) (*JobValidationService, error) {
	if jobs == nil || connections == nil || catalogRepository == nil || authorizer == nil || compiler == nil ||
		validationID == nil || strings.TrimSpace(executionProfile) == "" {
		return nil, fmt.Errorf("Job validation service dependencies must not be nil or blank")
	}
	result := &JobValidationService{
		jobs: jobs, connections: connections, catalog: catalogRepository,
		authorizer: authorizer, compiler: compiler, executionProfile: executionProfile,
		validationID: validationID,
	}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("Job validation service option must not be nil")
		}
		if err := option(result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *JobValidationService) ValidateJobSpec(
	ctx context.Context, request *controlv1.ValidateJobSpecRequest,
) (*controlv1.JobValidationResult, error) {
	validation, err := s.ValidateForMutation(ctx, request)
	if err != nil {
		return nil, err
	}
	return validation.Result, nil
}

// ValidateForMutation performs the same public canonical validation and also
// returns the metadata-only fence consumed by the atomic Job repository.
func (s *JobValidationService) ValidateForMutation(
	ctx context.Context, request *controlv1.ValidateJobSpecRequest,
) (MutationValidation, error) {
	if request == nil {
		return MutationValidation{}, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	key, err := requestKey(request.GetNamespace(), request.GetName())
	if err != nil {
		return MutationValidation{}, err
	}
	snapshot, err := s.catalog.Current(ctx, s.executionProfile)
	if err != nil {
		return MutationValidation{}, status.Error(codes.Unavailable, "connector catalog is unavailable")
	}
	inventory, err := catalogproto.ParseSnapshot(snapshot)
	if err != nil {
		return MutationValidation{}, status.Error(codes.Internal, "active connector catalog is invalid")
	}

	spec, versionErr := s.specForPurpose(ctx, request, key)
	if versionErr != nil {
		return MutationValidation{}, versionErr
	}
	result := &controlv1.JobValidationResult{
		ValidationId:     s.validationID(),
		CompilerRevision: inventory.GetCompilerRevision(),
	}
	if spec != nil {
		result.SpecDigest = deterministicSpecDigest(spec)
	}
	domainSpec, structuralErr := fromProtoSpec(spec)
	if structuralErr != nil {
		result.Issues = []*controlv1.JobValidationIssue{publicIssue(
			controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_STRUCTURE_INVALID,
			"spec", "job specification is structurally invalid", "",
		)}
		return MutationValidation{Result: result}, nil
	}

	descriptors := make(map[string]*controlv1.ConnectorDescriptor, len(inventory.GetDescriptors()))
	for _, descriptor := range inventory.GetDescriptors() {
		descriptors[descriptor.GetName()] = descriptor
	}
	localIssues := make([]*controlv1.JobValidationIssue, 0)
	tenantID, tenantErr := tenantIDForConnectionUse(ctx, request.GetNamespace())
	needsConnectionUse := domainSpec.Source.ConnectionRef != "" || domainSpec.Sink.ConnectionRef != ""
	if needsConnectionUse &&
		request.GetPurpose() == controlv1.JobValidationPurpose_JOB_VALIDATION_PURPOSE_START &&
		!s.connectionRuntimeEnabled {
		return MutationValidation{}, status.Error(
			codes.FailedPrecondition, "Connection-backed Job starts are disabled by rollout policy")
	}
	if needsConnectionUse {
		if tenantErr != nil {
			return MutationValidation{}, status.Error(codes.PermissionDenied, "tenant access denied")
		}
		if _, err := s.authorizer.Authorize(ctx, tenantID, auth.PermissionConnectionsUse); err != nil {
			if errors.Is(err, auth.ErrUnauthenticated) {
				return MutationValidation{}, status.Error(codes.Unauthenticated, "authentication required")
			}
			return MutationValidation{}, status.Error(codes.PermissionDenied, "tenant access denied")
		}
	}

	source, sourceBinding := s.effectiveConnector(
		ctx, tenantID, domainSpec.Source, descriptors[domainSpec.Source.Connector],
		controlv1.ConnectorRole_CONNECTOR_ROLE_SOURCE, "spec.source", &localIssues,
	)
	sink, sinkBinding := s.effectiveConnector(
		ctx, tenantID, domainSpec.Sink, descriptors[domainSpec.Sink.Connector],
		controlv1.ConnectorRole_CONNECTOR_ROLE_SINK, "spec.sink", &localIssues,
	)
	if len(localIssues) != 0 {
		result.Issues = boundedPublicIssues(localIssues)
		return MutationValidation{Result: result}, nil
	}

	compilerRequest := &compilerv1.ValidateRequest{
		Name:                     key.Name,
		Source:                   source,
		Sink:                     sink,
		Transforms:               compilerTransforms(domainSpec.Transforms),
		DeliveryGuarantee:        compilerDelivery(domainSpec.Delivery.Guarantee),
		MaxBatchRecords:          domainSpec.Runtime.MaxBatchRecords,
		ExecutionProfile:         s.executionProfile,
		ExpectedCompilerRevision: inventory.GetCompilerRevision(),
	}
	compilerResult, err := s.compiler.Validate(ctx, compilerRequest)
	if err != nil {
		return MutationValidation{}, status.Error(codes.Unavailable, "canonical validator is unavailable")
	}
	if compilerResult.GetCompilerRevision() != inventory.GetCompilerRevision() ||
		compilerResult.GetInventoryRevision() != inventory.GetInventoryRevision() {
		return MutationValidation{}, status.Error(codes.Unavailable, "canonical validator revision does not match the active catalog")
	}
	result.ExecutionMode = publicExecutionMode(compilerResult.GetExecutionMode())
	result.Issues = make([]*controlv1.JobValidationIssue, 0, len(compilerResult.GetIssues()))
	for _, issue := range compilerResult.GetIssues() {
		result.Issues = append(result.Issues, mapCompilerIssue(issue))
	}
	result.Issues = boundedPublicIssues(result.Issues)
	result.Valid = compilerResult.GetValid() && len(result.Issues) == 0
	validation := MutationValidation{Result: result}
	if result.Valid {
		bindings := make([]job.ConnectionBinding, 0, 2)
		if sourceBinding != nil {
			bindings = append(bindings, *sourceBinding)
		}
		if sinkBinding != nil {
			bindings = append(bindings, *sinkBinding)
		}
		validation.Fence = job.ValidationFence{
			ValidationID: result.GetValidationId(), SpecDigest: result.GetSpecDigest(),
			CompilerRevision: result.GetCompilerRevision(), Bindings: bindings,
		}
	}
	return validation, nil
}

func (s *JobValidationService) specForPurpose(
	ctx context.Context, request *controlv1.ValidateJobSpecRequest, key job.Key,
) (*controlv1.JobSpec, error) {
	switch request.GetPurpose() {
	case controlv1.JobValidationPurpose_JOB_VALIDATION_PURPOSE_CREATE:
		if request.GetExpectedVersion() != 0 || request.GetSpec() == nil {
			return nil, status.Error(codes.InvalidArgument, "CREATE requires a complete spec and no expected version")
		}
		return proto.Clone(request.GetSpec()).(*controlv1.JobSpec), nil
	case controlv1.JobValidationPurpose_JOB_VALIDATION_PURPOSE_UPDATE:
		if request.GetExpectedVersion() <= 0 || request.GetSpec() == nil {
			return nil, status.Error(codes.InvalidArgument, "UPDATE requires a positive expected version and complete spec")
		}
		stored, err := s.jobs.Get(ctx, key)
		if err != nil {
			return nil, repositoryError(err)
		}
		if stored.Version != request.GetExpectedVersion() {
			return nil, repositoryError(job.ErrConflict)
		}
		return proto.Clone(request.GetSpec()).(*controlv1.JobSpec), nil
	case controlv1.JobValidationPurpose_JOB_VALIDATION_PURPOSE_START:
		if request.GetExpectedVersion() <= 0 || request.GetSpec() != nil {
			return nil, status.Error(codes.InvalidArgument, "START requires a positive expected version and no caller spec")
		}
		stored, err := s.jobs.Get(ctx, key)
		if err != nil {
			return nil, repositoryError(err)
		}
		if stored.Version != request.GetExpectedVersion() {
			return nil, repositoryError(job.ErrConflict)
		}
		return toProtoSpec(stored.Spec), nil
	default:
		return nil, status.Error(codes.InvalidArgument, "validation purpose is required")
	}
}

func (s *JobValidationService) effectiveConnector(
	ctx context.Context,
	tenantID string,
	spec job.ConnectorSpec,
	descriptor *controlv1.ConnectorDescriptor,
	role controlv1.ConnectorRole,
	path string,
	issues *[]*controlv1.JobValidationIssue,
) (*compilerv1.EffectiveConnectorConfig, *job.ConnectionBinding) {
	result := &compilerv1.EffectiveConnectorConfig{Connector: spec.Connector}
	if descriptor == nil {
		*issues = append(*issues, publicIssue(
			controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_CONNECTOR_NOT_FOUND,
			path+".connector", "connector is not available in this deployment", "",
		))
		return result, nil
	}
	result.DescriptorRevision = descriptor.GetDescriptorRevision()
	acceptedJobOptions := make(map[string]string)
	for key, value := range spec.Options {
		definition := descriptorOption(descriptor, key)
		if definition == nil {
			*issues = append(*issues, publicIssue(
				controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_OPTION_UNKNOWN,
				path+".options", "connector option is not declared by the active descriptor", "",
			))
			continue
		}
		if definition.GetOwner() != controlv1.ConnectorOptionOwner_CONNECTOR_OPTION_OWNER_JOB ||
			definition.GetSensitivity() != controlv1.ConnectorOptionSensitivity_CONNECTOR_OPTION_SENSITIVITY_PUBLIC ||
			!containsRole(definition.GetRoles(), role) {
			*issues = append(*issues, publicIssue(
				controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_OPTION_INVALID,
				path+".options", "connector option is not allowed in JobSpec", definition.GetHelpKey(),
			))
			continue
		}
		acceptedJobOptions[key] = value
	}
	result.JobOptions = acceptedJobOptions
	if spec.ConnectionRef == "" {
		return result, nil
	}
	stored, err := s.connections.Get(ctx, tenantID, spec.ConnectionRef)
	if err != nil || stored.State != connection.StateActive || stored.Connector != spec.Connector ||
		!containsStringValue(descriptor.GetAcceptedConnectionSchemaRevisions(), stored.Current.ConnectionSchemaRevision) {
		*issues = append(*issues, publicIssue(
			controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_CONNECTION_REF_UNAVAILABLE,
			path+".connection_ref", "Connection is unavailable for this connector role", "",
		))
		return result, nil
	}
	if !containsRole(descriptor.GetRoles(), role) {
		*issues = append(*issues, publicIssue(
			controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_ROLE_UNSUPPORTED,
			path+".connector", "connector does not support this role", "",
		))
		return result, nil
	}
	result.ConnectionConfigured = true
	result.ConnectionSchemaRevision = stored.Current.ConnectionSchemaRevision
	result.ConnectionSettings = make(map[string]string, len(stored.Current.Settings))
	for _, setting := range stored.Current.Settings {
		result.ConnectionSettings[setting.Key] = setting.Value
	}
	result.ConfiguredSecretFields = make([]string, 0, len(stored.Current.SecretLocator.Fields))
	for _, field := range stored.Current.SecretLocator.Fields {
		result.ConfiguredSecretFields = append(result.ConfiguredSecretFields, field.LogicalField)
	}
	sort.Strings(result.ConfiguredSecretFields)
	roleValue := job.ConnectionRoleSource
	if role == controlv1.ConnectorRole_CONNECTOR_ROLE_SINK {
		roleValue = job.ConnectionRoleSink
	}
	binding := &job.ConnectionBinding{
		Role: roleValue, TenantID: tenantID, ReferenceName: spec.ConnectionRef,
		ConnectionUID: stored.UID, Connector: stored.Connector, Generation: stored.Current.Number,
		DescriptorRevision:       descriptor.GetDescriptorRevision(),
		ConnectionSchemaRevision: stored.Current.ConnectionSchemaRevision,
	}
	return result, binding
}

func descriptorOption(
	descriptor *controlv1.ConnectorDescriptor, key string,
) *controlv1.ConnectorOptionDefinition {
	for _, option := range descriptor.GetOptions() {
		if option.GetKey() == key {
			return option
		}
	}
	for _, prefix := range descriptor.GetOptionPrefixes() {
		if strings.HasPrefix(key, prefix.GetPrefix()) {
			return &controlv1.ConnectorOptionDefinition{
				Key: key, Roles: append([]controlv1.ConnectorRole(nil), prefix.GetRoles()...),
				Owner: prefix.GetOwner(), Sensitivity: prefix.GetSensitivity(),
				ValueType: controlv1.ConnectorOptionType_CONNECTOR_OPTION_TYPE_STRING,
			}
		}
	}
	return nil
}

func containsRole(values []controlv1.ConnectorRole, expected controlv1.ConnectorRole) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func tenantIDForConnectionUse(ctx context.Context, scope string) (string, error) {
	principal, found := auth.PrincipalFromContext(ctx)
	if found {
		membership, found := principal.MembershipForScope(scope)
		if !found || !membership.Active {
			return "", auth.ErrTenantUnavailable
		}
		return membership.TenantID, nil
	}
	if _, err := auth.NewMembership(scope, true); err != nil {
		return "", auth.ErrTenantUnavailable
	}
	return scope, nil
}

func deterministicSpecDigest(spec *controlv1.JobSpec) string {
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(spec)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(payload)
	return fmt.Sprintf("sha256:%x", digest)
}

func compilerTransforms(values []job.TransformSpec) []*compilerv1.CompilerTransform {
	result := make([]*compilerv1.CompilerTransform, len(values))
	for index, value := range values {
		result[index] = &compilerv1.CompilerTransform{Type: value.Type, Options: cloneStringMap(value.Options)}
	}
	return result
}

func compilerDelivery(value job.DeliveryGuarantee) compilerv1.CompilerDeliveryGuarantee {
	switch value {
	case job.DeliveryExactlyOnce:
		return compilerv1.CompilerDeliveryGuarantee_COMPILER_DELIVERY_GUARANTEE_EXACTLY_ONCE
	case job.DeliveryAtLeastOnce:
		return compilerv1.CompilerDeliveryGuarantee_COMPILER_DELIVERY_GUARANTEE_AT_LEAST_ONCE
	case job.DeliveryAtMostOnce:
		return compilerv1.CompilerDeliveryGuarantee_COMPILER_DELIVERY_GUARANTEE_AT_MOST_ONCE
	default:
		return compilerv1.CompilerDeliveryGuarantee_COMPILER_DELIVERY_GUARANTEE_UNSPECIFIED
	}
}

func publicExecutionMode(value compilerv1.CompilerExecutionMode) controlv1.ConnectorExecutionMode {
	switch value {
	case compilerv1.CompilerExecutionMode_COMPILER_EXECUTION_MODE_BATCH:
		return controlv1.ConnectorExecutionMode_CONNECTOR_EXECUTION_MODE_BATCH
	case compilerv1.CompilerExecutionMode_COMPILER_EXECUTION_MODE_CDC:
		return controlv1.ConnectorExecutionMode_CONNECTOR_EXECUTION_MODE_CDC
	default:
		return controlv1.ConnectorExecutionMode_CONNECTOR_EXECUTION_MODE_UNSPECIFIED
	}
}

func mapCompilerIssue(source *compilerv1.CompilerValidationIssue) *controlv1.JobValidationIssue {
	code := mapCompilerIssueCode(source.GetCode())
	return publicIssue(code, source.GetFieldPath(), publicIssueMessage(code), source.GetDocumentationKey())
}

func mapCompilerIssueCode(value compilerv1.CompilerValidationIssueCode) controlv1.JobValidationIssueCode {
	switch value {
	case compilerv1.CompilerValidationIssueCode_COMPILER_VALIDATION_ISSUE_CODE_CONNECTOR_NOT_FOUND:
		return controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_CONNECTOR_NOT_FOUND
	case compilerv1.CompilerValidationIssueCode_COMPILER_VALIDATION_ISSUE_CODE_ROLE_UNSUPPORTED:
		return controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_ROLE_UNSUPPORTED
	case compilerv1.CompilerValidationIssueCode_COMPILER_VALIDATION_ISSUE_CODE_CAPABILITY_MISSING:
		return controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_CAPABILITY_MISSING
	case compilerv1.CompilerValidationIssueCode_COMPILER_VALIDATION_ISSUE_CODE_TRANSFORM_UNSUPPORTED:
		return controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_TRANSFORM_UNSUPPORTED
	case compilerv1.CompilerValidationIssueCode_COMPILER_VALIDATION_ISSUE_CODE_DELIVERY_UNSUPPORTED:
		return controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_DELIVERY_UNSUPPORTED
	case compilerv1.CompilerValidationIssueCode_COMPILER_VALIDATION_ISSUE_CODE_OPTION_UNKNOWN:
		return controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_OPTION_UNKNOWN
	case compilerv1.CompilerValidationIssueCode_COMPILER_VALIDATION_ISSUE_CODE_OPTION_REQUIRED:
		return controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_OPTION_REQUIRED
	case compilerv1.CompilerValidationIssueCode_COMPILER_VALIDATION_ISSUE_CODE_OPTION_INVALID,
		compilerv1.CompilerValidationIssueCode_COMPILER_VALIDATION_ISSUE_CODE_OPTION_OWNERSHIP_INVALID,
		compilerv1.CompilerValidationIssueCode_COMPILER_VALIDATION_ISSUE_CODE_SECRET_FIELD_INVALID:
		return controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_OPTION_INVALID
	case compilerv1.CompilerValidationIssueCode_COMPILER_VALIDATION_ISSUE_CODE_CONNECTION_REF_REQUIRED:
		return controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_CONNECTION_REF_REQUIRED
	case compilerv1.CompilerValidationIssueCode_COMPILER_VALIDATION_ISSUE_CODE_CONNECTION_SCHEMA_INCOMPATIBLE:
		return controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_CONNECTION_REF_UNAVAILABLE
	case compilerv1.CompilerValidationIssueCode_COMPILER_VALIDATION_ISSUE_CODE_VALIDATION_REVISION_CHANGED:
		return controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_VALIDATION_REVISION_CHANGED
	case compilerv1.CompilerValidationIssueCode_COMPILER_VALIDATION_ISSUE_CODE_ISSUES_TRUNCATED:
		return controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_ISSUES_TRUNCATED
	default:
		return controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_STRUCTURE_INVALID
	}
}

func publicIssueMessage(code controlv1.JobValidationIssueCode) string {
	return map[controlv1.JobValidationIssueCode]string{
		controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_STRUCTURE_INVALID:           "job specification is structurally invalid",
		controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_CONNECTOR_NOT_FOUND:         "connector is not available in this deployment",
		controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_ROLE_UNSUPPORTED:            "connector does not support the requested role",
		controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_CAPABILITY_MISSING:          "connector combination lacks a required execution capability",
		controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_TRANSFORM_UNSUPPORTED:       "configured transforms are not supported by this runtime",
		controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_DELIVERY_UNSUPPORTED:        "delivery guarantee is not supported by this connector combination",
		controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_OPTION_UNKNOWN:              "connector option is not declared by the active descriptor",
		controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_OPTION_REQUIRED:             "a required connector option is not configured",
		controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_OPTION_INVALID:              "connector option is invalid for this resource or role",
		controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_CONNECTION_REF_REQUIRED:     "an active Connection is required",
		controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_CONNECTION_REF_UNAVAILABLE:  "Connection is unavailable for this connector role",
		controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_VALIDATION_REVISION_CHANGED: "validation revision changed; refresh and validate again",
		controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_ISSUES_TRUNCATED:            "additional validation issues were omitted",
	}[code]
}

func publicIssue(
	code controlv1.JobValidationIssueCode, fieldPath, message, documentationKey string,
) *controlv1.JobValidationIssue {
	if len(fieldPath) > 256 {
		fieldPath = fieldPath[:256]
	}
	if len(message) > 256 {
		message = message[:256]
	}
	if len(documentationKey) > 256 {
		documentationKey = documentationKey[:256]
	}
	return &controlv1.JobValidationIssue{
		Code: code, Severity: controlv1.JobValidationSeverity_JOB_VALIDATION_SEVERITY_ERROR,
		FieldPath: fieldPath, Message: message, DocumentationKey: documentationKey,
	}
}

func boundedPublicIssues(values []*controlv1.JobValidationIssue) []*controlv1.JobValidationIssue {
	if len(values) <= maximumValidationIssues {
		return values
	}
	result := append([]*controlv1.JobValidationIssue(nil), values[:maximumValidationIssues-1]...)
	result = append(result, publicIssue(
		controlv1.JobValidationIssueCode_JOB_VALIDATION_ISSUE_CODE_ISSUES_TRUNCATED,
		"spec", "additional validation issues were omitted", "",
	))
	return result
}
