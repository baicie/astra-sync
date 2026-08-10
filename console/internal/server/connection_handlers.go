package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"io.astrasync/console/internal/authflow"
	jobv1 "io.astrasync/control-plane/api-server/gen/go/v1"
)

type connectionSettingBody struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type secretFieldBody struct {
	LogicalField string `json:"logicalField"`
	SecretKey    string `json:"secretKey"`
}

type secretBindingBody struct {
	Provider   string            `json:"provider"`
	SecretName string            `json:"secretName"`
	SecretUID  string            `json:"secretUid"`
	Fields     []secretFieldBody `json:"fields"`
}

type connectionBody struct {
	Name            string                  `json:"name,omitempty"`
	Connector       string                  `json:"connector,omitempty"`
	DisplayName     string                  `json:"displayName,omitempty"`
	Description     string                  `json:"description,omitempty"`
	ExpectedVersion int64                   `json:"expectedVersion,omitempty"`
	Settings        []connectionSettingBody `json:"settings,omitempty"`
	SecretBinding   *secretBindingBody      `json:"secretBinding,omitempty"`
}

func (s *Server) listConnections(response http.ResponseWriter, request *http.Request) {
	scope, session, err := s.scope(request)
	if err != nil {
		writeError(response, err)
		return
	}
	_, pageSize, err := parsePagination(request)
	if err != nil {
		writeError(response, status.Error(codes.InvalidArgument, "pagination is invalid"))
		return
	}
	state, err := parseConnectionState(request.URL.Query().Get("state"))
	if err != nil {
		writeError(response, status.Error(codes.InvalidArgument, "connection state is invalid"))
		return
	}
	ctx, cancel := s.backendContext(request, session, readTimeout)
	defer cancel()
	result, err := s.connections.ListConnections(ctx, &jobv1.ListConnectionsRequest{
		TenantId: scope.tenantID, Connector: strings.TrimSpace(request.URL.Query().Get("connector")),
		State: state, PageSize: int32(pageSize), PageToken: request.URL.Query().Get("page_token"),
	})
	if err != nil {
		writeError(response, err)
		return
	}
	s.setScope(response, scope)
	writeProtoJSON(response, result)
}

func (s *Server) getConnection(response http.ResponseWriter, request *http.Request) {
	scope, session, err := s.scope(request)
	if err != nil {
		writeError(response, err)
		return
	}
	name := resourceName(request, "name")
	if name == "" {
		writeError(response, status.Error(codes.InvalidArgument, "connection name is invalid"))
		return
	}
	ctx, cancel := s.backendContext(request, session, readTimeout)
	defer cancel()
	result, err := s.connections.GetConnection(ctx, &jobv1.GetConnectionRequest{TenantId: scope.tenantID, Name: name})
	if err != nil {
		writeError(response, err)
		return
	}
	s.setScope(response, scope)
	writeProtoJSON(response, result)
}

func (s *Server) createConnection(response http.ResponseWriter, request *http.Request) {
	scope, session, err := s.scope(request)
	if err != nil {
		writeError(response, err)
		return
	}
	if err := s.requireMutation(request, session); err != nil {
		writeError(response, err)
		return
	}
	var body connectionBody
	if err := parseJSONBody(request, &body, s.maximumBody); err != nil || body.Name == "" || body.Connector == "" {
		writeError(response, status.Error(codes.InvalidArgument, "connection request is invalid"))
		return
	}
	ctx, cancel := s.backendContext(request, session, writeTimeout)
	defer cancel()
	settings, err := s.connectionSettings(ctx, session, scope.tenantID, body.Connector, body.Settings)
	if err != nil {
		writeError(response, err)
		return
	}
	secretBinding, err := buildSecretBinding(body.SecretBinding)
	if err != nil {
		writeError(response, status.Error(codes.InvalidArgument, "secret binding is invalid"))
		return
	}
	result, err := s.connections.CreateConnection(ctx, &jobv1.CreateConnectionRequest{
		TenantId: scope.tenantID, Name: body.Name, Connector: body.Connector,
		DisplayName: body.DisplayName, Description: body.Description, Settings: settings,
		SecretBinding: secretBinding, IdempotencyKey: idempotencyKey(request),
	})
	if err != nil {
		writeError(response, err)
		return
	}
	s.setScope(response, scope)
	writeProtoJSON(response, result)
}

func (s *Server) updateConnection(response http.ResponseWriter, request *http.Request) {
	scope, session, err := s.scope(request)
	if err != nil {
		writeError(response, err)
		return
	}
	if err := s.requireMutation(request, session); err != nil {
		writeError(response, err)
		return
	}
	var body connectionBody
	if err := parseJSONBody(request, &body, s.maximumBody); err != nil || body.ExpectedVersion <= 0 {
		writeError(response, status.Error(codes.InvalidArgument, "connection request is invalid"))
		return
	}
	name := resourceName(request, "name")
	if name == "" {
		writeError(response, status.Error(codes.InvalidArgument, "connection name is invalid"))
		return
	}
	ctx, cancel := s.backendContext(request, session, writeTimeout)
	defer cancel()
	current, err := s.connections.GetConnection(ctx, &jobv1.GetConnectionRequest{TenantId: scope.tenantID, Name: name})
	if err != nil {
		writeError(response, err)
		return
	}
	settings, err := s.connectionSettings(ctx, session, scope.tenantID, current.GetConnector(), body.Settings)
	if err != nil {
		writeError(response, err)
		return
	}
	result, err := s.connections.UpdateConnection(ctx, &jobv1.UpdateConnectionRequest{
		TenantId: scope.tenantID, Name: name, ExpectedVersion: body.ExpectedVersion,
		DisplayName: body.DisplayName, Description: body.Description, Settings: settings,
		IdempotencyKey: idempotencyKey(request),
	})
	if err != nil {
		writeError(response, err)
		return
	}
	s.setScope(response, scope)
	writeProtoJSON(response, result)
}

func (s *Server) rotateConnection(response http.ResponseWriter, request *http.Request) {
	s.mutateConnectionAction(response, request, "rotate")
}

func (s *Server) enableConnection(response http.ResponseWriter, request *http.Request) {
	s.mutateConnectionAction(response, request, "enable")
}

func (s *Server) disableConnection(response http.ResponseWriter, request *http.Request) {
	s.mutateConnectionAction(response, request, "disable")
}

func (s *Server) mutateConnectionAction(response http.ResponseWriter, request *http.Request, action string) {
	scope, session, err := s.scope(request)
	if err != nil {
		writeError(response, err)
		return
	}
	if err := s.requireMutation(request, session); err != nil {
		writeError(response, err)
		return
	}
	var body connectionBody
	if err := parseJSONBody(request, &body, s.maximumBody); err != nil || body.ExpectedVersion <= 0 {
		writeError(response, status.Error(codes.InvalidArgument, "connection request is invalid"))
		return
	}
	name := resourceName(request, "name")
	if name == "" {
		writeError(response, status.Error(codes.InvalidArgument, "connection name is invalid"))
		return
	}
	ctx, cancel := s.backendContext(request, session, writeTimeout)
	defer cancel()
	var result *jobv1.Connection
	switch action {
	case "rotate":
		binding, bindingErr := buildSecretBinding(body.SecretBinding)
		if bindingErr != nil {
			writeError(response, status.Error(codes.InvalidArgument, "secret binding is invalid"))
			return
		}
		result, err = s.connections.RotateConnection(ctx, &jobv1.RotateConnectionRequest{TenantId: scope.tenantID, Name: name,
			ExpectedVersion: body.ExpectedVersion, SecretBinding: binding, IdempotencyKey: idempotencyKey(request)})
	case "enable":
		result, err = s.connections.EnableConnection(ctx, &jobv1.EnableConnectionRequest{TenantId: scope.tenantID, Name: name,
			ExpectedVersion: body.ExpectedVersion, IdempotencyKey: idempotencyKey(request)})
	case "disable":
		result, err = s.connections.DisableConnection(ctx, &jobv1.DisableConnectionRequest{TenantId: scope.tenantID, Name: name,
			ExpectedVersion: body.ExpectedVersion, IdempotencyKey: idempotencyKey(request)})
	default:
		err = status.Error(codes.InvalidArgument, "connection action is invalid")
	}
	if err != nil {
		writeError(response, err)
		return
	}
	s.setScope(response, scope)
	writeProtoJSON(response, result)
}

func (s *Server) deleteConnection(response http.ResponseWriter, request *http.Request) {
	scope, session, err := s.scope(request)
	if err != nil {
		writeError(response, err)
		return
	}
	if err := s.requireMutation(request, session); err != nil {
		writeError(response, err)
		return
	}
	var body connectionBody
	if err := parseJSONBody(request, &body, s.maximumBody); err != nil || body.ExpectedVersion <= 0 {
		writeError(response, status.Error(codes.InvalidArgument, "connection request is invalid"))
		return
	}
	ctx, cancel := s.backendContext(request, session, writeTimeout)
	defer cancel()
	result, err := s.connections.DeleteConnection(ctx, &jobv1.DeleteConnectionRequest{TenantId: scope.tenantID,
		Name: resourceName(request, "name"), ExpectedVersion: body.ExpectedVersion, IdempotencyKey: idempotencyKey(request)})
	if err != nil {
		writeError(response, err)
		return
	}
	s.setScope(response, scope)
	writeProtoJSON(response, result)
}

func (s *Server) testConnection(response http.ResponseWriter, request *http.Request) {
	scope, session, err := s.scope(request)
	if err != nil {
		writeError(response, err)
		return
	}
	if err := s.requireMutation(request, session); err != nil {
		writeError(response, err)
		return
	}
	var body connectionBody
	if err := parseJSONBody(request, &body, s.maximumBody); err != nil || body.ExpectedVersion <= 0 {
		writeError(response, status.Error(codes.InvalidArgument, "connection request is invalid"))
		return
	}
	ctx, cancel := s.backendContext(request, session, writeTimeout)
	defer cancel()
	result, err := s.connections.TestConnection(ctx, &jobv1.TestConnectionRequest{TenantId: scope.tenantID,
		Name: resourceName(request, "name"), ExpectedVersion: body.ExpectedVersion, IdempotencyKey: idempotencyKey(request)})
	if err != nil {
		writeError(response, err)
		return
	}
	s.setScope(response, scope)
	writeProtoJSON(response, result)
}

func (s *Server) getConnectionTest(response http.ResponseWriter, request *http.Request) {
	scope, session, err := s.scope(request)
	if err != nil {
		writeError(response, err)
		return
	}
	operationID := strings.TrimSpace(request.PathValue("operationID"))
	if operationID == "" || len(operationID) > 128 {
		writeError(response, status.Error(codes.InvalidArgument, "test operation is invalid"))
		return
	}
	ctx, cancel := s.backendContext(request, session, readTimeout)
	defer cancel()
	result, err := s.connections.GetConnectionTest(ctx, &jobv1.GetConnectionTestRequest{TenantId: scope.tenantID, OperationId: operationID})
	if err != nil {
		writeError(response, err)
		return
	}
	s.setScope(response, scope)
	writeProtoJSON(response, result)
}

func parseConnectionState(value string) (jobv1.ConnectionState, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "", "CONNECTION_STATE_UNSPECIFIED":
		return jobv1.ConnectionState_CONNECTION_STATE_UNSPECIFIED, nil
	case "ACTIVE", "CONNECTION_STATE_ACTIVE":
		return jobv1.ConnectionState_CONNECTION_STATE_ACTIVE, nil
	case "DISABLED", "CONNECTION_STATE_DISABLED":
		return jobv1.ConnectionState_CONNECTION_STATE_DISABLED, nil
	default:
		return jobv1.ConnectionState_CONNECTION_STATE_UNSPECIFIED, fmt.Errorf("invalid state")
	}
}

func buildSecretBinding(body *secretBindingBody) (*jobv1.SecretBinding, error) {
	if body == nil {
		return nil, nil
	}
	provider := strings.ToUpper(strings.TrimSpace(body.Provider))
	if provider == "KUBERNETES" || provider == "KUBERNETES_SECRET_V1" || provider == "SECRET_PROVIDER_KIND_KUBERNETES_SECRET_V1" {
		if body.SecretName == "" || body.SecretUID == "" || len(body.SecretName) > 253 || len(body.SecretUID) > 256 ||
			strings.ContainsAny(body.SecretName, "/\r\n") || strings.ContainsAny(body.SecretUID, "\r\n") {
			return nil, fmt.Errorf("secret locator is invalid")
		}
		fields := make([]*jobv1.SecretFieldMapping, 0, len(body.Fields))
		seen := make(map[string]struct{}, len(body.Fields))
		for _, field := range body.Fields {
			logical, key := strings.TrimSpace(field.LogicalField), strings.TrimSpace(field.SecretKey)
			if logical == "" || key == "" || len(logical) > 128 || len(key) > 253 || strings.ContainsAny(logical+key, "\r\n") {
				return nil, fmt.Errorf("secret field mapping is invalid")
			}
			if _, exists := seen[logical]; exists {
				return nil, fmt.Errorf("duplicate secret field mapping")
			}
			seen[logical] = struct{}{}
			fields = append(fields, &jobv1.SecretFieldMapping{LogicalField: logical, SecretKey: key})
		}
		if len(fields) == 0 {
			return nil, fmt.Errorf("secret field mapping is required")
		}
		return &jobv1.SecretBinding{Provider: jobv1.SecretProviderKind_SECRET_PROVIDER_KIND_KUBERNETES_SECRET_V1,
			Locator: &jobv1.SecretBinding_KubernetesSecretV1{KubernetesSecretV1: &jobv1.KubernetesSecretBinding{
				SecretName: body.SecretName, SecretUid: body.SecretUID, Fields: fields,
			}}}, nil
	}
	return nil, fmt.Errorf("secret provider is unsupported")
}

func (s *Server) connectionSettings(ctx context.Context, session authflow.Session, tenantID, connector string, input []connectionSettingBody) ([]*jobv1.ConnectionSetting, error) {
	settings := make([]*jobv1.ConnectionSetting, 0, len(input))
	seen := make(map[string]struct{}, len(input))
	for _, value := range input {
		key := strings.TrimSpace(value.Key)
		if key == "" || len(key) > 256 || len(value.Value) > 64*1024 || strings.ContainsAny(key, "\r\n\x00") {
			return nil, status.Error(codes.InvalidArgument, "connection settings are invalid")
		}
		if _, exists := seen[key]; exists || likelySensitiveKey(key) {
			return nil, status.Error(codes.InvalidArgument, "secret values must use a write-only Secret binding")
		}
		seen[key] = struct{}{}
		settings = append(settings, &jobv1.ConnectionSetting{Key: key, Value: value.Value})
	}
	if s.catalog != nil && len(settings) > 0 {
		descriptor, err := s.catalog.GetConnectorDescriptor(ctx, &jobv1.GetConnectorDescriptorRequest{TenantId: tenantID, Name: connector})
		if err != nil {
			return nil, err
		}
		if err := validateDescriptorOwnedSettings(descriptor.GetConnectorDescriptor(), settings); err != nil {
			return nil, err
		}
	}
	sort.Slice(settings, func(left, right int) bool { return settings[left].Key < settings[right].Key })
	return settings, nil
}

func validateDescriptorOwnedSettings(descriptor *jobv1.ConnectorDescriptor, settings []*jobv1.ConnectionSetting) error {
	if descriptor == nil {
		return status.Error(codes.FailedPrecondition, "connector descriptor is unavailable")
	}
	definitions := make(map[string]*jobv1.ConnectorOptionDefinition, len(descriptor.GetOptions()))
	for _, definition := range descriptor.GetOptions() {
		definitions[definition.GetKey()] = definition
	}
	for _, setting := range settings {
		if definition, found := definitions[setting.GetKey()]; found {
			if definition.GetOwner() != jobv1.ConnectorOptionOwner_CONNECTOR_OPTION_OWNER_CONNECTION ||
				definition.GetSensitivity() == jobv1.ConnectorOptionSensitivity_CONNECTOR_OPTION_SENSITIVITY_SECRET {
				return status.Error(codes.InvalidArgument, "secret or Job-owned settings must not be persisted here")
			}
		}
		for _, prefix := range descriptor.GetOptionPrefixes() {
			if strings.HasPrefix(setting.GetKey(), prefix.GetPrefix()) &&
				(prefix.GetOwner() != jobv1.ConnectorOptionOwner_CONNECTOR_OPTION_OWNER_CONNECTION ||
					prefix.GetSensitivity() == jobv1.ConnectorOptionSensitivity_CONNECTOR_OPTION_SENSITIVITY_SECRET) {
				return status.Error(codes.InvalidArgument, "secret or Job-owned settings must not be persisted here")
			}
		}
	}
	return nil
}

func likelySensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, marker := range []string{"password", "passwd", "token", "secret", "private_key", "privatekey", "certificate", "credential"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
