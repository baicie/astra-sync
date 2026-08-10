package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"

	"io.astrasync/control-plane/api-server/internal/service"
	"io.astrasync/control-plane/connection"
)

const (
	maximumConnectionTestPolicyBytes   = 64 * 1024
	maximumConnectionTestPolicyTenants = 1024
)

type staticConnectionTestPolicies struct {
	values        map[string]connection.TestEgressPolicy
	defaultPolicy connection.TestEgressPolicy
}

func (p staticConnectionTestPolicies) ResolveConnectionTestPolicy(
	_ context.Context, tenantID string,
) (connection.TestEgressPolicy, error) {
	if policy, found := p.values[tenantID]; found {
		return policy.Clone(), nil
	}
	return p.defaultPolicy.Clone(), nil
}

func newStaticConnectionTestPolicies(
	values map[string]connection.TestEgressPolicy,
) service.ConnectionTestPolicyResolver {
	cloned := make(map[string]connection.TestEgressPolicy, len(values))
	for tenantID, policy := range values {
		cloned[tenantID] = policy.Clone()
	}
	return staticConnectionTestPolicies{
		values: cloned, defaultPolicy: connection.DefaultTestEgressPolicy(),
	}
}

func parseConnectionTestPolicies(value string) (map[string]connection.TestEgressPolicy, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return map[string]connection.TestEgressPolicy{}, nil
	}
	if len(value) > maximumConnectionTestPolicyBytes {
		return nil, fmt.Errorf("CONNECTION_TEST_TENANT_EGRESS_POLICIES exceeds supported bounds")
	}
	decoder := json.NewDecoder(strings.NewReader(value))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("CONNECTION_TEST_TENANT_EGRESS_POLICIES must be a JSON object")
	}
	result := make(map[string]connection.TestEgressPolicy)
	for decoder.More() {
		keyToken, err := decoder.Token()
		tenantID, ok := keyToken.(string)
		if err != nil || !ok {
			return nil, fmt.Errorf("Connection test tenant policy key is invalid")
		}
		if _, err := uuid.Parse(tenantID); err != nil {
			return nil, fmt.Errorf("Connection test tenant policy key must be a UUID")
		}
		if _, duplicate := result[tenantID]; duplicate {
			return nil, fmt.Errorf("Connection test tenant policy contains a duplicate tenant")
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil || len(bytes.TrimSpace(raw)) == 0 ||
			bytes.TrimSpace(raw)[0] != '[' {
			return nil, fmt.Errorf("Connection test tenant policy must be a CIDR array")
		}
		var cidrs []string
		if err := json.Unmarshal(raw, &cidrs); err != nil {
			return nil, fmt.Errorf("Connection test tenant policy must contain CIDR strings")
		}
		policy, err := connection.NewTestEgressPolicy(cidrs)
		if err != nil {
			return nil, err
		}
		result[tenantID] = policy
		if len(result) > maximumConnectionTestPolicyTenants {
			return nil, fmt.Errorf("Connection test tenant policy count exceeds supported bounds")
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, fmt.Errorf("Connection test tenant policy object is incomplete")
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return nil, err
	}
	return result, nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("Connection test tenant policy contains trailing JSON")
	}
	return nil
}
