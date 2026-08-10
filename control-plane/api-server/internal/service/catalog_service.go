package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/api-server/internal/catalogproto"
	"io.astrasync/control-plane/auth"
	"io.astrasync/control-plane/catalog"
)

const (
	defaultCatalogPageSize = 50
	maximumCatalogPageSize = 100
	catalogTokenLifetime   = 15 * time.Minute
	retainedCatalogLimit   = 32
)

type ConnectorCatalogService struct {
	controlv1.UnimplementedConnectorCatalogServiceServer
	repository       catalog.Repository
	authorizer       auth.Authorizer
	executionProfile string
	tokenKey         []byte
	clock            func() time.Time
}

func NewConnectorCatalogService(
	repository catalog.Repository,
	authorizer auth.Authorizer,
	executionProfile string,
	tokenKey []byte,
	clock func() time.Time,
) (*ConnectorCatalogService, error) {
	if repository == nil || authorizer == nil || clock == nil || strings.TrimSpace(executionProfile) == "" {
		return nil, fmt.Errorf("connector catalog service dependencies must not be nil or blank")
	}
	if len(tokenKey) < 32 {
		return nil, fmt.Errorf("connector catalog page-token key must contain at least 32 bytes")
	}
	return &ConnectorCatalogService{
		repository:       repository,
		authorizer:       authorizer,
		executionProfile: executionProfile,
		tokenKey:         append([]byte(nil), tokenKey...),
		clock:            clock,
	}, nil
}

func (s *ConnectorCatalogService) ListConnectorDescriptors(
	ctx context.Context, request *controlv1.ListConnectorDescriptorsRequest,
) (*controlv1.ListConnectorDescriptorsResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	decision, err := s.authorize(ctx, request.GetTenantId())
	if err != nil {
		return nil, err
	}
	roles, capabilities, err := catalogFilters(request.GetRoles(), request.GetCapabilities())
	if err != nil {
		return nil, err
	}
	pageSize := request.GetPageSize()
	if pageSize == 0 {
		pageSize = defaultCatalogPageSize
	}
	if pageSize < 0 || pageSize > maximumCatalogPageSize {
		return nil, status.Errorf(codes.InvalidArgument, "page_size must be between 1 and %d", maximumCatalogPageSize)
	}
	snapshot, repositoryErr := s.repository.Current(ctx, s.executionProfile)
	if repositoryErr != nil {
		return nil, catalogReadError(repositoryErr)
	}
	inventory, parseErr := catalogproto.ParseSnapshot(snapshot)
	if parseErr != nil {
		return nil, status.Error(codes.Internal, "active connector catalog is invalid")
	}
	filtered := filterDescriptors(inventory.GetDescriptors(), roles, capabilities)
	cursor := 0
	if request.GetPageToken() != "" {
		claims, tokenErr := s.decodeToken(request.GetPageToken())
		if tokenErr != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid catalog page token")
		}
		if claims.TenantID != request.GetTenantId() || claims.PolicyRevision != decision.PolicyRevision ||
			!equalInt32s(claims.Roles, enumNumbers(roles)) ||
			!equalInt32s(claims.Capabilities, enumNumbers(capabilities)) {
			return nil, status.Error(codes.InvalidArgument, "catalog page token scope mismatch")
		}
		if claims.InventoryRevision != inventory.GetInventoryRevision() {
			return nil, status.Error(codes.FailedPrecondition, "CATALOG_REVISION_CHANGED")
		}
		if claims.ExpiresAt <= s.clock().UTC().Unix() || claims.Cursor < 0 || claims.Cursor > len(filtered) {
			return nil, status.Error(codes.InvalidArgument, "catalog page token expired or invalid")
		}
		cursor = claims.Cursor
	}
	end := min(cursor+int(pageSize), len(filtered))
	response := &controlv1.ListConnectorDescriptorsResponse{
		InventoryRevision: inventory.GetInventoryRevision(),
		CompilerRevision:  inventory.GetCompilerRevision(),
		Descriptors:       cloneDescriptors(filtered[cursor:end]),
	}
	if end < len(filtered) {
		response.NextPageToken, err = s.encodeToken(catalogPageToken{
			TenantID:          request.GetTenantId(),
			Roles:             enumNumbers(roles),
			Capabilities:      enumNumbers(capabilities),
			PolicyRevision:    decision.PolicyRevision,
			InventoryRevision: inventory.GetInventoryRevision(),
			Cursor:            end,
			ExpiresAt:         s.clock().UTC().Add(catalogTokenLifetime).Unix(),
		})
		if err != nil {
			return nil, status.Error(codes.Internal, "create catalog page token")
		}
	}
	return response, nil
}

func (s *ConnectorCatalogService) GetConnectorDescriptor(
	ctx context.Context, request *controlv1.GetConnectorDescriptorRequest,
) (*controlv1.GetConnectorDescriptorResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	if _, err := s.authorize(ctx, request.GetTenantId()); err != nil {
		return nil, err
	}
	if request.GetName() == "" || len(request.GetName()) > 128 {
		return nil, status.Error(codes.InvalidArgument, "connector name is invalid")
	}
	current, repositoryErr := s.repository.Current(ctx, s.executionProfile)
	if repositoryErr != nil {
		return nil, catalogReadError(repositoryErr)
	}
	snapshots := []catalog.Snapshot{current}
	if request.GetDescriptorRevision() != "" {
		retained, err := s.repository.ListRecent(ctx, s.executionProfile, retainedCatalogLimit)
		if err != nil {
			return nil, catalogReadError(err)
		}
		snapshots = retained
	}
	for _, snapshot := range snapshots {
		inventory, err := catalogproto.ParseSnapshot(snapshot)
		if err != nil {
			return nil, status.Error(codes.Internal, "retained connector catalog is invalid")
		}
		for _, descriptor := range inventory.GetDescriptors() {
			if descriptor.GetName() == request.GetName() &&
				(request.GetDescriptorRevision() == "" ||
					descriptor.GetDescriptorRevision() == request.GetDescriptorRevision()) {
				return &controlv1.GetConnectorDescriptorResponse{
					InventoryRevision:   inventory.GetInventoryRevision(),
					CompilerRevision:    inventory.GetCompilerRevision(),
					ConnectorDescriptor: proto.Clone(descriptor).(*controlv1.ConnectorDescriptor),
				}, nil
			}
		}
	}
	return nil, status.Error(codes.NotFound, "connector descriptor not found")
}

func (s *ConnectorCatalogService) authorize(ctx context.Context, tenantID string) (auth.Decision, error) {
	decision, err := s.authorizer.Authorize(ctx, tenantID, auth.PermissionConnectorsRead)
	if err == nil {
		return decision, nil
	}
	if errors.Is(err, auth.ErrUnauthenticated) {
		return auth.Decision{}, status.Error(codes.Unauthenticated, "authentication required")
	}
	return auth.Decision{}, status.Error(codes.PermissionDenied, "tenant access denied")
}

func catalogFilters(
	roles []controlv1.ConnectorRole, capabilities []controlv1.ConnectorCapability,
) ([]controlv1.ConnectorRole, []controlv1.ConnectorCapability, error) {
	roleCopy := append([]controlv1.ConnectorRole(nil), roles...)
	capabilityCopy := append([]controlv1.ConnectorCapability(nil), capabilities...)
	if !strictlyIncreasingRoles(roleCopy) || !strictlyIncreasingCapabilities(capabilityCopy) {
		return nil, nil, status.Error(codes.InvalidArgument, "catalog filters must be specified, unique, and ordered")
	}
	return roleCopy, capabilityCopy, nil
}

func strictlyIncreasingRoles(values []controlv1.ConnectorRole) bool {
	previous := controlv1.ConnectorRole(-1)
	for _, value := range values {
		if value == controlv1.ConnectorRole_CONNECTOR_ROLE_UNSPECIFIED || value <= previous {
			return false
		}
		previous = value
	}
	return true
}

func strictlyIncreasingCapabilities(values []controlv1.ConnectorCapability) bool {
	previous := controlv1.ConnectorCapability(-1)
	for _, value := range values {
		if value == controlv1.ConnectorCapability_CONNECTOR_CAPABILITY_UNSPECIFIED || value <= previous {
			return false
		}
		previous = value
	}
	return true
}

func filterDescriptors(
	descriptors []*controlv1.ConnectorDescriptor,
	roles []controlv1.ConnectorRole,
	capabilities []controlv1.ConnectorCapability,
) []*controlv1.ConnectorDescriptor {
	result := make([]*controlv1.ConnectorDescriptor, 0, len(descriptors))
	for _, descriptor := range descriptors {
		if containsAll(descriptor.GetRoles(), roles) && containsAll(descriptor.GetCapabilities(), capabilities) {
			result = append(result, descriptor)
		}
	}
	return result
}

func containsAll[E comparable](available, required []E) bool {
	set := make(map[E]struct{}, len(available))
	for _, value := range available {
		set[value] = struct{}{}
	}
	for _, value := range required {
		if _, found := set[value]; !found {
			return false
		}
	}
	return true
}

func cloneDescriptors(source []*controlv1.ConnectorDescriptor) []*controlv1.ConnectorDescriptor {
	result := make([]*controlv1.ConnectorDescriptor, len(source))
	for index, descriptor := range source {
		result[index] = proto.Clone(descriptor).(*controlv1.ConnectorDescriptor)
	}
	return result
}

func catalogReadError(err error) error {
	if errors.Is(err, catalog.ErrNotFound) {
		return status.Error(codes.Unavailable, "connector catalog is unavailable")
	}
	return status.Error(codes.Internal, "connector catalog read failed")
}

type catalogPageToken struct {
	TenantID          string  `json:"tenantId"`
	Roles             []int32 `json:"roles,omitempty"`
	Capabilities      []int32 `json:"capabilities,omitempty"`
	PolicyRevision    string  `json:"policyRevision"`
	InventoryRevision string  `json:"inventoryRevision"`
	Cursor            int     `json:"cursor"`
	ExpiresAt         int64   `json:"expiresAt"`
}

func (s *ConnectorCatalogService) encodeToken(claims catalogPageToken) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.tokenKey)
	_, _ = mac.Write([]byte(encoded))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + signature, nil
}

func (s *ConnectorCatalogService) decodeToken(token string) (catalogPageToken, error) {
	if len(token) > 4096 {
		return catalogPageToken{}, fmt.Errorf("token is too large")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return catalogPageToken{}, fmt.Errorf("token shape is invalid")
	}
	provided, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return catalogPageToken{}, err
	}
	mac := hmac.New(sha256.New, s.tokenKey)
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return catalogPageToken{}, fmt.Errorf("token signature is invalid")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return catalogPageToken{}, err
	}
	var claims catalogPageToken
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&claims); err != nil {
		return catalogPageToken{}, err
	}
	return claims, nil
}

func enumNumbers[E ~int32](values []E) []int32 {
	result := make([]int32, len(values))
	for index, value := range values {
		result[index] = int32(value)
	}
	return result
}

func equalInt32s(left, right []int32) bool {
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
