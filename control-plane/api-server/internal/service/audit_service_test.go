package service_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/api-server/internal/service"
	"io.astrasync/control-plane/auth"
)

const (
	auditTenantID      = "8d58d674-7cc7-4b15-a46c-9e7768bbf103"
	otherAuditTenantID = "5cb7ba31-06c0-4aa6-a025-bfc17d14e73d"
)

func TestAuditServicePaginatesAndProjectsOnlyReviewedAttributes(t *testing.T) {
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)
	repository := &fakeAuditRepository{events: []auth.SecurityAuditEvent{
		auditEvent("event-3", auditTenantID, now.Add(-time.Minute), "job.update", "CHANGED", map[string]any{
			"name": "orders", "afterVersion": 3, "secret": "must-not-leak", "nested": map[string]any{"x": "y"},
			"resultCode": strings.Repeat("x", 513), "epoch": math.NaN(),
		}),
		auditEvent("event-2", auditTenantID, now.Add(-2*time.Minute), "connection.rotate", "CHANGED", map[string]any{
			"uid": "connection-uid", "providerKind": "KUBERNETES_SECRET_V1",
		}),
		auditEvent("event-1", auditTenantID, now.Add(-3*time.Minute), "authorization.denied", "TENANT_DENIED", map[string]any{
			"method": "/astra.control.v1.JobService/GetJob", "raw": "hidden",
		}),
	}}
	auditService, err := service.NewAuditService(repository, auth.DevelopmentAuthorizer{}, []byte("01234567890123456789012345678901"), func() time.Time {
		return now
	}, func() string { return fmt.Sprintf("audit-read-%d", len(repository.writes)+1) })
	if err != nil {
		t.Fatalf("new audit service: %v", err)
	}

	first, err := auditService.ListAuditEvents(context.Background(), &controlv1.ListAuditEventsRequest{
		TenantId: auditTenantID, PageSize: 1,
		EventTypes: []string{"authorization.denied", "connection.rotate", "job.update"},
		Outcomes:   []string{"CHANGED", "TENANT_DENIED"},
	})
	if err != nil {
		t.Fatalf("first audit page: %v", err)
	}
	if len(first.GetEvents()) != 1 || first.GetEvents()[0].GetEventId() != "event-3" || first.GetNextPageToken() == "" {
		t.Fatalf("unexpected first audit page: %+v", first)
	}
	if _, leaked := first.GetEvents()[0].GetAttributes()["secret"]; leaked {
		t.Fatal("unreviewed audit attribute was projected")
	}
	if _, leaked := first.GetEvents()[0].GetAttributes()["nested"]; leaked {
		t.Fatal("nested audit attribute was projected")
	}
	if first.GetEvents()[0].GetAttributes()["afterVersion"] != "3" {
		t.Fatalf("numeric audit attribute was not normalized: %+v", first.GetEvents()[0].GetAttributes())
	}
	if _, leaked := first.GetEvents()[0].GetAttributes()["resultCode"]; leaked {
		t.Fatal("oversized audit attribute was projected")
	}
	if _, leaked := first.GetEvents()[0].GetAttributes()["epoch"]; leaked {
		t.Fatal("non-finite audit attribute was projected")
	}
	if first.GetSnapshotTime() == nil || !first.GetSnapshotTime().AsTime().Equal(now) {
		t.Fatalf("unexpected audit snapshot: %v", first.GetSnapshotTime())
	}
	if len(repository.writes) != 1 || repository.writes[0].EventType != "audit.list" {
		t.Fatalf("successful audit read was not recorded: %+v", repository.writes)
	}

	second, err := auditService.ListAuditEvents(context.Background(), &controlv1.ListAuditEventsRequest{
		TenantId: auditTenantID, PageToken: first.GetNextPageToken(),
	})
	if err != nil {
		t.Fatalf("continuation audit page: %v", err)
	}
	if len(second.GetEvents()) != 1 || second.GetEvents()[0].GetEventId() != "event-2" || second.GetNextPageToken() == "" {
		t.Fatalf("unexpected continuation audit page: %+v", second)
	}
	if repository.queries[1].Limit != 2 || repository.queries[1].TenantID != auditTenantID ||
		len(repository.queries[1].EventTypes) != 3 || len(repository.queries[1].Outcomes) != 2 {
		t.Fatalf("continuation query was not bounded or tenant-scoped: %+v", repository.queries[1])
	}
	third, err := auditService.ListAuditEvents(context.Background(), &controlv1.ListAuditEventsRequest{
		TenantId: auditTenantID, PageToken: second.GetNextPageToken(),
	})
	if err != nil || len(third.GetEvents()) != 1 || third.GetEvents()[0].GetEventId() != "event-1" ||
		third.GetNextPageToken() != "" {
		t.Fatalf("unexpected final audit page: page=%+v err=%v", third, err)
	}
}

func TestAuditServiceRejectsTamperedTokenAndFailsClosedOnReadAudit(t *testing.T) {
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)
	repository := &fakeAuditRepository{events: []auth.SecurityAuditEvent{
		auditEvent("event-1", auditTenantID, now.Add(-time.Minute), "job.start", "CHANGED", nil),
		auditEvent("event-2", auditTenantID, now.Add(-2*time.Minute), "job.stop", "CHANGED", nil),
	}}
	serviceUnderTest, err := service.NewAuditService(repository, auth.DevelopmentAuthorizer{}, []byte("01234567890123456789012345678901"), func() time.Time { return now }, func() string { return "read-event" })
	if err != nil {
		t.Fatalf("new audit service: %v", err)
	}
	page, err := serviceUnderTest.ListAuditEvents(context.Background(), &controlv1.ListAuditEventsRequest{TenantId: auditTenantID, PageSize: 1})
	if err != nil {
		t.Fatalf("create page token: %v", err)
	}
	tampered := page.GetNextPageToken() + "x"
	if _, err := serviceUnderTest.ListAuditEvents(context.Background(), &controlv1.ListAuditEventsRequest{TenantId: auditTenantID, PageToken: tampered}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected tampered token rejection, got %v", err)
	}

	repository.writeErr = errors.New("audit table unavailable")
	if _, err := serviceUnderTest.ListAuditEvents(context.Background(), &controlv1.ListAuditEventsRequest{TenantId: auditTenantID}); status.Code(err) != codes.Internal {
		t.Fatalf("expected read-audit failure to fail closed, got %v", err)
	}
	repository.writeErr = nil
	repository.readErr = errors.New("postgres detail must remain private")
	_, err = serviceUnderTest.ListAuditEvents(context.Background(), &controlv1.ListAuditEventsRequest{TenantId: auditTenantID})
	if status.Code(err) != codes.Internal || strings.Contains(status.Convert(err).Message(), "postgres detail") {
		t.Fatalf("expected sanitized repository failure, got %v", err)
	}
}

func TestAuditServiceAppliesDefaultAndMaximumBounds(t *testing.T) {
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)
	repository := &fakeAuditRepository{}
	serviceUnderTest, err := service.NewAuditService(
		repository, auth.DevelopmentAuthorizer{}, []byte("01234567890123456789012345678901"),
		func() time.Time { return now }, func() string { return "audit-read" },
	)
	if err != nil {
		t.Fatalf("new audit service: %v", err)
	}
	response, err := serviceUnderTest.ListAuditEvents(context.Background(), &controlv1.ListAuditEventsRequest{
		TenantId: auditTenantID,
	})
	if err != nil || len(repository.queries) != 1 {
		t.Fatalf("default audit query: response=%+v queries=%+v err=%v", response, repository.queries, err)
	}
	query := repository.queries[0]
	if query.Limit != 51 || !query.OccurredAfter.Equal(now.Add(-24*time.Hour)) ||
		!query.OccurredBefore.Equal(now) {
		t.Fatalf("unexpected default audit bounds: %+v", query)
	}
	if _, err := serviceUnderTest.ListAuditEvents(context.Background(), &controlv1.ListAuditEventsRequest{
		TenantId: auditTenantID, PageSize: auth.MaximumAuditPageSize + 1,
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected oversized page rejection, got %v", err)
	}
	if _, err := serviceUnderTest.ListAuditEvents(context.Background(), &controlv1.ListAuditEventsRequest{
		TenantId:       auditTenantID,
		OccurredAfter:  timestamppb.New(now.Add(-auth.MaximumAuditQueryWindow - time.Nanosecond)),
		OccurredBefore: timestamppb.New(now),
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected oversized window rejection, got %v", err)
	}
	if len(repository.queries) != 1 {
		t.Fatalf("invalid bounds reached the repository: queries=%d", len(repository.queries))
	}
}

func TestAuditServiceRejectsExpiredAndScopeMismatchedTokens(t *testing.T) {
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)
	currentTime := now
	repository := &fakeAuditRepository{events: []auth.SecurityAuditEvent{
		auditEvent("event-2", auditTenantID, now.Add(-time.Minute), "job.start", "CHANGED", nil),
		auditEvent("event-1", auditTenantID, now.Add(-2*time.Minute), "job.stop", "CHANGED", nil),
	}}
	authorizer := &revisionAuditAuthorizer{revision: "7"}
	serviceUnderTest, err := service.NewAuditService(
		repository, authorizer, []byte("01234567890123456789012345678901"),
		func() time.Time { return currentTime }, func() string { return "audit-read" },
	)
	if err != nil {
		t.Fatalf("new audit service: %v", err)
	}
	page, err := serviceUnderTest.ListAuditEvents(context.Background(), &controlv1.ListAuditEventsRequest{
		TenantId: auditTenantID, PageSize: 1,
	})
	if err != nil || page.GetNextPageToken() == "" {
		t.Fatalf("create scoped page token: page=%+v err=%v", page, err)
	}

	currentTime = now.Add(16 * time.Minute)
	if _, err := serviceUnderTest.ListAuditEvents(context.Background(), &controlv1.ListAuditEventsRequest{
		TenantId: auditTenantID, PageToken: page.GetNextPageToken(),
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected expired token rejection, got %v", err)
	}
	currentTime = now
	if _, err := serviceUnderTest.ListAuditEvents(context.Background(), &controlv1.ListAuditEventsRequest{
		TenantId: otherAuditTenantID, PageToken: page.GetNextPageToken(),
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected cross-tenant token rejection, got %v", err)
	}
	authorizer.revision = "8"
	if _, err := serviceUnderTest.ListAuditEvents(context.Background(), &controlv1.ListAuditEventsRequest{
		TenantId: auditTenantID, PageToken: page.GetNextPageToken(),
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected policy-revision token rejection, got %v", err)
	}
	if len(repository.queries) != 1 {
		t.Fatalf("invalid tokens reached the repository: queries=%d", len(repository.queries))
	}
}

func TestAuditServiceRejectsUnorderedFiltersAndUnauthorizedReaders(t *testing.T) {
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)
	repository := &fakeAuditRepository{}
	serviceUnderTest, err := service.NewAuditService(repository, auth.DevelopmentAuthorizer{}, []byte("01234567890123456789012345678901"), func() time.Time { return now }, func() string { return "audit-read" })
	if err != nil {
		t.Fatalf("new audit service: %v", err)
	}
	_, err = serviceUnderTest.ListAuditEvents(context.Background(), &controlv1.ListAuditEventsRequest{
		TenantId: auditTenantID, EventTypes: []string{"job.stop", "job.create"},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected unordered filter rejection, got %v", err)
	}

	serviceUnderTest, err = service.NewAuditService(repository, auth.ContextAuthorizer{}, []byte("01234567890123456789012345678901"), func() time.Time { return now }, func() string { return "audit-read" })
	if err != nil {
		t.Fatalf("new unauthorized audit service: %v", err)
	}
	_, err = serviceUnderTest.ListAuditEvents(context.Background(), &controlv1.ListAuditEventsRequest{TenantId: auditTenantID})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected unauthenticated denial, got %v", err)
	}
	membership, err := auth.NewMembership(auditTenantID, true, auth.PermissionJobsRead)
	if err != nil {
		t.Fatalf("create restricted membership: %v", err)
	}
	restrictedContext, err := auth.WithPrincipal(context.Background(), auth.Principal{
		ID: "principal-1", Subject: "operator-1", Active: true, PolicyRevision: "1",
		Memberships: map[string]auth.Membership{auditTenantID: membership},
	})
	if err != nil {
		t.Fatalf("create restricted principal context: %v", err)
	}
	_, err = serviceUnderTest.ListAuditEvents(restrictedContext, &controlv1.ListAuditEventsRequest{TenantId: auditTenantID})
	if status.Code(err) != codes.PermissionDenied || len(repository.queries) != 0 {
		t.Fatalf("expected permission denial before repository read, queries=%d err=%v", len(repository.queries), err)
	}
}

type fakeAuditRepository struct {
	events   []auth.SecurityAuditEvent
	queries  []auth.SecurityAuditQuery
	writes   []auth.SecurityAuditEvent
	readErr  error
	writeErr error
}

type revisionAuditAuthorizer struct {
	revision string
}

func (a *revisionAuditAuthorizer) Authorize(
	_ context.Context, tenantID string, permission auth.Permission,
) (auth.Decision, error) {
	membership, err := auth.NewMembership(tenantID, true, permission)
	if err != nil {
		return auth.Decision{}, err
	}
	return auth.Decision{
		Principal: auth.Principal{
			ID: "principal-1", Subject: "operator-1", Active: true, PolicyRevision: a.revision,
		},
		Membership: membership, TenantID: tenantID, Permission: permission, PolicyRevision: a.revision,
	}, nil
}

func (r *fakeAuditRepository) ListSecurityAudit(_ context.Context, query auth.SecurityAuditQuery) ([]auth.SecurityAuditEvent, error) {
	r.queries = append(r.queries, query)
	if r.readErr != nil {
		return nil, r.readErr
	}
	result := make([]auth.SecurityAuditEvent, len(r.events))
	copy(result, r.events)
	if query.Cursor != nil {
		filtered := result[:0]
		for _, event := range result {
			if event.OccurredAt.Before(query.Cursor.OccurredAt) ||
				(event.OccurredAt.Equal(query.Cursor.OccurredAt) && event.EventID < query.Cursor.EventID) {
				filtered = append(filtered, event)
			}
		}
		result = filtered
	}
	if len(result) > query.Limit {
		result = result[:query.Limit]
	}
	return result, nil
}

func (r *fakeAuditRepository) WriteSecurityAudit(_ context.Context, event auth.SecurityAuditEvent) error {
	if r.writeErr != nil {
		return r.writeErr
	}
	r.writes = append(r.writes, event)
	return nil
}

func auditEvent(id, tenant string, occurredAt time.Time, eventType, outcome string, attributes map[string]any) auth.SecurityAuditEvent {
	return auth.SecurityAuditEvent{
		EventID: id, EventType: eventType, ActorID: "principal-1", TenantID: tenant,
		RequestID: "request-" + id, Outcome: outcome, Attributes: attributes, OccurredAt: occurredAt,
	}
}
