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
		"0190F7C4-6C8D-7A01-9D2B-1ECABDFF0011",   // uppercase
		"{0190f7c4-6c8d-7a01-9d2b-1ecabdff0011}", // brace form
		"urn:uuid:0190f7c4-6c8d-7a01-9d2b-1ecabdff0011",
		"alice@acme.example", // OIDC subject
		"change-me",          // explicit fixture placeholder
		"plain text",
		"123",
		"0190f7c4-6c8d-7a01-9d2b-1ecabdff0011 ", // trailing space (TrimSpace covers it; we still expect canonical)
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

// NormalizeFreeText tests (Phase 21 slice 46.1).

func TestNormalizeFreeText_should_return_trimmed_value_when_valid(t *testing.T) {
	t.Parallel()
	// Whitespace-only input is trimmed to empty, which is rejected.
	// Valid non-empty input with surrounding space is trimmed and returned.
	got := normalize.NormalizeFreeText("  us-east-1  ", 128, "_unknown")
	if got != "us-east-1" {
		t.Fatalf("NormalizeFreeText(%q) = %q, want %q", "  us-east-1  ", got, "us-east-1")
	}
}

func TestNormalizeFreeText_should_return_unknown_when_input_is_empty(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "   ", "\t", " \t\n"} {
		got := normalize.NormalizeFreeText(value, 128, "_unknown")
		if got != "_unknown" {
			t.Fatalf("NormalizeFreeText(%q) = %q, want _unknown", value, got)
		}
	}
}

func TestNormalizeFreeText_should_return_unknown_when_input_contains_non_printable_characters(t *testing.T) {
	// Prometheus/OpenMetrics label values must be printable. Any Unicode
	// IsControl rune (C0/C1 categories) must reject the value so that
	// downstream scrapers do not receive malformed series. The control
	// rune is placed in the MIDDLE of the string because strings.TrimSpace
	// strips leading/trailing C0 control characters before this check runs
	// (mirrors the existing NormalizeWorkerID contract). The "trim-only"
	// cases verify that a value consisting solely of trimmed controls
	// collapses to _unknown via the empty-after-trim branch.
	nonPrintableInputs := []struct {
		name     string
		value    string
		expectOk bool // true means we accept the value (not _unknown)
	}{
		{name: "embedded_newline", value: "us-east-\n1"},
		{name: "embedded_carriage_return", value: "us-east-\r1"},
		{name: "embedded_tab", value: "us-east-\t1"},
		{name: "embedded_vertical_tab", value: "us-east-\v1"},
		{name: "embedded_form_feed", value: "us-east-\f1"},
		{name: "embedded_nul_byte", value: "us-east-\x001"},
		{name: "embedded_bell", value: "us-east-\x071"},
		{name: "embedded_mid_newline", value: "us\n-east-1"},
		// Pure-control inputs trim to empty -> _unknown via the earlier
		// empty-after-trim branch. Both are valid _unknown results.
		{name: "pure_newline", value: "\n", expectOk: true},
		{name: "pure_tab", value: "\t", expectOk: true},
	}
	for _, tc := range nonPrintableInputs {
		t.Run(tc.name, func(t *testing.T) {
			got := normalize.NormalizeFreeText(tc.value, 128, "_unknown")
			if !tc.expectOk {
				if got != "_unknown" {
					t.Fatalf("NormalizeFreeText(%q) = %q, want _unknown", tc.value, got)
				}
			} else {
				// Pure-control values collapse via the empty-after-trim
				// branch to _unknown (we document this is _unknown, not
				// because of the IsControl check).
				if got != "_unknown" {
					t.Fatalf("NormalizeFreeText(%q) = %q, want _unknown via trim-collapse", tc.value, got)
				}
			}
		})
	}
}

func TestNormalizeFreeText_should_return_unknown_when_exceeds_max_bytes(t *testing.T) {
	// Over-length input must reject to unknown, not truncate, so that
	// two distinct long inputs cannot collide on the same truncated label.
	overLen := strings.Repeat("a", 129)
	got := normalize.NormalizeFreeText(overLen, 128, "_unknown")
	if got != "_unknown" {
		t.Fatalf("NormalizeFreeText(%d-byte input) = %q, want _unknown", len(overLen), got)
	}
	// Boundary: exactly maxBytes must be accepted.
	atBoundary := strings.Repeat("b", 128)
	got = normalize.NormalizeFreeText(atBoundary, 128, "_unknown")
	if got != atBoundary {
		t.Fatalf("NormalizeFreeText at 128-byte boundary should pass through; got %q", got)
	}
	// Two distinct over-length inputs must both reject to _unknown (no collision).
	overLenA := strings.Repeat("a", 500)
	overLenB := strings.Repeat("b", 500)
	gotA := normalize.NormalizeFreeText(overLenA, 128, "_unknown")
	gotB := normalize.NormalizeFreeText(overLenB, 128, "_unknown")
	if gotA != "_unknown" || gotB != "_unknown" {
		t.Fatalf("NormalizeFreeText should reject oversize inputs, got %q vs %q", gotA, gotB)
	}
}

func TestNormalizeFreeText_should_preserve_case(t *testing.T) {
	t.Parallel()
	// Free-form text is case-sensitive. NormalizeFreeText does NOT lowercase.
	got := normalize.NormalizeFreeText("US-EAST-1", 128, "_unknown")
	if got != "US-EAST-1" {
		t.Fatalf("NormalizeFreeText should preserve case, got %q", got)
	}
	got = normalize.NormalizeFreeText("us-east-1", 128, "_unknown")
	if got != "us-east-1" {
		t.Fatalf("NormalizeFreeText should preserve case, got %q", got)
	}
}

func TestNormalizeFreeText_should_preserve_internal_spaces(t *testing.T) {
	t.Parallel()
	// Internal spaces are intentional in some region name formats.
	// NormalizeFreeText trims but does not collapse internal whitespace.
	got := normalize.NormalizeFreeText("us east 1", 128, "_unknown")
	if got != "us east 1" {
		t.Fatalf("NormalizeFreeText should preserve internal spaces, got %q", got)
	}
}

func TestNormalizeFreeText_should_honor_caller_supplied_unknown(t *testing.T) {
	t.Parallel()
	// The unknown sentinel is caller-supplied so that different Recorder
	// owners can use different sentinels if their catalog row requires it.
	got := normalize.NormalizeFreeText("", 128, "_fallback")
	if got != "_fallback" {
		t.Fatalf("NormalizeFreeText should honour caller-supplied unknown; got %q", got)
	}
}

func TestNormalizeFreeText_should_panic_when_max_bytes_is_non_positive(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NormalizeFreeText with maxBytes=0 did not panic")
		}
	}()
	normalize.NormalizeFreeText("any-value", 0, "_unknown")
}

func TestNormalizeFreeText_should_panic_when_unknown_is_empty(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NormalizeFreeText with unknown='' did not panic")
		}
	}()
	normalize.NormalizeFreeText("any-value", 128, "")
}
