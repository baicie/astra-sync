package auth_test

import (
	"fmt"
	"testing"
	"time"

	"io.astrasync/control-plane/auth"
)

const queryTenantID = "5dd716bf-c2ea-423c-8ef7-0ac8c8dd83ba"

func TestSecurityAuditQueryEnforcesBoundsAndCanonicalFilters(t *testing.T) {
	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)
	valid := auth.SecurityAuditQuery{
		TenantID: queryTenantID, OccurredAfter: now.Add(-90 * 24 * time.Hour),
		OccurredBefore: now, EventTypes: []string{"authorization.denied", "job.start"},
		Outcomes: []string{"CHANGED", "PERMISSION_DENIED"}, Limit: auth.MaximumAuditPageSize + 1,
		Cursor: &auth.SecurityAuditCursor{OccurredAt: now.Add(-time.Hour), EventID: "event-1"},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("maximum bounded audit query should be valid: %v", err)
	}

	tests := map[string]func(*auth.SecurityAuditQuery){
		"non-canonical tenant": func(query *auth.SecurityAuditQuery) { query.TenantID = "tenant-one" },
		"window over maximum": func(query *auth.SecurityAuditQuery) {
			query.OccurredAfter = query.OccurredAfter.Add(-time.Nanosecond)
		},
		"page over maximum": func(query *auth.SecurityAuditQuery) { query.Limit++ },
		"zero page":         func(query *auth.SecurityAuditQuery) { query.Limit = 0 },
		"too many event types": func(query *auth.SecurityAuditQuery) {
			query.EventTypes = make([]string, auth.MaximumAuditFilterValues+1)
			for index := range query.EventTypes {
				query.EventTypes[index] = fmt.Sprintf("event.%02d", index)
			}
		},
		"unordered event types": func(query *auth.SecurityAuditQuery) {
			query.EventTypes = []string{"job.stop", "job.start"}
		},
		"duplicate outcomes": func(query *auth.SecurityAuditQuery) {
			query.Outcomes = []string{"CHANGED", "CHANGED"}
		},
		"invalid outcome": func(query *auth.SecurityAuditQuery) { query.Outcomes = []string{"changed"} },
		"cursor before window": func(query *auth.SecurityAuditQuery) {
			query.Cursor = &auth.SecurityAuditCursor{OccurredAt: query.OccurredAfter.Add(-time.Nanosecond), EventID: "event-1"}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			query := valid
			query.EventTypes = append([]string(nil), valid.EventTypes...)
			query.Outcomes = append([]string(nil), valid.Outcomes...)
			cursor := *valid.Cursor
			query.Cursor = &cursor
			mutate(&query)
			if err := query.Validate(); err == nil {
				t.Fatal("expected invalid audit query")
			}
		})
	}
}
