package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"

	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/api-server/internal/catalogproto"
	"io.astrasync/control-plane/auth"
	"io.astrasync/control-plane/catalog"
	"io.astrasync/control-plane/connection"
)

const (
	defaultConnectionPageSize     = 50
	maximumConnectionPageSize     = 100
	connectionTokenLifetime       = 15 * time.Minute
	connectionTestRetention       = 24 * time.Hour
	defaultConnectionTestDeadline = 30 * time.Second
	minimumIdempotencyKeySize     = 16
	maximumIdempotencyKeySize     = 128
)

var jdbcURLPattern = regexp.MustCompile(`^jdbc:[A-Za-z0-9][A-Za-z0-9_-]*:.+$`)

type ConnectionService struct {
	controlv1.UnimplementedConnectionServiceServer
	repository       connection.Repository
	catalog          catalog.Repository
	authorizer       auth.Authorizer
	executionProfile string
	tokenKey         []byte
	clock            func() time.Time
	uid              func() string
	testDeadline     time.Duration
	testPolicy       ConnectionTestPolicyResolver
	mutationsEnabled bool
	testsEnabled     bool
}

type ConnectionTestPolicyResolver interface {
	ResolveConnectionTestPolicy(context.Context, string) (connection.TestEgressPolicy, error)
}

type ConnectionTestPolicyResolverFunc func(context.Context, string) (connection.TestEgressPolicy, error)

func (f ConnectionTestPolicyResolverFunc) ResolveConnectionTestPolicy(
	ctx context.Context, tenantID string,
) (connection.TestEgressPolicy, error) {
	return f(ctx, tenantID)
}

type ConnectionServiceOption func(*ConnectionService) error

func WithConnectionMutationsEnabled(enabled bool) ConnectionServiceOption {
	return func(service *ConnectionService) error {
		service.mutationsEnabled = enabled
		return nil
	}
}

func WithConnectionTestsEnabled(enabled bool) ConnectionServiceOption {
	return func(service *ConnectionService) error {
		service.testsEnabled = enabled
		return nil
	}
}

func WithConnectionTestPolicyResolver(resolver ConnectionTestPolicyResolver) ConnectionServiceOption {
	return func(service *ConnectionService) error {
		if resolver == nil {
			return fmt.Errorf("Connection test policy resolver must not be nil")
		}
		service.testPolicy = resolver
		return nil
	}
}

func WithConnectionTestDeadline(deadline time.Duration) ConnectionServiceOption {
	return func(service *ConnectionService) error {
		if deadline < time.Second || deadline > 2*time.Minute {
			return fmt.Errorf("Connection test deadline must be between one second and two minutes")
		}
		service.testDeadline = deadline
		return nil
	}
}

func NewConnectionService(
	repository connection.Repository,
	catalogRepository catalog.Repository,
	authorizer auth.Authorizer,
	executionProfile string,
	tokenKey []byte,
	clock func() time.Time,
	uid func() string,
	options ...ConnectionServiceOption,
) (*ConnectionService, error) {
	if repository == nil || catalogRepository == nil || authorizer == nil || clock == nil || uid == nil ||
		strings.TrimSpace(executionProfile) == "" || len(tokenKey) < 32 {
		return nil, fmt.Errorf("Connection service dependencies must not be nil, blank, or undersized")
	}
	defaultPolicy := connection.DefaultTestEgressPolicy()
	result := &ConnectionService{
		repository: repository, catalog: catalogRepository, authorizer: authorizer,
		executionProfile: executionProfile, tokenKey: append([]byte(nil), tokenKey...),
		clock: clock, uid: uid, testDeadline: defaultConnectionTestDeadline,
		testPolicy: ConnectionTestPolicyResolverFunc(func(context.Context, string) (connection.TestEgressPolicy, error) {
			return defaultPolicy.Clone(), nil
		}),
	}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("Connection service option must not be nil")
		}
		if err := option(result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *ConnectionService) CreateConnection(
	ctx context.Context, request *controlv1.CreateConnectionRequest,
) (*controlv1.Connection, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	decision, err := s.authorize(ctx, request.GetTenantId(), auth.PermissionConnectionsCreate)
	if err != nil {
		return nil, err
	}
	if !s.mutationsEnabled {
		return nil, stableConnectionError(codes.FailedPrecondition, "CONNECTION_MUTATIONS_DISABLED")
	}
	if err := validateConnectionName(request.GetName()); err != nil {
		return nil, err
	}
	descriptor, err := s.currentDescriptor(ctx, request.GetConnector())
	if err != nil {
		return nil, err
	}
	if !connectionCapable(descriptor) {
		return nil, stableConnectionError(codes.FailedPrecondition, "CONNECTOR_UNAVAILABLE")
	}
	settings, err := validateConnectionSettings(descriptor, request.GetSettings())
	if err != nil {
		return nil, err
	}
	locator, err := secretLocatorFromProto(descriptor, request.GetSecretBinding())
	if err != nil {
		return nil, err
	}
	if err := validateSecretPresence(descriptor, locator); err != nil {
		return nil, err
	}
	now := s.clock().UTC()
	candidate, err := connection.New(
		request.GetTenantId(), request.GetName(), s.uid(), descriptor.GetName(),
		request.GetDisplayName(), request.GetDescription(),
		connection.Generation{
			Number: 1, DescriptorRevision: descriptor.GetDescriptorRevision(),
			ConnectionSchemaRevision: descriptor.GetConnectionSchemaRevision(),
			Settings:                 settings, SecretLocator: locator, CreatedAt: now,
		}, now,
	)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "Connection fields are invalid")
	}
	mutation, err := s.mutation(
		ctx, request.GetTenantId(), request.GetName(), 0, connection.MutationCreate,
		controlv1.ConnectionService_CreateConnection_FullMethodName, request.GetIdempotencyKey(), request,
		decision, &candidate, nil,
		map[string]any{
			"uid": candidate.UID, "connector": candidate.Connector, "version": int64(1),
			"generation": int64(1), "descriptorRevision": candidate.Current.DescriptorRevision,
		},
	)
	if err != nil {
		return nil, err
	}
	result, err := s.repository.Apply(ctx, mutation)
	if err != nil {
		return nil, connectionRepositoryError(err)
	}
	return s.projectConnection(ctx, *result.Connection, descriptor, result.Outcome)
}

func (s *ConnectionService) GetConnection(
	ctx context.Context, request *controlv1.GetConnectionRequest,
) (*controlv1.Connection, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	if _, err := s.authorize(ctx, request.GetTenantId(), auth.PermissionConnectionsRead); err != nil {
		return nil, err
	}
	if err := validateConnectionName(request.GetName()); err != nil {
		return nil, err
	}
	stored, err := s.repository.Get(ctx, request.GetTenantId(), request.GetName())
	if err != nil {
		return nil, connectionRepositoryError(err)
	}
	descriptor, _ := s.findCurrentDescriptor(ctx, stored.Connector)
	return s.projectConnection(ctx, stored, descriptor, "")
}

func (s *ConnectionService) ListConnections(
	ctx context.Context, request *controlv1.ListConnectionsRequest,
) (*controlv1.ListConnectionsResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	decision, err := s.authorize(ctx, request.GetTenantId(), auth.PermissionConnectionsRead)
	if err != nil {
		return nil, err
	}
	pageSize := request.GetPageSize()
	if pageSize == 0 {
		pageSize = defaultConnectionPageSize
	}
	if pageSize < 0 || pageSize > maximumConnectionPageSize {
		return nil, status.Errorf(codes.InvalidArgument, "page_size must be between 1 and %d", maximumConnectionPageSize)
	}
	filterState, err := connectionStateFromProto(request.GetState(), true)
	if err != nil {
		return nil, err
	}
	if request.GetConnector() != "" && !catalogConnectorNamePattern.MatchString(request.GetConnector()) {
		return nil, status.Error(codes.InvalidArgument, "connector filter is invalid")
	}
	claims := connectionPageToken{}
	if request.GetPageToken() != "" {
		if err := s.decodeConnectionToken(request.GetPageToken(), &claims); err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid Connection page token")
		}
		if claims.TenantID != request.GetTenantId() || claims.Connector != request.GetConnector() ||
			claims.State != string(filterState) || claims.PolicyRevision != decision.PolicyRevision ||
			claims.ExpiresAt <= s.clock().UTC().Unix() {
			return nil, status.Error(codes.InvalidArgument, "Connection page token scope mismatch or expired")
		}
	}
	listed, err := s.repository.List(ctx, request.GetTenantId(), connection.ListFilter{
		Connector: request.GetConnector(), State: filterState, AfterName: claims.AfterName,
		AfterUID: claims.AfterUID, Limit: int(pageSize),
	})
	if err != nil {
		return nil, connectionRepositoryError(err)
	}
	if claims.ListRevision != "" && claims.ListRevision != listed.Revision {
		return nil, status.Error(codes.Aborted, "Connection list revision changed")
	}
	response := &controlv1.ListConnectionsResponse{
		Connections: make([]*controlv1.Connection, 0, len(listed.Connections)), ListRevision: listed.Revision,
	}
	for _, stored := range listed.Connections {
		descriptor, _ := s.findCurrentDescriptor(ctx, stored.Connector)
		projected, err := s.projectConnection(ctx, stored, descriptor, "")
		if err != nil {
			return nil, err
		}
		response.Connections = append(response.Connections, projected)
	}
	if listed.HasMore && len(listed.Connections) > 0 {
		last := listed.Connections[len(listed.Connections)-1]
		response.NextPageToken, err = s.encodeConnectionToken(connectionPageToken{
			TenantID: request.GetTenantId(), Connector: request.GetConnector(), State: string(filterState),
			PolicyRevision: decision.PolicyRevision, ListRevision: listed.Revision,
			AfterName: last.Name, AfterUID: last.UID,
			ExpiresAt: s.clock().UTC().Add(connectionTokenLifetime).Unix(),
		})
		if err != nil {
			return nil, status.Error(codes.Internal, "create Connection page token")
		}
	}
	return response, nil
}

func (s *ConnectionService) UpdateConnection(
	ctx context.Context, request *controlv1.UpdateConnectionRequest,
) (*controlv1.Connection, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	decision, err := s.authorize(ctx, request.GetTenantId(), auth.PermissionConnectionsUpdate)
	if err != nil {
		return nil, err
	}
	if !s.mutationsEnabled {
		return nil, stableConnectionError(codes.FailedPrecondition, "CONNECTION_MUTATIONS_DISABLED")
	}
	current, err := s.loadForMutation(ctx, request.GetTenantId(), request.GetName(), request.GetExpectedVersion())
	if err != nil {
		return nil, err
	}
	descriptor, err := s.currentDescriptor(ctx, current.Connector)
	if err != nil {
		return nil, err
	}
	settings, err := validateConnectionSettings(descriptor, request.GetSettings())
	if err != nil {
		return nil, err
	}
	if err := validateSecretPresence(descriptor, current.Current.SecretLocator); err != nil {
		return nil, err
	}
	next, changed, domainErr := current.Replace(
		request.GetDisplayName(), request.GetDescription(), settings,
		descriptor.GetDescriptorRevision(), descriptor.GetConnectionSchemaRevision(), s.clock().UTC(),
	)
	if domainErr != nil {
		return nil, connectionDomainError(domainErr)
	}
	mutation, err := s.mutation(
		ctx, request.GetTenantId(), request.GetName(), request.GetExpectedVersion(), connection.MutationUpdate,
		controlv1.ConnectionService_UpdateConnection_FullMethodName, request.GetIdempotencyKey(), request,
		decision, &next, nil,
		map[string]any{
			"uid": current.UID, "beforeVersion": current.Version, "afterVersion": next.Version,
			"beforeGeneration": current.Current.Number, "afterGeneration": next.Current.Number,
			"effectiveChanged": changed && next.Current.Number != current.Current.Number,
		},
	)
	if err != nil {
		return nil, err
	}
	result, err := s.repository.Apply(ctx, mutation)
	if err != nil {
		return nil, connectionRepositoryError(err)
	}
	return s.projectConnection(ctx, *result.Connection, descriptor, result.Outcome)
}

func (s *ConnectionService) RotateConnection(
	ctx context.Context, request *controlv1.RotateConnectionRequest,
) (*controlv1.Connection, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	decision, err := s.authorize(ctx, request.GetTenantId(), auth.PermissionConnectionsRotate)
	if err != nil {
		return nil, err
	}
	if !s.mutationsEnabled {
		return nil, stableConnectionError(codes.FailedPrecondition, "CONNECTION_MUTATIONS_DISABLED")
	}
	current, err := s.loadForMutation(ctx, request.GetTenantId(), request.GetName(), request.GetExpectedVersion())
	if err != nil {
		return nil, err
	}
	descriptor, _ := s.findCurrentDescriptor(ctx, current.Connector)
	locator, err := secretLocatorForRotation(descriptor, current.Current.SecretLocator, request.GetSecretBinding())
	if err != nil {
		return nil, err
	}
	next, err := current.Rotate(
		locator, current.Current.DescriptorRevision, current.Current.ConnectionSchemaRevision, s.clock().UTC(),
	)
	if err != nil {
		return nil, connectionDomainError(err)
	}
	mutation, err := s.mutation(
		ctx, request.GetTenantId(), request.GetName(), request.GetExpectedVersion(), connection.MutationRotate,
		controlv1.ConnectionService_RotateConnection_FullMethodName, request.GetIdempotencyKey(), request,
		decision, &next, nil,
		map[string]any{
			"uid": current.UID, "beforeGeneration": current.Current.Number,
			"afterGeneration": next.Current.Number, "providerKind": string(locator.Provider),
		},
	)
	if err != nil {
		return nil, err
	}
	result, err := s.repository.Apply(ctx, mutation)
	if err != nil {
		return nil, connectionRepositoryError(err)
	}
	return s.projectConnection(ctx, *result.Connection, descriptor, result.Outcome)
}

func (s *ConnectionService) EnableConnection(
	ctx context.Context, request *controlv1.EnableConnectionRequest,
) (*controlv1.Connection, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	return s.setConnectionState(
		ctx, request.GetTenantId(), request.GetName(), request.GetExpectedVersion(), request.GetIdempotencyKey(),
		request, connection.StateActive, connection.MutationEnable,
		controlv1.ConnectionService_EnableConnection_FullMethodName,
	)
}

func (s *ConnectionService) DisableConnection(
	ctx context.Context, request *controlv1.DisableConnectionRequest,
) (*controlv1.Connection, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	return s.setConnectionState(
		ctx, request.GetTenantId(), request.GetName(), request.GetExpectedVersion(), request.GetIdempotencyKey(),
		request, connection.StateDisabled, connection.MutationDisable,
		controlv1.ConnectionService_DisableConnection_FullMethodName,
	)
}

func (s *ConnectionService) DeleteConnection(
	ctx context.Context, request *controlv1.DeleteConnectionRequest,
) (*controlv1.DeleteConnectionResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	decision, err := s.authorize(ctx, request.GetTenantId(), auth.PermissionConnectionsDelete)
	if err != nil {
		return nil, err
	}
	if !s.mutationsEnabled {
		return nil, stableConnectionError(codes.FailedPrecondition, "CONNECTION_MUTATIONS_DISABLED")
	}
	current, err := s.loadForMutation(ctx, request.GetTenantId(), request.GetName(), request.GetExpectedVersion())
	if err != nil {
		return nil, err
	}
	mutation, err := s.mutation(
		ctx, request.GetTenantId(), request.GetName(), request.GetExpectedVersion(), connection.MutationDelete,
		controlv1.ConnectionService_DeleteConnection_FullMethodName, request.GetIdempotencyKey(), request,
		decision, nil, nil,
		map[string]any{
			"uid": current.UID, "finalVersion": current.Version,
			"finalGeneration": current.Current.Number,
		},
	)
	if err != nil {
		return nil, err
	}
	result, err := s.repository.Apply(ctx, mutation)
	if err != nil {
		return nil, connectionRepositoryError(err)
	}
	return &controlv1.DeleteConnectionResponse{
		Uid: result.Tombstone.UID, Outcome: mutationOutcomeToProto(result.Outcome),
		DeletedAt: timestamppb.New(result.Tombstone.DeletedAt),
	}, nil
}

func (s *ConnectionService) TestConnection(
	ctx context.Context, request *controlv1.TestConnectionRequest,
) (*controlv1.ConnectionTest, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	decision, err := s.authorize(ctx, request.GetTenantId(), auth.PermissionConnectionsTest)
	if err != nil {
		return nil, err
	}
	if !s.testsEnabled {
		return nil, stableConnectionError(codes.FailedPrecondition, "CONNECTION_TESTS_DISABLED")
	}
	current, err := s.loadForMutation(ctx, request.GetTenantId(), request.GetName(), request.GetExpectedVersion())
	if err != nil {
		return nil, err
	}
	descriptor, _ := s.findCurrentDescriptor(ctx, current.Connector)
	if compatibility(current, descriptor) != connection.CompatibilityCompatible {
		return nil, stableConnectionError(codes.FailedPrecondition, "CONNECTION_SCHEMA_INCOMPATIBLE")
	}
	now := s.clock().UTC()
	policy, err := s.testPolicy.ResolveConnectionTestPolicy(ctx, request.GetTenantId())
	if err != nil || policy.Validate() != nil {
		return nil, stableConnectionError(codes.FailedPrecondition, "TEST_POLICY_UNAVAILABLE")
	}
	actorID := principalActorID(decision)
	test := connection.TestOperation{
		TenantID: request.GetTenantId(), OperationID: s.uid(), ConnectionUID: current.UID,
		Generation: current.Current.Number, DescriptorRevision: current.Current.DescriptorRevision,
		ActorID: actorID, EgressPolicy: policy.Clone(), State: connection.TestQueued,
		CreatedAt: now, DeadlineAt: now.Add(s.testDeadline), ExpiresAt: now.Add(connectionTestRetention),
	}
	mutation, err := s.mutation(
		ctx, request.GetTenantId(), request.GetName(), request.GetExpectedVersion(), connection.MutationTest,
		controlv1.ConnectionService_TestConnection_FullMethodName, request.GetIdempotencyKey(), request,
		decision, nil, &test,
		map[string]any{
			"uid": current.UID, "operationId": test.OperationID,
			"generation": test.Generation, "descriptorRevision": test.DescriptorRevision,
		},
	)
	if err != nil {
		return nil, err
	}
	result, err := s.repository.Apply(ctx, mutation)
	if err != nil {
		return nil, connectionRepositoryError(err)
	}
	projected := testToProto(*result.Test, current.Current.Number == result.Test.Generation)
	projected.Outcome = mutationOutcomeToProto(result.Outcome)
	return projected, nil
}

func (s *ConnectionService) GetConnectionTest(
	ctx context.Context, request *controlv1.GetConnectionTestRequest,
) (*controlv1.ConnectionTest, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	if _, err := s.authorize(ctx, request.GetTenantId(), auth.PermissionConnectionsTest); err != nil {
		return nil, err
	}
	if _, err := parseUUID(request.GetOperationId()); err != nil {
		return nil, status.Error(codes.InvalidArgument, "operation_id must be a UUID")
	}
	test, err := s.repository.GetTest(ctx, request.GetTenantId(), request.GetOperationId())
	if err != nil {
		return nil, connectionRepositoryError(err)
	}
	current, currentErr := s.repository.GetByUID(ctx, request.GetTenantId(), test.ConnectionUID)
	return testToProto(test, currentErr == nil && current.Current.Number == test.Generation), nil
}

func (s *ConnectionService) setConnectionState(
	ctx context.Context,
	tenantID, name string,
	expectedVersion int64,
	idempotencyKey string,
	request proto.Message,
	target connection.State,
	kind connection.MutationKind,
	method string,
) (*controlv1.Connection, error) {
	decision, err := s.authorize(ctx, tenantID, auth.PermissionConnectionsDisable)
	if err != nil {
		return nil, err
	}
	if !s.mutationsEnabled {
		return nil, stableConnectionError(codes.FailedPrecondition, "CONNECTION_MUTATIONS_DISABLED")
	}
	current, err := s.loadForMutation(ctx, tenantID, name, expectedVersion)
	if err != nil {
		return nil, err
	}
	descriptor, _ := s.findCurrentDescriptor(ctx, current.Connector)
	compatible := compatibility(current, descriptor)
	if target == connection.StateActive {
		if descriptor == nil {
			return nil, stableConnectionError(codes.FailedPrecondition, "CONNECTOR_UNAVAILABLE")
		}
		if err := validateSecretPresence(descriptor, current.Current.SecretLocator); err != nil {
			return nil, err
		}
	}
	next, _, domainErr := current.SetState(target, compatible, s.clock().UTC())
	if domainErr != nil {
		return nil, connectionDomainError(domainErr)
	}
	mutation, err := s.mutation(
		ctx, tenantID, name, expectedVersion, kind, method, idempotencyKey, request,
		decision, &next, nil,
		map[string]any{
			"uid": current.UID, "beforeState": string(current.State), "afterState": string(next.State),
			"beforeVersion": current.Version, "afterVersion": next.Version,
			"compatibility": string(compatible),
		},
	)
	if err != nil {
		return nil, err
	}
	result, err := s.repository.Apply(ctx, mutation)
	if err != nil {
		return nil, connectionRepositoryError(err)
	}
	return s.projectConnection(ctx, *result.Connection, descriptor, result.Outcome)
}

func (s *ConnectionService) loadForMutation(
	ctx context.Context, tenantID, name string, expectedVersion int64,
) (connection.Connection, error) {
	if err := validateConnectionName(name); err != nil {
		return connection.Connection{}, err
	}
	if expectedVersion <= 0 {
		return connection.Connection{}, status.Error(codes.InvalidArgument, "expected_version must be positive")
	}
	current, err := s.repository.Get(ctx, tenantID, name)
	if err != nil {
		return connection.Connection{}, connectionRepositoryError(err)
	}
	return current, nil
}

func (s *ConnectionService) mutation(
	ctx context.Context,
	tenantID, name string,
	expectedVersion int64,
	kind connection.MutationKind,
	method, idempotencyKey string,
	request proto.Message,
	decision auth.Decision,
	candidate *connection.Connection,
	test *connection.TestOperation,
	auditAttributes map[string]any,
) (connection.Mutation, error) {
	if len(idempotencyKey) < minimumIdempotencyKeySize || len(idempotencyKey) > maximumIdempotencyKeySize ||
		strings.ContainsAny(idempotencyKey, "\r\n\x00") {
		return connection.Mutation{}, status.Errorf(
			codes.InvalidArgument, "idempotency_key must contain between %d and %d characters",
			minimumIdempotencyKeySize, maximumIdempotencyKeySize,
		)
	}
	digest, err := s.requestDigest(request)
	if err != nil {
		return connection.Mutation{}, status.Error(codes.Internal, "compute Connection request identity")
	}
	actorID := principalActorID(decision)
	now := s.clock().UTC()
	return connection.Mutation{
		Kind: kind, TenantID: tenantID, Name: name, ExpectedVersion: expectedVersion,
		Candidate: candidate, Test: test, AuditAttributes: auditAttributes,
		Identity: connection.MutationIdentity{
			ActorID: actorID, Method: method, KeyFingerprint: s.keyedDigest([]byte(idempotencyKey)),
			RequestDigest: digest, RequestID: requestID(ctx, s.uid), AuditEventID: s.uid(), OccurredAt: now,
		},
	}, nil
}

func (s *ConnectionService) requestDigest(request proto.Message) (string, error) {
	cloned := proto.Clone(request)
	field := cloned.ProtoReflect().Descriptor().Fields().ByName(protoreflect.Name("idempotency_key"))
	if field != nil {
		cloned.ProtoReflect().Clear(field)
	}
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(cloned)
	if err != nil {
		return "", err
	}
	return s.keyedDigest(payload), nil
}

func (s *ConnectionService) keyedDigest(payload []byte) string {
	mac := hmac.New(sha256.New, s.tokenKey)
	_, _ = mac.Write(payload)
	return fmt.Sprintf("sha256:%x", mac.Sum(nil))
}

func (s *ConnectionService) authorize(
	ctx context.Context, tenantID string, permission auth.Permission,
) (auth.Decision, error) {
	decision, err := s.authorizer.Authorize(ctx, tenantID, permission)
	if err == nil {
		return decision, nil
	}
	if errors.Is(err, auth.ErrUnauthenticated) {
		return auth.Decision{}, status.Error(codes.Unauthenticated, "authentication required")
	}
	return auth.Decision{}, status.Error(codes.PermissionDenied, "tenant access denied")
}

func (s *ConnectionService) currentDescriptor(
	ctx context.Context, connector string,
) (*controlv1.ConnectorDescriptor, error) {
	if !catalogConnectorNamePattern.MatchString(connector) {
		return nil, status.Error(codes.InvalidArgument, "connector name is invalid")
	}
	descriptor, err := s.findCurrentDescriptor(ctx, connector)
	if err != nil || descriptor == nil {
		return nil, stableConnectionError(codes.FailedPrecondition, "CONNECTOR_UNAVAILABLE")
	}
	return descriptor, nil
}

func (s *ConnectionService) findCurrentDescriptor(
	ctx context.Context, connector string,
) (*controlv1.ConnectorDescriptor, error) {
	snapshot, err := s.catalog.Current(ctx, s.executionProfile)
	if err != nil {
		return nil, err
	}
	inventory, err := catalogproto.ParseSnapshot(snapshot)
	if err != nil {
		return nil, err
	}
	for _, descriptor := range inventory.GetDescriptors() {
		if descriptor.GetName() == connector {
			return descriptor, nil
		}
	}
	return nil, catalog.ErrNotFound
}

func (s *ConnectionService) projectConnection(
	ctx context.Context,
	stored connection.Connection,
	descriptor *controlv1.ConnectorDescriptor,
	outcome connection.MutationOutcome,
) (*controlv1.Connection, error) {
	counts, err := s.repository.ReferenceCounts(ctx, stored.UID)
	if err != nil && !errors.Is(err, connection.ErrNotFound) {
		return nil, connectionRepositoryError(err)
	}
	result := &controlv1.Connection{
		TenantId: stored.TenantID, Name: stored.Name, Uid: stored.UID, Connector: stored.Connector,
		Version: stored.Version, Generation: stored.Current.Number,
		State: connectionStateToProto(stored.State), Compatibility: compatibilityToProto(compatibility(stored, descriptor)),
		DisplayName: stored.DisplayName, Description: stored.Description,
		DescriptorRevision:       stored.Current.DescriptorRevision,
		ConnectionSchemaRevision: stored.Current.ConnectionSchemaRevision,
		SecretProvider:           providerToProto(stored.Current.SecretLocator.Provider),
		SecretConfigured:         stored.Current.SecretLocator.Provider != "",
		ReferenceCounts: &controlv1.ConnectionReferenceCounts{
			Jobs: counts.Jobs, Executions: counts.Executions, Tests: counts.Tests,
			CleanupObligations: counts.CleanupObligations,
		},
		CreatedAt: timestamppb.New(stored.CreatedAt), UpdatedAt: timestamppb.New(stored.UpdatedAt),
		Outcome: mutationOutcomeToProto(outcome),
	}
	for _, setting := range stored.Current.Settings {
		if setting.Sensitivity == connection.SensitivityPublic {
			result.PublicSettings = append(result.PublicSettings, &controlv1.ConnectionSetting{
				Key: setting.Key, Value: setting.Value,
			})
		}
	}
	latest, latestErr := s.repository.LatestTest(ctx, stored.UID)
	if latestErr == nil {
		result.LastTest = &controlv1.ConnectionTestSummary{
			OperationId: latest.OperationID, Generation: latest.Generation,
			State: testStateToProto(latest.State), CurrentGeneration: latest.Generation == stored.Current.Number,
			CompletedAt: optionalTimestamp(latest.CompletedAt),
		}
	} else if !errors.Is(latestErr, connection.ErrTestNotFound) {
		return nil, connectionRepositoryError(latestErr)
	}
	return result, nil
}

func validateConnectionSettings(
	descriptor *controlv1.ConnectorDescriptor,
	requested []*controlv1.ConnectionSetting,
) ([]connection.Setting, error) {
	if len(requested) > connection.MaximumSettings {
		return nil, status.Error(codes.InvalidArgument, "too many Connection settings")
	}
	exact := make(map[string]*controlv1.ConnectorOptionDefinition)
	for _, option := range descriptor.GetOptions() {
		exact[option.GetKey()] = option
	}
	result := make([]connection.Setting, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))
	for _, setting := range requested {
		if setting == nil || !catalogOptionNamePattern.MatchString(setting.GetKey()) ||
			len(setting.GetValue()) > connection.MaximumSettingValue {
			return nil, status.Error(codes.InvalidArgument, "Connection setting is invalid")
		}
		if _, duplicate := seen[setting.GetKey()]; duplicate {
			return nil, status.Error(codes.InvalidArgument, "Connection settings contain duplicate keys")
		}
		seen[setting.GetKey()] = struct{}{}
		definition := exact[setting.GetKey()]
		if definition == nil {
			definition = matchingPrefixDefinition(descriptor, setting.GetKey())
		}
		if definition == nil || definition.GetOwner() != controlv1.ConnectorOptionOwner_CONNECTOR_OPTION_OWNER_CONNECTION ||
			definition.GetSensitivity() == controlv1.ConnectorOptionSensitivity_CONNECTOR_OPTION_SENSITIVITY_SECRET {
			return nil, status.Errorf(codes.InvalidArgument, "Connection setting %q is not accepted", setting.GetKey())
		}
		if err := validateOptionValue(definition, setting.GetValue()); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "Connection setting %q is invalid", setting.GetKey())
		}
		sensitivity := connection.SensitivityPublic
		if definition.GetSensitivity() == controlv1.ConnectorOptionSensitivity_CONNECTOR_OPTION_SENSITIVITY_RESTRICTED {
			sensitivity = connection.SensitivityRestricted
		}
		result = append(result, connection.Setting{Key: setting.GetKey(), Value: setting.GetValue(), Sensitivity: sensitivity})
	}
	for _, option := range descriptor.GetOptions() {
		if option.GetOwner() == controlv1.ConnectorOptionOwner_CONNECTOR_OPTION_OWNER_CONNECTION &&
			option.GetSensitivity() != controlv1.ConnectorOptionSensitivity_CONNECTOR_OPTION_SENSITIVITY_SECRET &&
			option.GetRequired() && option.DefaultValue == nil {
			if _, present := seen[option.GetKey()]; !present {
				return nil, status.Errorf(codes.InvalidArgument, "required Connection setting %q is missing", option.GetKey())
			}
		}
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Key < result[right].Key })
	return result, nil
}

func matchingPrefixDefinition(
	descriptor *controlv1.ConnectorDescriptor, key string,
) *controlv1.ConnectorOptionDefinition {
	for _, prefix := range descriptor.GetOptionPrefixes() {
		if strings.HasPrefix(key, prefix.GetPrefix()) &&
			prefix.GetOwner() == controlv1.ConnectorOptionOwner_CONNECTOR_OPTION_OWNER_CONNECTION {
			return &controlv1.ConnectorOptionDefinition{
				Key: key, Owner: prefix.GetOwner(), Sensitivity: prefix.GetSensitivity(),
				ValueType: controlv1.ConnectorOptionType_CONNECTOR_OPTION_TYPE_STRING,
				MaxLength: proto.Int32(prefix.GetMaxValueLength()),
			}
		}
	}
	return nil
}

func validateOptionValue(definition *controlv1.ConnectorOptionDefinition, value string) error {
	length := int32(utf8.RuneCountInString(value))
	if definition.MinLength != nil && length < definition.GetMinLength() ||
		definition.MaxLength != nil && length > definition.GetMaxLength() {
		return fmt.Errorf("value length is invalid")
	}
	switch definition.GetValueType() {
	case controlv1.ConnectorOptionType_CONNECTOR_OPTION_TYPE_STRING:
		switch definition.GetPatternKey() {
		case "jdbc.url":
			if !jdbcURLPattern.MatchString(value) {
				return fmt.Errorf("JDBC URL is invalid")
			}
		}
	case controlv1.ConnectorOptionType_CONNECTOR_OPTION_TYPE_INTEGER:
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || definition.Minimum != nil && parsed < definition.GetMinimum() ||
			definition.Maximum != nil && parsed > definition.GetMaximum() {
			return fmt.Errorf("integer is outside bounds")
		}
	case controlv1.ConnectorOptionType_CONNECTOR_OPTION_TYPE_BOOLEAN:
		if value != "true" && value != "false" {
			return fmt.Errorf("boolean must be true or false")
		}
	case controlv1.ConnectorOptionType_CONNECTOR_OPTION_TYPE_DURATION:
		if _, err := time.ParseDuration(value); err != nil {
			return err
		}
	case controlv1.ConnectorOptionType_CONNECTOR_OPTION_TYPE_ENUM:
		if !containsStringValue(definition.GetEnumValues(), value) {
			return fmt.Errorf("enum value is not accepted")
		}
	default:
		return fmt.Errorf("option type is not supported")
	}
	return nil
}

func secretLocatorFromProto(
	descriptor *controlv1.ConnectorDescriptor, binding *controlv1.SecretBinding,
) (connection.SecretLocator, error) {
	secretFields := descriptorSecretFields(descriptor)
	if binding == nil {
		if requiredSecretCount(secretFields) != 0 {
			return connection.SecretLocator{}, stableConnectionError(codes.InvalidArgument, "SECRET_BINDING_INVALID")
		}
		return connection.SecretLocator{}, nil
	}
	return validateKubernetesBinding(binding, secretFields)
}

func secretLocatorForRotation(
	descriptor *controlv1.ConnectorDescriptor,
	current connection.SecretLocator,
	binding *controlv1.SecretBinding,
) (connection.SecretLocator, error) {
	if binding == nil {
		return connection.SecretLocator{}, stableConnectionError(codes.InvalidArgument, "SECRET_BINDING_INVALID")
	}
	fields := make(map[string]bool)
	if descriptor != nil {
		fields = descriptorSecretFields(descriptor)
	} else {
		for _, field := range current.Fields {
			fields[field.LogicalField] = true
		}
	}
	return validateKubernetesBinding(binding, fields)
}

func validateKubernetesBinding(
	binding *controlv1.SecretBinding, accepted map[string]bool,
) (connection.SecretLocator, error) {
	if binding.GetProvider() != controlv1.SecretProviderKind_SECRET_PROVIDER_KIND_KUBERNETES_SECRET_V1 ||
		binding.GetKubernetesSecretV1() == nil || len(accepted) == 0 {
		return connection.SecretLocator{}, stableConnectionError(codes.InvalidArgument, "SECRET_BINDING_INVALID")
	}
	source := binding.GetKubernetesSecretV1()
	fields := make([]connection.SecretFieldMapping, 0, len(source.GetFields()))
	seen := make(map[string]struct{}, len(source.GetFields()))
	for _, field := range source.GetFields() {
		if field == nil || !accepted[field.GetLogicalField()] || field.GetSecretKey() == "" {
			return connection.SecretLocator{}, stableConnectionError(codes.InvalidArgument, "SECRET_BINDING_INVALID")
		}
		if _, duplicate := seen[field.GetLogicalField()]; duplicate {
			return connection.SecretLocator{}, stableConnectionError(codes.InvalidArgument, "SECRET_BINDING_INVALID")
		}
		seen[field.GetLogicalField()] = struct{}{}
		fields = append(fields, connection.SecretFieldMapping{
			LogicalField: field.GetLogicalField(), SecretKey: field.GetSecretKey(),
		})
	}
	for field, required := range accepted {
		if required {
			if _, present := seen[field]; !present {
				return connection.SecretLocator{}, stableConnectionError(codes.InvalidArgument, "SECRET_BINDING_INVALID")
			}
		}
	}
	sort.Slice(fields, func(left, right int) bool { return fields[left].LogicalField < fields[right].LogicalField })
	locator := connection.SecretLocator{
		Provider:   connection.ProviderKubernetesSecretV1,
		SecretName: source.GetSecretName(), SecretUID: source.GetSecretUid(), Fields: fields,
	}
	if err := locator.Validate(); err != nil {
		return connection.SecretLocator{}, stableConnectionError(codes.InvalidArgument, "SECRET_BINDING_INVALID")
	}
	return locator, nil
}

func descriptorSecretFields(descriptor *controlv1.ConnectorDescriptor) map[string]bool {
	result := make(map[string]bool)
	for _, option := range descriptor.GetOptions() {
		if option.GetOwner() == controlv1.ConnectorOptionOwner_CONNECTOR_OPTION_OWNER_CONNECTION &&
			option.GetSensitivity() == controlv1.ConnectorOptionSensitivity_CONNECTOR_OPTION_SENSITIVITY_SECRET {
			result[option.GetKey()] = option.GetRequired()
		}
	}
	return result
}

func requiredSecretCount(fields map[string]bool) int {
	count := 0
	for _, required := range fields {
		if required {
			count++
		}
	}
	return count
}

func validateSecretPresence(
	descriptor *controlv1.ConnectorDescriptor, locator connection.SecretLocator,
) error {
	fields := descriptorSecretFields(descriptor)
	if len(fields) == 0 {
		if locator.Provider != "" {
			return stableConnectionError(codes.InvalidArgument, "SECRET_BINDING_INVALID")
		}
		return nil
	}
	if locator.Provider == "" {
		if requiredSecretCount(fields) > 0 {
			return stableConnectionError(codes.InvalidArgument, "SECRET_BINDING_INVALID")
		}
		return nil
	}
	present := make(map[string]struct{}, len(locator.Fields))
	for _, field := range locator.Fields {
		present[field.LogicalField] = struct{}{}
	}
	for name, required := range fields {
		if required {
			if _, found := present[name]; !found {
				return stableConnectionError(codes.InvalidArgument, "SECRET_BINDING_INVALID")
			}
		}
	}
	return nil
}

func connectionCapable(descriptor *controlv1.ConnectorDescriptor) bool {
	for _, requirement := range descriptor.GetConnectionRequirements() {
		if requirement.GetRequirement() == controlv1.ConnectionRequirement_CONNECTION_REQUIREMENT_OPTIONAL ||
			requirement.GetRequirement() == controlv1.ConnectionRequirement_CONNECTION_REQUIREMENT_REQUIRED {
			return true
		}
	}
	return false
}

func compatibility(
	stored connection.Connection, descriptor *controlv1.ConnectorDescriptor,
) connection.Compatibility {
	if descriptor == nil || descriptor.GetName() != stored.Connector {
		return connection.CompatibilityConnectorUnavailable
	}
	if containsStringValue(
		descriptor.GetAcceptedConnectionSchemaRevisions(), stored.Current.ConnectionSchemaRevision,
	) {
		return connection.CompatibilityCompatible
	}
	return connection.CompatibilityRevalidationRequired
}

func validateConnectionName(name string) error {
	if len(name) == 0 || len(name) > 63 || !connectionNamePattern.MatchString(name) {
		return status.Error(codes.InvalidArgument, "Connection name must be a lowercase DNS label")
	}
	return nil
}

var (
	connectionNamePattern       = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)
	catalogConnectorNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?$`)
	catalogOptionNamePattern    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,127}$`)
)

func connectionStateFromProto(value controlv1.ConnectionState, allowUnspecified bool) (connection.State, error) {
	switch value {
	case controlv1.ConnectionState_CONNECTION_STATE_UNSPECIFIED:
		if allowUnspecified {
			return "", nil
		}
	case controlv1.ConnectionState_CONNECTION_STATE_DISABLED:
		return connection.StateDisabled, nil
	case controlv1.ConnectionState_CONNECTION_STATE_ACTIVE:
		return connection.StateActive, nil
	}
	return "", status.Error(codes.InvalidArgument, "Connection state is invalid")
}

func connectionStateToProto(value connection.State) controlv1.ConnectionState {
	if value == connection.StateActive {
		return controlv1.ConnectionState_CONNECTION_STATE_ACTIVE
	}
	return controlv1.ConnectionState_CONNECTION_STATE_DISABLED
}

func compatibilityToProto(value connection.Compatibility) controlv1.ConnectionCompatibility {
	switch value {
	case connection.CompatibilityCompatible:
		return controlv1.ConnectionCompatibility_CONNECTION_COMPATIBILITY_COMPATIBLE
	case connection.CompatibilityRevalidationRequired:
		return controlv1.ConnectionCompatibility_CONNECTION_COMPATIBILITY_REVALIDATION_REQUIRED
	default:
		return controlv1.ConnectionCompatibility_CONNECTION_COMPATIBILITY_CONNECTOR_UNAVAILABLE
	}
}

func providerToProto(value connection.ProviderKind) controlv1.SecretProviderKind {
	if value == connection.ProviderKubernetesSecretV1 {
		return controlv1.SecretProviderKind_SECRET_PROVIDER_KIND_KUBERNETES_SECRET_V1
	}
	return controlv1.SecretProviderKind_SECRET_PROVIDER_KIND_UNSPECIFIED
}

func mutationOutcomeToProto(value connection.MutationOutcome) controlv1.MutationOutcome {
	switch value {
	case connection.OutcomeChanged:
		return controlv1.MutationOutcome_MUTATION_OUTCOME_CHANGED
	case connection.OutcomeNoChange:
		return controlv1.MutationOutcome_MUTATION_OUTCOME_NO_CHANGE
	case connection.OutcomeReplayed:
		return controlv1.MutationOutcome_MUTATION_OUTCOME_REPLAYED
	default:
		return controlv1.MutationOutcome_MUTATION_OUTCOME_UNSPECIFIED
	}
}

func testToProto(source connection.TestOperation, current bool) *controlv1.ConnectionTest {
	return &controlv1.ConnectionTest{
		TenantId: source.TenantID, OperationId: source.OperationID, ConnectionUid: source.ConnectionUID,
		Generation: source.Generation, State: testStateToProto(source.State), Phase: testPhaseToProto(source.Phase),
		ResultCode: testResultToProto(source.ResultCode), Success: source.Success,
		RemediationKey: source.RemediationKey, CurrentGeneration: current,
		CreatedAt: timestamppb.New(source.CreatedAt), StartedAt: optionalTimestamp(source.StartedAt),
		CompletedAt: optionalTimestamp(source.CompletedAt), ExpiresAt: timestamppb.New(source.ExpiresAt),
	}
}

func testStateToProto(value connection.TestState) controlv1.ConnectionTestState {
	return map[connection.TestState]controlv1.ConnectionTestState{
		connection.TestQueued:    controlv1.ConnectionTestState_CONNECTION_TEST_STATE_QUEUED,
		connection.TestRunning:   controlv1.ConnectionTestState_CONNECTION_TEST_STATE_RUNNING,
		connection.TestSucceeded: controlv1.ConnectionTestState_CONNECTION_TEST_STATE_SUCCEEDED,
		connection.TestFailed:    controlv1.ConnectionTestState_CONNECTION_TEST_STATE_FAILED,
		connection.TestTimedOut:  controlv1.ConnectionTestState_CONNECTION_TEST_STATE_TIMED_OUT,
		connection.TestCanceled:  controlv1.ConnectionTestState_CONNECTION_TEST_STATE_CANCELED,
		connection.TestExpired:   controlv1.ConnectionTestState_CONNECTION_TEST_STATE_EXPIRED,
	}[value]
}

func testPhaseToProto(value connection.TestPhase) controlv1.ConnectionTestPhase {
	return map[connection.TestPhase]controlv1.ConnectionTestPhase{
		connection.TestPhasePolicy:         controlv1.ConnectionTestPhase_CONNECTION_TEST_PHASE_POLICY,
		connection.TestPhaseDNS:            controlv1.ConnectionTestPhase_CONNECTION_TEST_PHASE_DNS,
		connection.TestPhaseTransport:      controlv1.ConnectionTestPhase_CONNECTION_TEST_PHASE_TRANSPORT,
		connection.TestPhaseTLS:            controlv1.ConnectionTestPhase_CONNECTION_TEST_PHASE_TLS,
		connection.TestPhaseAuthentication: controlv1.ConnectionTestPhase_CONNECTION_TEST_PHASE_AUTHENTICATION,
		connection.TestPhaseHandshake:      controlv1.ConnectionTestPhase_CONNECTION_TEST_PHASE_HANDSHAKE,
		connection.TestPhaseComplete:       controlv1.ConnectionTestPhase_CONNECTION_TEST_PHASE_COMPLETE,
	}[value]
}

func testResultToProto(value connection.TestResultCode) controlv1.ConnectionTestResultCode {
	return map[connection.TestResultCode]controlv1.ConnectionTestResultCode{
		connection.TestResultOK:                   controlv1.ConnectionTestResultCode_CONNECTION_TEST_RESULT_CODE_OK,
		connection.TestResultPolicyDenied:         controlv1.ConnectionTestResultCode_CONNECTION_TEST_RESULT_CODE_POLICY_DENIED,
		connection.TestResultSecretUnavailable:    controlv1.ConnectionTestResultCode_CONNECTION_TEST_RESULT_CODE_SECRET_UNAVAILABLE,
		connection.TestResultDNSFailed:            controlv1.ConnectionTestResultCode_CONNECTION_TEST_RESULT_CODE_DNS_FAILED,
		connection.TestResultTransportFailed:      controlv1.ConnectionTestResultCode_CONNECTION_TEST_RESULT_CODE_TRANSPORT_FAILED,
		connection.TestResultTLSFailed:            controlv1.ConnectionTestResultCode_CONNECTION_TEST_RESULT_CODE_TLS_FAILED,
		connection.TestResultAuthenticationFailed: controlv1.ConnectionTestResultCode_CONNECTION_TEST_RESULT_CODE_AUTHENTICATION_FAILED,
		connection.TestResultHandshakeFailed:      controlv1.ConnectionTestResultCode_CONNECTION_TEST_RESULT_CODE_HANDSHAKE_FAILED,
		connection.TestResultDeadlineExceeded:     controlv1.ConnectionTestResultCode_CONNECTION_TEST_RESULT_CODE_DEADLINE_EXCEEDED,
		connection.TestResultExecutorUnavailable:  controlv1.ConnectionTestResultCode_CONNECTION_TEST_RESULT_CODE_EXECUTOR_UNAVAILABLE,
	}[value]
}

func optionalTimestamp(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return timestamppb.New(*value)
}

func connectionRepositoryError(err error) error {
	switch {
	case errors.Is(err, connection.ErrNotFound), errors.Is(err, connection.ErrTestNotFound):
		return status.Error(codes.NotFound, "Connection resource not found")
	case errors.Is(err, connection.ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, "Connection already exists")
	case errors.Is(err, connection.ErrConflict):
		return stableConnectionError(codes.Aborted, "CONNECTION_VERSION_CONFLICT")
	case errors.Is(err, connection.ErrInUse):
		return stableConnectionError(codes.FailedPrecondition, "CONNECTION_IN_USE")
	case errors.Is(err, connection.ErrIdempotencyReused):
		return stableConnectionError(codes.AlreadyExists, "IDEMPOTENCY_KEY_REUSED")
	case errors.Is(err, connection.ErrTestLimitExceeded):
		return stableConnectionError(codes.ResourceExhausted, "TEST_LIMIT_EXCEEDED")
	default:
		return status.Error(codes.Internal, "Connection repository operation failed")
	}
}

func principalActorID(decision auth.Decision) string {
	if decision.Principal.ID != "" {
		return decision.Principal.ID
	}
	if decision.Principal.Subject != "" {
		return decision.Principal.Subject
	}
	return "development"
}

func connectionDomainError(err error) error {
	if errors.Is(err, connection.ErrInvalidTransition) {
		return status.Error(codes.FailedPrecondition, "Connection lifecycle transition is not allowed")
	}
	return status.Error(codes.InvalidArgument, "Connection mutation is invalid")
}

func stableConnectionError(code codes.Code, reason string) error { return status.Error(code, reason) }

func requestID(ctx context.Context, fallback func() string) string {
	values := metadata.ValueFromIncomingContext(ctx, "x-request-id")
	if len(values) == 1 && len(values[0]) > 0 && len(values[0]) <= 128 {
		return values[0]
	}
	return fallback()
}

func containsStringValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func parseUUID(value string) (string, error) {
	if len(value) != 36 {
		return "", fmt.Errorf("UUID is invalid")
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return "", fmt.Errorf("UUID is invalid")
			}
			continue
		}
		if !strings.ContainsRune("0123456789abcdefABCDEF", character) {
			return "", fmt.Errorf("UUID is invalid")
		}
	}
	return value, nil
}

type connectionPageToken struct {
	TenantID       string `json:"tenantId"`
	Connector      string `json:"connector,omitempty"`
	State          string `json:"state,omitempty"`
	PolicyRevision string `json:"policyRevision"`
	ListRevision   string `json:"listRevision"`
	AfterName      string `json:"afterName"`
	AfterUID       string `json:"afterUid"`
	ExpiresAt      int64  `json:"expiresAt"`
}

func (s *ConnectionService) encodeConnectionToken(claims connectionPageToken) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.tokenKey)
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *ConnectionService) decodeConnectionToken(token string, claims *connectionPageToken) error {
	if len(token) > 4096 {
		return fmt.Errorf("token is too large")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return fmt.Errorf("token shape is invalid")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, s.tokenKey)
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return fmt.Errorf("token signature is invalid")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	return decoder.Decode(claims)
}
