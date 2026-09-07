// Package normalize_test verifies the label-allowlist contract documented
// in ADR-058 §3. The tests are deliberately boundary-driven: each helper
// returns either the canonical value, the explicit fixed sentinel
// (_unknown / _platform), or refuses to emit caller input as a label.
package normalize_test

import (
	"strings"
	"testing"

	"io.astrasync/control-plane/observability/normalize"
)

// canonicalUUID is a lowercase UUID the catalog accepts as a tenant
// label value. The dashboard recipes bind on this shape.
const canonicalUUID = "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"

func TestNormalizeTenant_should_return_canonical_uuid_when_input_matches(t *testing.T) {
	t.Parallel()
	got := normalize.NormalizeTenant(canonicalUUID)
	if got != canonicalUUID {
		t.Fatalf("NormalizeTenant(%q) = %q, want %q", canonicalUUID, got, canonicalUUID)
	}
}

func TestNormalizeTenant_should_return_platform_when_input_is_platform_scope(t *testing.T) {
	t.Parallel()
	got := normalize.NormalizeTenant("_platform")
	if got != "_platform" {
		t.Fatalf("NormalizeTenant(_platform) = %q, want %q", got, "_platform")
	}
}

func TestNormalizeTenant_should_return_unknown_when_input_is_empty(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "   ", "\t\n"} {
		got := normalize.NormalizeTenant(value)
		if got != "_unknown" {
			t.Fatalf("NormalizeTenant(%q) = %q, want _unknown", value, got)
		}
	}
}

func TestNormalizeTenant_should_return_unknown_when_input_is_not_canonical(t *testing.T) {
	t.Parallel()
	// All of these parse via uuid.Parse but fail the round-trip check.
	// The catalog only accepts the canonical lowercase form; uppercase,
	// braces, and curly braces around a UUID must drop to _unknown.
	cases := []string{
		"0190F7C4-6C8D-7A01-9D2B-1ECABDFF0011",  // uppercase
		"{0190f7c4-6c8d-7a01-9d2b-1ecabdff0011}", // brace form
		"urn:uuid:0190f7c4-6c8d-7a01-9d2b-1ecabdff0011",
		"alice@acme.example",                       // OIDC subject
		"change-me",                                // explicit fixture placeholder
		"plain text",
		"123",
		"0190f7c4-6c8d-7a01-9d2b-1ecabdff0011 ",    // trailing space (TrimSpace covers it; we still expect canonical)
	}
	for _, value := range cases {
		got := normalize.NormalizeTenant(value)
		if strings.HasSuffix(value, " ") {
			// Trailing space trims to canonical UUID, so this case is fine.
			if got != canonicalUUID {
				t.Fatalf("NormalizeTenant(%q) = %q, want canonical UUID", value, got)
			}
			continue
		}
		if got != "_unknown" {
			t.Fatalf("NormalizeTenant(%q) = %q, want _unknown", value, got)
		}
	}
}

func TestNormalizeOutcome_should_return_value_when_in_allowlist(t *testing.T) {
	t.Parallel()
	allowed := []string{"success", "rejected", "failure"}
	for _, value := range allowed {
		got := normalize.NormalizeOutcome(value, allowed, "failure")
		if got != value {
			t.Fatalf("NormalizeOutcome(%q) = %q, want %q", value, got, value)
		}
	}
}

func TestNormalizeOutcome_should_return_fallback_when_outside_allowlist(t *testing.T) {
	t.Parallel()
	allowed := []string{"success", "rejected", "failure"}
	for _, value := range []string{"", "  ", "SUCCESS", "Success", "ok", "denied", "0", "1"} {
		got := normalize.NormalizeOutcome(value, allowed, "failure")
		if got != "failure" {
			t.Fatalf("NormalizeOutcome(%q) = %q, want failure", value, got)
		}
	}
}

func TestNormalizeOutcome_should_return_custom_fallback_when_supplied(t *testing.T) {
	t.Parallel()
	got := normalize.NormalizeOutcome("unknown", []string{"success", "rejected"}, "fenced")
	if got != "fenced" {
		t.Fatalf("NormalizeOutcome should honour caller-supplied fallback; got %q want fenced", got)
	}
}

func TestNormalizeOutcome_should_reject_empty_allowlist(t *testing.T) {
	t.Parallel()
	// An empty allowlist cannot admit any value; the function must
	// always return the fallback. This guard documents that the helper
	// has no implicit "empty means always-allow" semantics.
	got := normalize.NormalizeOutcome("success", nil, "failure")
	if got != "failure" {
		t.Fatalf("NormalizeOutcome with nil allowlist should return fallback; got %q", got)
	}
}

func TestNormalizeWorkerID_should_return_trimmed_value_when_within_length(t *testing.T) {
	t.Parallel()
	got := normalize.NormalizeWorkerID("  worker-7  ")
	if got != "worker-7" {
		t.Fatalf("NormalizeWorkerID should trim leading/trailing whitespace; got %q", got)
	}
}

func TestNormalizeWorkerID_should_return_unknown_when_empty(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "   ", "\t"} {
		got := normalize.NormalizeWorkerID(value)
		if got != "_unknown" {
			t.Fatalf("NormalizeWorkerID(%q) = %q, want _unknown", value, got)
		}
	}
}

func TestNormalizeWorkerID_should_return_unknown_when_exceeds_max_length(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", 129)
	got := normalize.NormalizeWorkerID(long)
	if got != "_unknown" {
		t.Fatalf("NormalizeWorkerID(%d-byte input) = %q, want _unknown", len(long), got)
	}
	// Boundary check: exactly 128 bytes must be accepted.
	boundary := strings.Repeat("b", 128)
	got = normalize.NormalizeWorkerID(boundary)
	if got != boundary {
		t.Fatalf("NormalizeWorkerID at 128-byte boundary should pass through; got length %d", len(got))
	}
}

func TestNormalizeWorkerID_should_not_truncate_to_avoid_collisions(t *testing.T) {
	t.Parallel()
	// Two distinct runaway callers must not collide on a truncated label.
	a := strings.Repeat("a", 200)
	b := strings.Repeat("b", 200)
	ga := normalize.NormalizeWorkerID(a)
	gb := normalize.NormalizeWorkerID(b)
	if ga != "_unknown" || gb != "_unknown" {
		t.Fatalf("NormalizeWorkerID should reject oversize inputs, got %q vs %q", ga, gb)
	}
}
