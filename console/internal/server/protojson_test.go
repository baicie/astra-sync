package server_test

import (
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	jobv1 "io.astrasync/control-plane/api-server/gen/go/v1"
)

// TestProtoJSONJobSpecParsing confirms the exact JSON shape that decodeJobSpec
// (job_handlers.go) accepts via protojson.Unmarshal. These tests verify
// that protojson can parse the minimal spec shape; validation (nil source,
// nil sink, nil delivery) is exercised in the api-server unit tests, not
// in the BFF black-box layer.
func TestProtoJSONJobSpecParsing(t *testing.T) {
	for _, tc := range []struct {
		name    string
		rawJSON string
		wantErr string
	}{
		{
			name:    "csv_minimal_at_least_once",
			rawJSON: `{"source":{"connector":"csv","options":{"path":"in.csv"}},"sink":{"connector":"csv","options":{"path":"out.csv"}},"delivery":{"guarantee":"DELIVERY_GUARANTEE_AT_LEAST_ONCE"}}`,
		},
		{
			name:    "csv_at_most_once",
			rawJSON: `{"source":{"connector":"csv","options":{"path":"in.csv"}},"sink":{"connector":"csv","options":{"path":"out.csv"}},"delivery":{"guarantee":"DELIVERY_GUARANTEE_AT_MOST_ONCE"}}`,
		},
		{
			name:    "csv_exactly_once",
			rawJSON: `{"source":{"connector":"csv","options":{"path":"in.csv"}},"sink":{"connector":"csv","options":{"path":"out.csv"}},"delivery":{"guarantee":"DELIVERY_GUARANTEE_EXACTLY_ONCE"}}`,
		},
		{
			name:    "no_options",
			rawJSON: `{"source":{"connector":"csv"},"sink":{"connector":"csv"},"delivery":{"guarantee":"DELIVERY_GUARANTEE_AT_MOST_ONCE"}}`,
		},
		{
			name:    "invalid_enum_value",
			rawJSON: `{"source":{"connector":"csv"},"sink":{"connector":"csv"},"delivery":{"guarantee":"AT_LEAST_ONCE"}}`,
			wantErr: "invalid value for enum",
		},
		{
			name:    "invalid_connector_name",
			rawJSON: `{"source":{"connector":"CSV"},"sink":{"connector":"csv"},"delivery":{"guarantee":"DELIVERY_GUARANTEE_AT_MOST_ONCE"}}`,
			// protojson accepts the JSON; the connector name validation is a
			// domain-layer check in fromProtoSpec, not a parse error.
			wantErr: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := &jobv1.JobSpec{}
			err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal([]byte(tc.rawJSON), spec)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			if spec.GetSource() == nil || spec.GetSink() == nil || spec.GetDelivery() == nil {
				t.Fatalf("spec not fully parsed: source=%v sink=%v delivery=%v",
					spec.GetSource(), spec.GetSink(), spec.GetDelivery())
			}
		})
	}
}
