package auth

import (
	"context"
	"fmt"
	"strings"
	"time"
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
