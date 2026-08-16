package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/auth"
)

const (
	defaultAuditPageSize = 50
	defaultAuditWindow   = 24 * time.Hour
	auditTokenLifetime   = 15 * time.Minute
	auditTokenVersion    = 1
)

var projectedAuditAttributes = map[string]struct{}{
	"afterState": {}, "afterVersion": {}, "beforeVersion": {}, "connectionUid": {},
	"connector": {}, "continued": {}, "descriptorRevision": {}, "epoch": {},
	"eventTypeCount": {}, "generation": {}, "hasPageToken": {}, "jobUid": {},
	"method": {}, "name": {}, "namespace": {}, "operationId": {},
	"outcomeCount": {}, "pageSize": {}, "providerKind": {}, "resultCode": {},
	"role": {}, "uid": {}, "validationId": {},
}

type AuditRepository interface {
	auth.AuditReader
	auth.AuditWriter
}

// AuditQueryMetrics records authorized audit-query latency with bounded
// request correlation.
type AuditQueryMetrics interface {
	ObserveAuditQuery(tenantID, requestID string, duration time.Duration)
}

// AuditServiceOption configures optional AuditService dependencies.
type AuditServiceOption func(*AuditService) error

// WithAuditQueryMetrics installs the observer for authorized audit queries.
func WithAuditQueryMetrics(observer AuditQueryMetrics) AuditServiceOption {
	return func(service *AuditService) error {
		if observer == nil {
			return fmt.Errorf("audit query metrics must not be nil")
		}
		service.queryMetrics = observer
		return nil
	}
}

type AuditService struct {
	controlv1.UnimplementedAuditServiceServer
	repository   AuditRepository
	authorizer   auth.Authorizer
	tokenKey     []byte
	clock        func() time.Time
	uid          func() string
	queryMetrics AuditQueryMetrics
}

func NewAuditService(
	repository AuditRepository,
	authorizer auth.Authorizer,
	tokenKey []byte,
	clock func() time.Time,
	uid func() string,
	options ...AuditServiceOption,
) (*AuditService, error) {
	if repository == nil || authorizer == nil || clock == nil || uid == nil {
		return nil, fmt.Errorf("audit service dependencies must not be nil")
	}
	if len(tokenKey) < 32 {
		return nil, fmt.Errorf("audit page-token key must contain at least 32 bytes")
	}
	service := &AuditService{
		repository: repository, authorizer: authorizer,
		tokenKey: append([]byte(nil), tokenKey...), clock: clock, uid: uid,
	}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("audit service option must not be nil")
		}
		if err := option(service); err != nil {
			return nil, err
		}
	}
	return service, nil
}

func (s *AuditService) ListAuditEvents(
	ctx context.Context, request *controlv1.ListAuditEventsRequest,
) (*controlv1.ListAuditEventsResponse, error) {
	startedAt := s.clock()
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	decision, err := s.authorize(ctx, request.GetTenantId())
	if err != nil {
		return nil, err
	}
	queryRequestID := requestID(ctx, s.uid)
	if s.queryMetrics != nil {
		defer func() {
			duration := s.clock().Sub(startedAt)
			if duration < 0 {
				duration = 0
			}
			s.queryMetrics.ObserveAuditQuery(decision.TenantID, queryRequestID, duration)
		}()
	}
	pageSize := request.GetPageSize()
	if pageSize == 0 && request.GetPageToken() == "" {
		pageSize = defaultAuditPageSize
	}
	if pageSize < 0 || pageSize > auth.MaximumAuditPageSize {
		return nil, status.Errorf(codes.InvalidArgument, "page_size must be between 1 and %d", auth.MaximumAuditPageSize)
	}

	now := s.clock().UTC()
	query, claims, err := s.query(request, decision, int(pageSize), now)
	if err != nil {
		return nil, err
	}
	if pageSize == 0 {
		pageSize = int32(claims.PageSize)
	}
	events, repositoryErr := s.repository.ListSecurityAudit(ctx, query)
	if repositoryErr != nil {
		return nil, status.Error(codes.Internal, "audit query failed")
	}
	hasMore := len(events) > int(pageSize)
	if hasMore {
		events = events[:pageSize]
	}

	response := &controlv1.ListAuditEventsResponse{SnapshotTime: timestamppb.New(query.OccurredBefore)}
	response.Events = make([]*controlv1.AuditEvent, 0, len(events))
	for _, event := range events {
		projected, projectionErr := auditEventToProto(event)
		if projectionErr != nil {
			return nil, status.Error(codes.Internal, "stored audit event is invalid")
		}
		response.Events = append(response.Events, projected)
	}
	if hasMore {
		last := events[len(events)-1]
		claims.CursorUnixNano = last.OccurredAt.UnixNano()
		claims.CursorEventID = last.EventID
		response.NextPageToken, err = s.encodeToken(claims)
		if err != nil {
			return nil, status.Error(codes.Internal, "create audit page token")
		}
	}
	actorID := decision.Principal.ID
	if actorID == "" {
		actorID = decision.Principal.Subject
	}
	if err := s.repository.WriteSecurityAudit(ctx, auth.SecurityAuditEvent{
		EventID: s.uid(), EventType: "audit.list", ActorID: actorID,
		TenantID: request.GetTenantId(), RequestID: queryRequestID, Outcome: "ALLOWED",
		Attributes: map[string]any{
			"pageSize": int(pageSize), "eventTypeCount": len(query.EventTypes),
			"outcomeCount": len(query.Outcomes), "hasPageToken": request.GetPageToken() != "",
		},
		OccurredAt: now,
	}); err != nil {
		return nil, status.Error(codes.Internal, "audit read could not be recorded")
	}
	return response, nil
}

func (s *AuditService) query(
	request *controlv1.ListAuditEventsRequest, decision auth.Decision, pageSize int, now time.Time,
) (auth.SecurityAuditQuery, auditPageToken, error) {
	if request.GetPageToken() != "" {
		if request.GetOccurredAfter() != nil || request.GetOccurredBefore() != nil ||
			len(request.GetEventTypes()) != 0 || len(request.GetOutcomes()) != 0 {
			return auth.SecurityAuditQuery{}, auditPageToken{},
				status.Error(codes.InvalidArgument, "audit page token must not be combined with filters")
		}
		claims, err := s.decodeToken(request.GetPageToken())
		if err != nil || claims.Version != auditTokenVersion || claims.ExpiresAt <= now.Unix() ||
			claims.TenantID != request.GetTenantId() || claims.PolicyRevision != decision.PolicyRevision ||
			(pageSize != 0 && claims.PageSize != pageSize) || claims.CursorUnixNano == 0 ||
			claims.CursorEventID == "" {
			return auth.SecurityAuditQuery{}, auditPageToken{},
				status.Error(codes.InvalidArgument, "audit page token scope mismatch or expired")
		}
		query := claims.query()
		if err := query.Validate(); err != nil {
			return auth.SecurityAuditQuery{}, auditPageToken{}, status.Error(codes.InvalidArgument, "audit page token is invalid")
		}
		return query, claims, nil
	}

	before := now
	if request.GetOccurredBefore() != nil {
		var err error
		before, err = checkedTimestamp(request.GetOccurredBefore())
		if err != nil || before.After(now) {
			return auth.SecurityAuditQuery{}, auditPageToken{}, status.Error(codes.InvalidArgument, "occurred_before is invalid")
		}
	}
	after := before.Add(-defaultAuditWindow)
	if request.GetOccurredAfter() != nil {
		var err error
		after, err = checkedTimestamp(request.GetOccurredAfter())
		if err != nil {
			return auth.SecurityAuditQuery{}, auditPageToken{}, status.Error(codes.InvalidArgument, "occurred_after is invalid")
		}
	}
	query := auth.SecurityAuditQuery{
		TenantID: request.GetTenantId(), OccurredAfter: after, OccurredBefore: before,
		EventTypes: append([]string(nil), request.GetEventTypes()...),
		Outcomes:   append([]string(nil), request.GetOutcomes()...), Limit: pageSize + 1,
	}
	if err := query.Validate(); err != nil {
		return auth.SecurityAuditQuery{}, auditPageToken{}, status.Error(codes.InvalidArgument, "audit query is invalid")
	}
	claims := auditPageToken{
		Version: auditTokenVersion, TenantID: request.GetTenantId(), PolicyRevision: decision.PolicyRevision,
		AfterUnixNano: after.UnixNano(), SnapshotUnixNano: before.UnixNano(),
		EventTypes: append([]string(nil), query.EventTypes...), Outcomes: append([]string(nil), query.Outcomes...),
		PageSize: pageSize, ExpiresAt: now.Add(auditTokenLifetime).Unix(),
	}
	return query, claims, nil
}

func (s *AuditService) authorize(ctx context.Context, tenantID string) (auth.Decision, error) {
	decision, err := s.authorizer.Authorize(ctx, tenantID, auth.PermissionAuditRead)
	if err == nil {
		return decision, nil
	}
	if errors.Is(err, auth.ErrUnauthenticated) {
		return auth.Decision{}, status.Error(codes.Unauthenticated, "authentication required")
	}
	return auth.Decision{}, status.Error(codes.PermissionDenied, "tenant access denied")
}

func checkedTimestamp(value *timestamppb.Timestamp) (time.Time, error) {
	if value == nil || value.CheckValid() != nil {
		return time.Time{}, fmt.Errorf("timestamp is invalid")
	}
	return value.AsTime().UTC(), nil
}

func auditEventToProto(event auth.SecurityAuditEvent) (*controlv1.AuditEvent, error) {
	if err := event.Validate(); err != nil {
		return nil, err
	}
	occurredAt := timestamppb.New(event.OccurredAt.UTC())
	if occurredAt.CheckValid() != nil {
		return nil, fmt.Errorf("audit event timestamp is invalid")
	}
	return &controlv1.AuditEvent{
		EventId: event.EventID, EventType: event.EventType, ActorId: event.ActorID,
		RequestId: event.RequestID, Outcome: event.Outcome,
		Attributes: safeAuditAttributes(event.Attributes), OccurredAt: occurredAt,
	}, nil
}

func safeAuditAttributes(source map[string]any) map[string]string {
	keys := make([]string, 0, len(source))
	for key := range source {
		if _, allowed := projectedAuditAttributes[key]; allowed {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := safeAuditScalar(source[key]); ok {
			result[key] = value
		}
	}
	return result
}

func safeAuditScalar(value any) (string, bool) {
	var result string
	switch typed := value.(type) {
	case string:
		result = typed
	case bool:
		result = strconv.FormatBool(typed)
	case json.Number:
		if _, err := strconv.ParseFloat(string(typed), 64); err != nil {
			return "", false
		}
		result = string(typed)
	case int:
		result = strconv.Itoa(typed)
	case int32:
		result = strconv.FormatInt(int64(typed), 10)
	case int64:
		result = strconv.FormatInt(typed, 10)
	case float64:
		if math.IsInf(typed, 0) || math.IsNaN(typed) {
			return "", false
		}
		result = strconv.FormatFloat(typed, 'g', -1, 64)
	default:
		return "", false
	}
	if len(result) > 512 || strings.ContainsRune(result, '\x00') {
		return "", false
	}
	return result, true
}

type auditPageToken struct {
	Version          int      `json:"version"`
	TenantID         string   `json:"tenantId"`
	PolicyRevision   string   `json:"policyRevision"`
	AfterUnixNano    int64    `json:"afterUnixNano"`
	SnapshotUnixNano int64    `json:"snapshotUnixNano"`
	EventTypes       []string `json:"eventTypes,omitempty"`
	Outcomes         []string `json:"outcomes,omitempty"`
	PageSize         int      `json:"pageSize"`
	CursorUnixNano   int64    `json:"cursorUnixNano,omitempty"`
	CursorEventID    string   `json:"cursorEventId,omitempty"`
	ExpiresAt        int64    `json:"expiresAt"`
}

func (t auditPageToken) query() auth.SecurityAuditQuery {
	return auth.SecurityAuditQuery{
		TenantID: t.TenantID, OccurredAfter: time.Unix(0, t.AfterUnixNano).UTC(),
		OccurredBefore: time.Unix(0, t.SnapshotUnixNano).UTC(),
		EventTypes:     append([]string(nil), t.EventTypes...), Outcomes: append([]string(nil), t.Outcomes...),
		Cursor: &auth.SecurityAuditCursor{
			OccurredAt: time.Unix(0, t.CursorUnixNano).UTC(), EventID: t.CursorEventID,
		},
		Limit: t.PageSize + 1,
	}
}

func (s *AuditService) encodeToken(claims auditPageToken) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.tokenKey)
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *AuditService) decodeToken(token string) (auditPageToken, error) {
	if len(token) > 4096 {
		return auditPageToken{}, fmt.Errorf("token is too large")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return auditPageToken{}, fmt.Errorf("token shape is invalid")
	}
	provided, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return auditPageToken{}, err
	}
	mac := hmac.New(sha256.New, s.tokenKey)
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return auditPageToken{}, fmt.Errorf("token signature is invalid")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return auditPageToken{}, err
	}
	var claims auditPageToken
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&claims); err != nil {
		return auditPageToken{}, err
	}
	return claims, nil
}
