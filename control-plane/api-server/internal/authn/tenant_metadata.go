// Package authn: server-side tenant-id metadata consumption (Phase 29 /
// ADR-074). The extractor is a pure function over `metadata.MD`. The
// reconciliation against `principal.Membership.TenantID` lives in the
// interceptor and is covered by interceptor_test.go.
package authn

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc/metadata"
)

// TenantMetadataKey is the incoming gRPC metadata key carrying the
// tenant-id envelope on mutation paths. Matches ADR-072 §1 (kebab-case,
// lowercase) and the kubebuilder label convention from ADR-071 §4.
const TenantMetadataKey = "x-astra-tenant-id"

// JobTenantIDFromIncomingMetadata extracts a single canonical-UUID
// tenant-id value from the incoming gRPC metadata.
//
// The function returns:
//
//   - (value, true, nil)  when the header is present and canonical
//   - ("",     false, nil) when the header is absent
//   - ("",     false, err) when the header is malformed (multi-value or
//     non-canonical UUID)
//
// The third outcome is a *defect signal*, not a fallthrough path; the
// interceptor must reject the request when err != nil.
func JobTenantIDFromIncomingMetadata(ctx context.Context) (string, bool, error) {
	if ctx == nil {
		return "", false, fmt.Errorf("context must not be nil")
	}
	values := metadata.ValueFromIncomingContext(ctx, TenantMetadataKey)
	if len(values) == 0 {
		return "", false, nil
	}
	if len(values) != 1 {
		return "", false, fmt.Errorf("%s must appear at most once", TenantMetadataKey)
	}
	raw := strings.TrimSpace(values[0])
	if _, err := uuid.Parse(raw); err != nil {
		return "", false, fmt.Errorf("%s must be a canonical UUID", TenantMetadataKey)
	}
	return raw, true, nil
}

// jobTenantIDContextKey is the unexported context key under which the
// interceptor stores the resolved tenant-id for downstream handlers.
// The value is always a non-empty canonical UUID.
type jobTenantIDContextKey struct{}

// WithJobTenantID attaches a resolved tenant-id to the request context.
// The interceptor is the only intended caller; tests use
// WithJobTenantIDForTest to exercise the read side without an interceptor.
func WithJobTenantID(ctx context.Context, tenantID string) context.Context {
	if ctx == nil {
		return nil
	}
	if _, err := uuid.Parse(tenantID); err != nil {
		return ctx
	}
	return context.WithValue(ctx, jobTenantIDContextKey{}, tenantID)
}

// JobTenantIDFromContext returns the verified tenant-id the interceptor
// attached, or the empty string when the interceptor did not run.
func JobTenantIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(jobTenantIDContextKey{}).(string)
	return value
}
