package connection_test

import (
	"testing"

	"io.astrasync/control-plane/connection"
)

func TestTestEgressPolicyIsCanonicalAndContentAddressed(t *testing.T) {
	t.Parallel()
	policy, err := connection.NewTestEgressPolicy([]string{
		"10.2.1.7/16", "2001:db8:1::9/48", "10.2.0.0/16",
	})
	if err != nil {
		t.Fatalf("construct policy: %v", err)
	}
	if err := policy.Validate(); err != nil {
		t.Fatalf("validate policy: %v", err)
	}
	if len(policy.AllowedCIDRs) != 2 || policy.AllowedCIDRs[0] != "10.2.0.0/16" ||
		policy.AllowedCIDRs[1] != "2001:db8:1::/48" {
		t.Fatalf("policy is not canonical: %+v", policy)
	}
	changed, err := connection.NewTestEgressPolicy([]string{"10.3.0.0/16"})
	if err != nil || changed.Revision == policy.Revision {
		t.Fatalf("policy revision is not content addressed: changed=%+v err=%v", changed, err)
	}
	tampered := policy.Clone()
	tampered.AllowedCIDRs[0] = "10.4.0.0/16"
	if err := tampered.Validate(); err == nil {
		t.Fatal("expected tampered policy revision to fail validation")
	}
}
