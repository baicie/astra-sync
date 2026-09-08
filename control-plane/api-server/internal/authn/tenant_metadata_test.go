// Package authn_test exercises the x-astra-tenant-id metadata extractor
// added in Phase 29 (ADR-074 §2). The extractor is a pure function with
// no principal state — the metadata-vs-membership reconciliation is
// covered by the interceptor tests in interceptor_test.go.
package authn_test

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc/metadata"

	"io.astrasync/control-plane/api-server/internal/authn"
)

const phase29TenantUUID = "8d58d674-7cc7-4b15-a46c-9e7768bbf103"

// TestJobTenantIDFromIncomingMetadataAbsent covers the absence path: a
// request without `x-astra-tenant-id` returns (_, false, nil). The
// interceptor uses the `(false, nil)` outcome to fall through to the
// membership-derived value.
func TestJobTenantIDFromIncomingMetadataAbsent(t *testing.T) {
	value, present, err := authn.JobTenantIDFromIncomingMetadata(context.Background())
	if err != nil {
		t.Fatalf("absent metadata: unexpected error %v", err)
	}
	if present || value != "" {
		t.Fatalf("absent metadata: got value=%q present=%v, want empty", value, present)
	}
}

// TestJobTenantIDFromIncomingMetadataCanonical covers the happy path.
func TestJobTenantIDFromIncomingMetadataCanonical(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		authn.TenantMetadataKey, phase29TenantUUID,
	))
	value, present, err := authn.JobTenantIDFromIncomingMetadata(ctx)
	if err != nil {
		t.Fatalf("canonical metadata: %v", err)
	}
	if !present || value != phase29TenantUUID {
		t.Fatalf("canonical metadata: got value=%q present=%v, want %q", value, present, phase29TenantUUID)
	}
}

// TestJobTenantIDFromIncomingMetadataRejectsDuplicate ensures the
// extractor refuses multi-value headers. gRPC `metadata.Pairs` would
// collapse duplicate keys into a multi-value slice.
func TestJobTenantIDFromIncomingMetadataRejectsDuplicate(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.MD{
		authn.TenantMetadataKey: {phase29TenantUUID, "22222222-2222-4222-8222-222222222222"},
	})
	_, _, err := authn.JobTenantIDFromIncomingMetadata(ctx)
	if err == nil {
		t.Fatalf("duplicate metadata must be rejected, got nil")
	}
	if !strings.Contains(err.Error(), "at most once") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestJobTenantIDFromIncomingMetadataRejectsNonUUID ensures the extractor
// rejects non-canonical values. ADR-074 §2 requires a canonical UUID.
func TestJobTenantIDFromIncomingMetadataRejectsNonUUID(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		authn.TenantMetadataKey, "not-a-uuid",
	))
	_, _, err := authn.JobTenantIDFromIncomingMetadata(ctx)
	if err == nil {
		t.Fatalf("non-canonical metadata must be rejected, got nil")
	}
	if !strings.Contains(err.Error(), "canonical UUID") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestJobTenantIDContextRoundTrip ensures the context-bound variant
// returns the value the interceptor would have stored.
func TestJobTenantIDContextRoundTrip(t *testing.T) {
	if got := authn.JobTenantIDFromContext(context.Background()); got != "" {
		t.Fatalf("empty context must yield empty tenant ID, got %q", got)
	}
	ctx := authn.WithJobTenantID(context.Background(), phase29TenantUUID)
	if got := authn.JobTenantIDFromContext(ctx); got != phase29TenantUUID {
		t.Fatalf("context round-trip: got %q want %q", got, phase29TenantUUID)
	}
}