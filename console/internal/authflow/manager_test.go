package authflow

import (
	"strings"
	"testing"
)

func TestSafeReturnPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "root", value: "/", want: "/"},
		{name: "path", value: "/connections/orders-db", want: "/connections/orders-db"},
		{name: "query and fragment", value: "/jobs?page=2#orders", want: "/jobs?page=2#orders"},
		{name: "empty", value: "", want: "/"},
		{name: "relative", value: "connections", want: "/"},
		{name: "absolute URL", value: "https://evil.example/capture", want: "/"},
		{name: "network path", value: "//evil.example/capture", want: "/"},
		{name: "triple slash", value: "///evil.example/capture", want: "/"},
		{name: "backslash authority", value: `/\evil.example/capture`, want: "/"},
		{name: "encoded backslash", value: "/%5Cevil.example/capture", want: "/"},
		{name: "encoded authority", value: "/%2F%2Fevil.example/capture", want: "/"},
		{name: "newline", value: "/jobs\r\nLocation: https://evil.example", want: "/"},
		{name: "encoded newline", value: "/jobs%0ALocation:evil", want: "/"},
		{name: "malformed escape", value: "/jobs%2", want: "/"},
		{name: "too long", value: "/" + strings.Repeat("a", 2048), want: "/"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := SafeReturnPath(test.value); got != test.want {
				t.Fatalf("SafeReturnPath(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}
