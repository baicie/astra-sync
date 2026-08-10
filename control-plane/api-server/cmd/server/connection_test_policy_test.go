package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

func TestConnectionTestTenantPoliciesAreStrictAndTenantScoped(t *testing.T) {
	t.Parallel()
	tenantID := uuid.NewString()
	policies, err := parseConnectionTestPolicies(fmt.Sprintf(
		`{"%s":["10.2.1.1/16","10.2.0.0/16"]}`, tenantID,
	))
	if err != nil {
		t.Fatalf("parse tenant policies: %v", err)
	}
	resolver := newStaticConnectionTestPolicies(policies)
	policy, err := resolver.ResolveConnectionTestPolicy(context.Background(), tenantID)
	if err != nil || len(policy.AllowedCIDRs) != 1 || policy.AllowedCIDRs[0] != "10.2.0.0/16" {
		t.Fatalf("unexpected tenant policy: policy=%+v err=%v", policy, err)
	}
	defaultPolicy, err := resolver.ResolveConnectionTestPolicy(context.Background(), uuid.NewString())
	if err != nil || len(defaultPolicy.AllowedCIDRs) != 0 {
		t.Fatalf("unknown tenant did not fail closed: policy=%+v err=%v", defaultPolicy, err)
	}
}

func TestConnectionTestTenantPoliciesRejectMalformedAndDuplicateInput(t *testing.T) {
	t.Parallel()
	tenantID := uuid.NewString()
	for _, value := range []string{
		`[]`,
		`{"not-a-uuid":["10.0.0.0/8"]}`,
		fmt.Sprintf(`{"%s":"10.0.0.0/8"}`, tenantID),
		fmt.Sprintf(`{"%s":["invalid"]}`, tenantID),
		fmt.Sprintf(`{"%s":[],"%s":["10.0.0.0/8"]}`, tenantID, tenantID),
		fmt.Sprintf(`{"%s":[]} true`, tenantID),
	} {
		if _, err := parseConnectionTestPolicies(value); err == nil {
			t.Fatalf("expected policy input to be rejected: %s", value)
		}
	}
}
