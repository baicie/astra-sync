package connection

import (
	"crypto/sha256"
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

const MaximumTestPolicyCIDRs = 64

type TestEgressPolicy struct {
	Revision     string   `json:"revision"`
	AllowedCIDRs []string `json:"allowedCidrs"`
}

func NewTestEgressPolicy(allowedCIDRs []string) (TestEgressPolicy, error) {
	if len(allowedCIDRs) > MaximumTestPolicyCIDRs {
		return TestEgressPolicy{}, fmt.Errorf("Connection test egress policy exceeds supported bounds")
	}
	canonical := make([]string, 0, len(allowedCIDRs))
	seen := make(map[string]struct{}, len(allowedCIDRs))
	for _, value := range allowedCIDRs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			return TestEgressPolicy{}, fmt.Errorf("Connection test egress policy contains an invalid CIDR")
		}
		value = prefix.Masked().String()
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		canonical = append(canonical, value)
	}
	sort.Strings(canonical)
	sum := sha256.Sum256([]byte("astra-sync/test-egress-policy/v1\n" + strings.Join(canonical, "\n")))
	return TestEgressPolicy{
		Revision: fmt.Sprintf("sha256:%x", sum), AllowedCIDRs: canonical,
	}, nil
}

func DefaultTestEgressPolicy() TestEgressPolicy {
	policy, err := NewTestEgressPolicy(nil)
	if err != nil {
		panic(err)
	}
	return policy
}

func (p TestEgressPolicy) Validate() error {
	if !revisionPattern.MatchString(p.Revision) || len(p.AllowedCIDRs) > MaximumTestPolicyCIDRs {
		return fmt.Errorf("Connection test egress policy is invalid")
	}
	canonical, err := NewTestEgressPolicy(p.AllowedCIDRs)
	if err != nil || canonical.Revision != p.Revision || len(canonical.AllowedCIDRs) != len(p.AllowedCIDRs) {
		return fmt.Errorf("Connection test egress policy is not canonical")
	}
	for index := range canonical.AllowedCIDRs {
		if canonical.AllowedCIDRs[index] != p.AllowedCIDRs[index] {
			return fmt.Errorf("Connection test egress policy is not canonical")
		}
	}
	return nil
}

func (p TestEgressPolicy) Clone() TestEgressPolicy {
	result := p
	result.AllowedCIDRs = append([]string(nil), p.AllowedCIDRs...)
	return result
}
