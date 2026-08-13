package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	jobv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/auth"
)

func (s *Server) listAuditEvents(response http.ResponseWriter, request *http.Request) {
	selected, session, err := s.scope(request)
	if err != nil {
		writeError(response, err)
		return
	}
	if !selected.membership.Has(auth.PermissionAuditRead) {
		writeError(response, status.Error(codes.PermissionDenied, "audit access denied"))
		return
	}
	pageSize, err := queryInt(request, "page_size", 0)
	if err != nil || pageSize < 0 || pageSize > auth.MaximumAuditPageSize {
		writeError(response, status.Error(codes.InvalidArgument, "audit pagination is invalid"))
		return
	}
	pageToken := strings.TrimSpace(request.URL.Query().Get("page_token"))
	if len(pageToken) > 4096 {
		writeError(response, status.Error(codes.InvalidArgument, "audit page token is invalid"))
		return
	}
	eventTypes, err := auditFilterValues(request.URL.Query()["event_type"], 128)
	if err != nil {
		writeError(response, status.Error(codes.InvalidArgument, "audit event-type filter is invalid"))
		return
	}
	outcomes, err := auditFilterValues(request.URL.Query()["outcome"], 32)
	if err != nil {
		writeError(response, status.Error(codes.InvalidArgument, "audit outcome filter is invalid"))
		return
	}
	after, err := auditTimestamp(request.URL.Query().Get("from"))
	if err != nil {
		writeError(response, status.Error(codes.InvalidArgument, "audit lower time bound is invalid"))
		return
	}
	before, err := auditTimestamp(request.URL.Query().Get("to"))
	if err != nil {
		writeError(response, status.Error(codes.InvalidArgument, "audit upper time bound is invalid"))
		return
	}
	if pageToken != "" && (after != nil || before != nil || len(eventTypes) != 0 || len(outcomes) != 0) {
		writeError(response, status.Error(codes.InvalidArgument, "audit page token cannot be combined with filters"))
		return
	}
	ctx, cancel := s.backendContext(request, session, readTimeout)
	defer cancel()
	result, err := s.audit.ListAuditEvents(ctx, &jobv1.ListAuditEventsRequest{
		TenantId: selected.tenantID, OccurredAfter: after, OccurredBefore: before,
		EventTypes: eventTypes, Outcomes: outcomes, PageSize: int32(pageSize), PageToken: pageToken,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	s.setScope(response, selected)
	writeProtoJSON(response, result)
}

func auditFilterValues(values []string, maximumLength int) ([]string, error) {
	if len(values) > auth.MaximumAuditFilterValues {
		return nil, fmt.Errorf("too many audit filter values")
	}
	result := make([]string, len(values))
	for index, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || trimmed != value || len(value) > maximumLength || strings.ContainsAny(value, "\x00\r\n") {
			return nil, fmt.Errorf("audit filter value is invalid")
		}
		result[index] = value
	}
	return result, nil
}

func auditTimestamp(value string) (*timestamppb.Timestamp, error) {
	if value == "" {
		return nil, nil
	}
	if len(value) > 64 || strings.TrimSpace(value) != value {
		return nil, fmt.Errorf("audit timestamp is invalid")
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, fmt.Errorf("parse audit timestamp: %w", err)
	}
	result := timestamppb.New(parsed.UTC())
	if err := result.CheckValid(); err != nil {
		return nil, fmt.Errorf("audit timestamp is invalid: %w", err)
	}
	return result, nil
}
