package connectiontest

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"io.astrasync/control-plane/connection"
)

type Configuration struct {
	values map[string][]byte
}

func NewConfiguration(
	settings []connection.Setting, credentialFields map[string][]byte,
) (*Configuration, error) {
	values := make(map[string][]byte, len(settings)+len(credentialFields))
	closeValues := func() {
		for key, value := range values {
			clear(value)
			delete(values, key)
		}
	}
	for _, setting := range settings {
		if _, duplicate := values[setting.Key]; duplicate {
			closeValues()
			return nil, fmt.Errorf("Connection test configuration contains duplicate keys")
		}
		value := []byte(setting.Value)
		if len(value) > connection.MaximumSettingValue || !utf8.Valid(value) || strings.ContainsRune(setting.Key, '\x00') {
			clear(value)
			closeValues()
			return nil, fmt.Errorf("Connection test configuration is invalid")
		}
		values[setting.Key] = value
	}
	for key, source := range credentialFields {
		if _, duplicate := values[key]; duplicate || len(source) == 0 ||
			len(source) > connection.MaximumSettingValue || !utf8.Valid(source) || strings.ContainsRune(key, '\x00') {
			closeValues()
			return nil, fmt.Errorf("Connection test credential configuration is invalid")
		}
		values[key] = append([]byte(nil), source...)
	}
	return &Configuration{values: values}, nil
}

func (c *Configuration) value(key string) (string, bool) {
	if c == nil || c.values == nil {
		return "", false
	}
	value, found := c.values[key]
	return string(value), found
}

func (c *Configuration) required(key string) (string, bool) {
	value, found := c.value(key)
	return value, found && strings.TrimSpace(value) != ""
}

func (c *Configuration) Close() error {
	if c == nil {
		return nil
	}
	for key, value := range c.values {
		clear(value)
		delete(c.values, key)
	}
	c.values = nil
	return nil
}

type ProbeResult struct {
	State          connection.TestState
	Phase          connection.TestPhase
	ResultCode     connection.TestResultCode
	Success        bool
	RemediationKey string
}

func SuccessfulProbe() ProbeResult {
	return ProbeResult{
		State: connection.TestSucceeded, Phase: connection.TestPhaseComplete,
		ResultCode: connection.TestResultOK, Success: true,
	}
}

func FailedProbe(
	phase connection.TestPhase, code connection.TestResultCode, remediationKey string,
) ProbeResult {
	return ProbeResult{
		State: connection.TestFailed, Phase: phase, ResultCode: code,
		RemediationKey: remediationKey,
	}
}

func TimedOutProbe(phase connection.TestPhase) ProbeResult {
	return ProbeResult{
		State: connection.TestTimedOut, Phase: phase,
		ResultCode:     connection.TestResultDeadlineExceeded,
		RemediationKey: "connection.test.deadline",
	}
}

func CanceledProbe(phase connection.TestPhase) ProbeResult {
	return ProbeResult{
		State: connection.TestCanceled, Phase: phase,
		ResultCode:     connection.TestResultExecutorUnavailable,
		RemediationKey: "connection.test.canceled",
	}
}

func (r ProbeResult) Completion() (connection.TestCompletion, error) {
	completion := connection.TestCompletion{
		State: r.State, Phase: r.Phase, ResultCode: r.ResultCode,
		Success: r.Success, RemediationKey: r.RemediationKey,
	}
	return completion, completion.Validate()
}

type Probe interface {
	Execute(context.Context, *Configuration, *EgressGuard, connection.TestEgressPolicy) ProbeResult
}

type Registry struct {
	probes map[string]Probe
}

func NewRegistry(probes map[string]Probe) (*Registry, error) {
	if len(probes) == 0 {
		return nil, fmt.Errorf("Connection test probe registry must not be empty")
	}
	result := &Registry{probes: make(map[string]Probe, len(probes))}
	for connector, probe := range probes {
		if strings.TrimSpace(connector) == "" || probe == nil {
			return nil, fmt.Errorf("Connection test probe registry entry is invalid")
		}
		result.probes[connector] = probe
	}
	return result, nil
}

func DefaultRegistry() (*Registry, error) {
	return NewRegistry(map[string]Probe{
		"jdbc":         JDBCProbe{kind: jdbcGeneric},
		"mysql-cdc":    JDBCProbe{kind: jdbcMySQL},
		"postgres-cdc": JDBCProbe{kind: jdbcPostgreSQL},
	})
}

func (r *Registry) Execute(
	ctx context.Context,
	connectorName string,
	configuration *Configuration,
	guard *EgressGuard,
	policy connection.TestEgressPolicy,
) ProbeResult {
	if r == nil || configuration == nil || guard == nil {
		return FailedProbe(
			connection.TestPhaseHandshake, connection.TestResultExecutorUnavailable,
			"connection.test.executor",
		)
	}
	probe := r.probes[connectorName]
	if probe == nil {
		return FailedProbe(
			connection.TestPhaseHandshake, connection.TestResultHandshakeFailed,
			"connection.test.unsupported_connector",
		)
	}
	result := probe.Execute(ctx, configuration, guard, policy)
	if _, err := result.Completion(); err != nil {
		return FailedProbe(
			connection.TestPhaseHandshake, connection.TestResultExecutorUnavailable,
			"connection.test.executor",
		)
	}
	return result
}
