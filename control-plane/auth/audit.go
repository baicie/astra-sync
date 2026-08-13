package auth

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	MaximumAuditFilterValues = 20
	MaximumAuditPageSize     = 100
	MaximumAuditQueryWindow  = 90 * 24 * time.Hour
)

var (
	auditEventTypePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$`)
	auditOutcomePattern   = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,31}$`)
)

type SecurityAuditEvent struct {
	EventID    string
	EventType  string
	ActorID    string
	TenantID   string
	RequestID  string
	Outcome    string
	Attributes map[string]any
	OccurredAt time.Time
}

func (e SecurityAuditEvent) Validate() error {
	if strings.TrimSpace(e.EventID) == "" || len(e.EventID) > 128 ||
		strings.TrimSpace(e.EventType) == "" || len(e.EventType) > 128 ||
		strings.TrimSpace(e.ActorID) == "" || len(e.ActorID) > 256 ||
		strings.TrimSpace(e.RequestID) == "" || len(e.RequestID) > 128 ||
		strings.TrimSpace(e.Outcome) == "" || len(e.Outcome) > 32 || e.OccurredAt.IsZero() {
		return fmt.Errorf("security audit event is invalid")
	}
	if e.TenantID != "" && !tenantIDPattern.MatchString(e.TenantID) {
		return fmt.Errorf("security audit tenant ID is invalid")
	}
	if len(e.Attributes) > 32 {
		return fmt.Errorf("security audit attributes exceed the supported count")
	}
	return nil
}

type AuditWriter interface {
	WriteSecurityAudit(context.Context, SecurityAuditEvent) error
}

type SecurityAuditCursor struct {
	OccurredAt time.Time
	EventID    string
}

type SecurityAuditQuery struct {
	TenantID       string
	OccurredAfter  time.Time
	OccurredBefore time.Time
	EventTypes     []string
	Outcomes       []string
	Cursor         *SecurityAuditCursor
	Limit          int
}

func (q SecurityAuditQuery) Validate() error {
	if !tenantIDPattern.MatchString(q.TenantID) {
		return fmt.Errorf("security audit query tenant ID is invalid")
	}
	if q.OccurredAfter.IsZero() || q.OccurredBefore.IsZero() ||
		!q.OccurredAfter.Before(q.OccurredBefore) ||
		q.OccurredBefore.Sub(q.OccurredAfter) > MaximumAuditQueryWindow {
		return fmt.Errorf("security audit query time window is invalid")
	}
	if q.Limit <= 0 || q.Limit > MaximumAuditPageSize+1 {
		return fmt.Errorf("security audit query limit is invalid")
	}
	if err := validateAuditFilter(q.EventTypes, auditEventTypePattern, 128); err != nil {
		return fmt.Errorf("security audit event-type filter is invalid: %w", err)
	}
	if err := validateAuditFilter(q.Outcomes, auditOutcomePattern, 32); err != nil {
		return fmt.Errorf("security audit outcome filter is invalid: %w", err)
	}
	if q.Cursor != nil {
		if q.Cursor.OccurredAt.IsZero() || q.Cursor.OccurredAt.Before(q.OccurredAfter) ||
			q.Cursor.OccurredAt.After(q.OccurredBefore) || strings.TrimSpace(q.Cursor.EventID) == "" ||
			len(q.Cursor.EventID) > 128 || strings.ContainsAny(q.Cursor.EventID, "\x00\r\n") {
			return fmt.Errorf("security audit cursor is invalid")
		}
	}
	return nil
}

func validateAuditFilter(values []string, pattern *regexp.Regexp, maximumLength int) error {
	if len(values) > MaximumAuditFilterValues {
		return fmt.Errorf("too many values")
	}
	previous := ""
	for _, value := range values {
		if len(value) > maximumLength || !pattern.MatchString(value) || value <= previous {
			return fmt.Errorf("values must be valid, unique, and ordered")
		}
		previous = value
	}
	return nil
}

type AuditReader interface {
	ListSecurityAudit(context.Context, SecurityAuditQuery) ([]SecurityAuditEvent, error)
}
