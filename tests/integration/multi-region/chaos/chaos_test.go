// Package chaos provides chaos tests for multi-region failure scenarios.
package chaos

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// FailureType defines the type of failure.
type FailureType int

const (
	FailureNone FailureType = iota
	FailureNetworkPartition
	FailurePodKill
	FailureRegionUnreachable
	FailureHighLatency
	FailurePacketLoss
)

// String returns the string representation of the failure type.
func (f FailureType) String() string {
	switch f {
	case FailureNone:
		return "none"
	case FailureNetworkPartition:
		return "network_partition"
	case FailurePodKill:
		return "pod_kill"
	case FailureRegionUnreachable:
		return "region_unreachable"
	case FailureHighLatency:
		return "high_latency"
	case FailurePacketLoss:
		return "packet_loss"
	default:
		return "unknown"
	}
}

// ChaosScenario defines a chaos test scenario.
type ChaosScenario struct {
	Name        string
	Failures    []FailureType
	Duration    time.Duration
	Recovery    time.Duration
	ExpectRecover bool
}

// ChaosState represents the current state of the chaos test.
type ChaosState int

const (
	StateHealthy ChaosState = iota
	StateFailureInjected
	StateRecovering
	StateRecovered
)

// String returns the string representation of the state.
func (s ChaosState) String() string {
	switch s {
	case StateHealthy:
		return "healthy"
	case StateFailureInjected:
		return "failure_injected"
	case StateRecovering:
		return "recovering"
	case StateRecovered:
		return "recovered"
	default:
		return "unknown"
	}
}

// ChaosEvent represents an event in the chaos test.
type ChaosEvent struct {
	Timestamp time.Time
	State     ChaosState
	Failure   FailureType
	Message   string
}

// ChaosEngine simulates chaos failures.
type ChaosEngine struct {
	mu     sync.RWMutex
	state  ChaosState
	events []ChaosEvent
}

// NewChaosEngine creates a new chaos engine.
func NewChaosEngine() *ChaosEngine {
	return &ChaosEngine{
		state:  StateHealthy,
		events: make([]ChaosEvent, 0),
	}
}

// GetState returns the current state.
func (e *ChaosEngine) GetState() ChaosState {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.state
}

// GetEvents returns the recorded events.
func (e *ChaosEngine) GetEvents() []ChaosEvent {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return append([]ChaosEvent(nil), e.events...)
}

// InjectFailure injects a failure.
func (e *ChaosEngine) InjectFailure(failure FailureType) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.state = StateFailureInjected
	e.events = append(e.events, ChaosEvent{
		Timestamp: time.Now().UTC(),
		State:     StateFailureInjected,
		Failure:   failure,
		Message:   "failure injected",
	})

	return nil
}

// Recover transitions to recovering state.
func (e *ChaosEngine) Recover() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.state != StateFailureInjected {
		return errors.New("not in failure state")
	}

	e.state = StateRecovering
	e.events = append(e.events, ChaosEvent{
		Timestamp: time.Now().UTC(),
		State:     StateRecovering,
		Failure:   FailureNone,
		Message:   "recovery started",
	})

	return nil
}

// CompleteRecovery marks recovery as complete.
func (e *ChaosEngine) CompleteRecovery() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.state = StateRecovered
	e.events = append(e.events, ChaosEvent{
		Timestamp: time.Now().UTC(),
		State:     StateRecovered,
		Failure:   FailureNone,
		Message:   "recovery complete",
	})

	return nil
}

// Reset resets the chaos engine.
func (e *ChaosEngine) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.state = StateHealthy
	e.events = e.events[:0]
}

// RunScenario runs a chaos scenario.
func (e *ChaosEngine) RunScenario(ctx context.Context, scenario ChaosScenario) error {
	// Inject failures
	for _, failure := range scenario.Failures {
		if err := e.InjectFailure(failure); err != nil {
			return err
		}
	}

	// Wait for failure duration
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(scenario.Duration):
	}

	// Start recovery
	if scenario.ExpectRecover {
		if err := e.Recover(); err != nil {
			return err
		}

		// Wait for recovery duration
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(scenario.Recovery):
		}

		if err := e.CompleteRecovery(); err != nil {
			return err
		}
	}

	return nil
}

// TestChaos_NetworkPartition verifies network partition handling.
func TestChaos_NetworkPartition(t *testing.T) {
	engine := NewChaosEngine()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	scenario := ChaosScenario{
		Name:        "network_partition",
		Failures:    []FailureType{FailureNetworkPartition},
		Duration:    100 * time.Millisecond,
		Recovery:    100 * time.Millisecond,
		ExpectRecover: true,
	}

	if err := engine.RunScenario(ctx, scenario); err != nil {
		t.Fatalf("chaos scenario failed: %v", err)
	}

	if engine.GetState() != StateRecovered {
		t.Errorf("expected state recovered, got %s", engine.GetState())
	}
}

// TestChaos_PodFailure verifies pod failure handling.
func TestChaos_PodFailure(t *testing.T) {
	engine := NewChaosEngine()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	scenario := ChaosScenario{
		Name:        "pod_failure",
		Failures:    []FailureType{FailurePodKill},
		Duration:    100 * time.Millisecond,
		Recovery:    100 * time.Millisecond,
		ExpectRecover: true,
	}

	if err := engine.RunScenario(ctx, scenario); err != nil {
		t.Fatalf("chaos scenario failed: %v", err)
	}

	if engine.GetState() != StateRecovered {
		t.Errorf("expected state recovered, got %s", engine.GetState())
	}
}

// TestChaos_RegionUnreachable verifies region unreachable handling.
func TestChaos_RegionUnreachable(t *testing.T) {
	engine := NewChaosEngine()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	scenario := ChaosScenario{
		Name:        "region_unreachable",
		Failures:    []FailureType{FailureRegionUnreachable},
		Duration:    100 * time.Millisecond,
		Recovery:    200 * time.Millisecond,
		ExpectRecover: true,
	}

	if err := engine.RunScenario(ctx, scenario); err != nil {
		t.Fatalf("chaos scenario failed: %v", err)
	}

	if engine.GetState() != StateRecovered {
		t.Errorf("expected state recovered, got %s", engine.GetState())
	}
}

// TestChaos_MultipleFailures verifies multiple failures.
func TestChaos_MultipleFailures(t *testing.T) {
	engine := NewChaosEngine()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	scenario := ChaosScenario{
		Name:        "multiple_failures",
		Failures:    []FailureType{FailureNetworkPartition, FailurePodKill},
		Duration:    100 * time.Millisecond,
		Recovery:    100 * time.Millisecond,
		ExpectRecover: true,
	}

	if err := engine.RunScenario(ctx, scenario); err != nil {
		t.Fatalf("chaos scenario failed: %v", err)
	}

	if engine.GetState() != StateRecovered {
		t.Errorf("expected state recovered, got %s", engine.GetState())
	}
}

// TestChaos_NoRecovery verifies scenario without recovery.
func TestChaos_NoRecovery(t *testing.T) {
	engine := NewChaosEngine()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	scenario := ChaosScenario{
		Name:        "no_recovery",
		Failures:    []FailureType{FailureNetworkPartition},
		Duration:    100 * time.Millisecond,
		ExpectRecover: false,
	}

	if err := engine.RunScenario(ctx, scenario); err != nil {
		t.Fatalf("chaos scenario failed: %v", err)
	}

	if engine.GetState() != StateFailureInjected {
		t.Errorf("expected state failure_injected, got %s", engine.GetState())
	}
}

// TestChaos_Reset verifies reset.
func TestChaos_Reset(t *testing.T) {
	engine := NewChaosEngine()

	engine.InjectFailure(FailureNetworkPartition)
	engine.Reset()

	if engine.GetState() != StateHealthy {
		t.Errorf("expected state healthy, got %s", engine.GetState())
	}

	if len(engine.GetEvents()) != 0 {
		t.Errorf("expected 0 events after reset, got %d", len(engine.GetEvents()))
	}
}

// TestChaos_InitialState verifies initial state.
func TestChaos_InitialState(t *testing.T) {
	engine := NewChaosEngine()

	if engine.GetState() != StateHealthy {
		t.Errorf("expected initial state healthy, got %s", engine.GetState())
	}
}

// TestChaos_RecoverWithoutFailure verifies recover without failure.
func TestChaos_RecoverWithoutFailure(t *testing.T) {
	engine := NewChaosEngine()

	err := engine.Recover()
	if err == nil {
		t.Error("expected error when recovering without failure")
	}
}

// TestChaos_EventsRecorded verifies events are recorded.
func TestChaos_EventsRecorded(t *testing.T) {
	engine := NewChaosEngine()

	engine.InjectFailure(FailureNetworkPartition)
	engine.Recover()
	engine.CompleteRecovery()

	events := engine.GetEvents()
	if len(events) != 3 {
		t.Errorf("expected 3 events, got %d", len(events))
	}
}

// TestChaos_FailureType verifies failure type string.
func TestChaos_FailureType(t *testing.T) {
	if FailureNetworkPartition.String() != "network_partition" {
		t.Errorf("expected network_partition, got %s", FailureNetworkPartition.String())
	}
	if FailurePodKill.String() != "pod_kill" {
		t.Errorf("expected pod_kill, got %s", FailurePodKill.String())
	}
}

// TestChaos_ChaosState verifies chaos state string.
func TestChaos_ChaosState(t *testing.T) {
	if StateHealthy.String() != "healthy" {
		t.Errorf("expected healthy, got %s", StateHealthy.String())
	}
	if StateRecovered.String() != "recovered" {
		t.Errorf("expected recovered, got %s", StateRecovered.String())
	}
}

// TestChaos_HighLatency verifies high latency handling.
func TestChaos_HighLatency(t *testing.T) {
	engine := NewChaosEngine()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	scenario := ChaosScenario{
		Name:        "high_latency",
		Failures:    []FailureType{FailureHighLatency},
		Duration:    100 * time.Millisecond,
		Recovery:    100 * time.Millisecond,
		ExpectRecover: true,
	}

	if err := engine.RunScenario(ctx, scenario); err != nil {
		t.Fatalf("chaos scenario failed: %v", err)
	}

	if engine.GetState() != StateRecovered {
		t.Errorf("expected state recovered, got %s", engine.GetState())
	}
}

// TestChaos_PacketLoss verifies packet loss handling.
func TestChaos_PacketLoss(t *testing.T) {
	engine := NewChaosEngine()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	scenario := ChaosScenario{
		Name:        "packet_loss",
		Failures:    []FailureType{FailurePacketLoss},
		Duration:    100 * time.Millisecond,
		Recovery:    100 * time.Millisecond,
		ExpectRecover: true,
	}

	if err := engine.RunScenario(ctx, scenario); err != nil {
		t.Fatalf("chaos scenario failed: %v", err)
	}

	if engine.GetState() != StateRecovered {
		t.Errorf("expected state recovered, got %s", engine.GetState())
	}
}

// TestChaos_ContextCancellation verifies context cancellation.
func TestChaos_ContextCancellation(t *testing.T) {
	engine := NewChaosEngine()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	scenario := ChaosScenario{
		Name:        "cancellation",
		Failures:    []FailureType{FailureNetworkPartition},
		Duration:    1 * time.Second,
	}

	err := engine.RunScenario(ctx, scenario)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

// TestChaos_AllFailures verifies all failure types.
func TestChaos_AllFailures(t *testing.T) {
	failures := []FailureType{
		FailureNetworkPartition,
		FailurePodKill,
		FailureRegionUnreachable,
		FailureHighLatency,
		FailurePacketLoss,
	}

	for _, failure := range failures {
		t.Run(failure.String(), func(t *testing.T) {
			engine := NewChaosEngine()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			scenario := ChaosScenario{
				Failures:    []FailureType{failure},
				Duration:    100 * time.Millisecond,
				Recovery:    100 * time.Millisecond,
				ExpectRecover: true,
			}

			if err := engine.RunScenario(ctx, scenario); err != nil {
				t.Errorf("chaos scenario failed: %v", err)
			}

			if engine.GetState() != StateRecovered {
				t.Errorf("expected state recovered, got %s", engine.GetState())
			}
		})
	}
}

// TestChaos_AllStates verifies all states.
func TestChaos_AllStates(t *testing.T) {
	states := []ChaosState{
		StateHealthy,
		StateFailureInjected,
		StateRecovering,
		StateRecovered,
	}

	for _, state := range states {
		if state.String() == "" {
			t.Errorf("state %d has empty string", state)
		}
	}
}

// TestChaos_ChaosEvent verifies event struct.
func TestChaos_ChaosEvent(t *testing.T) {
	event := ChaosEvent{
		Timestamp: time.Now().UTC(),
		State:     StateFailureInjected,
		Failure:   FailureNetworkPartition,
		Message:   "test event",
	}

	if event.State != StateFailureInjected {
		t.Error("expected state failure_injected")
	}
	if event.Failure != FailureNetworkPartition {
		t.Error("expected failure network_partition")
	}
}

// TestChaos_ScenarioStruct verifies scenario struct.
func TestChaos_ScenarioStruct(t *testing.T) {
	scenario := ChaosScenario{
		Name:        "test",
		Failures:    []FailureType{FailureNetworkPartition},
		Duration:    1 * time.Second,
		Recovery:    1 * time.Second,
		ExpectRecover: true,
	}

	if scenario.Name != "test" {
		t.Error("expected name test")
	}
	if len(scenario.Failures) != 1 {
		t.Errorf("expected 1 failure, got %d", len(scenario.Failures))
	}
}

// TestChaos_RecoverySequence verifies the recovery sequence.
func TestChaos_RecoverySequence(t *testing.T) {
	engine := NewChaosEngine()

	engine.InjectFailure(FailureNetworkPartition)

	if engine.GetState() != StateFailureInjected {
		t.Fatalf("expected state failure_injected, got %s", engine.GetState())
	}

	if err := engine.Recover(); err != nil {
		t.Fatalf("recover failed: %v", err)
	}

	if engine.GetState() != StateRecovering {
		t.Errorf("expected state recovering, got %s", engine.GetState())
	}

	if err := engine.CompleteRecovery(); err != nil {
		t.Fatalf("complete recovery failed: %v", err)
	}

	if engine.GetState() != StateRecovered {
		t.Errorf("expected state recovered, got %s", engine.GetState())
	}
}

// TestChaos_ComplexScenario verifies a complex scenario.
func TestChaos_ComplexScenario(t *testing.T) {
	engine := NewChaosEngine()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	scenario := ChaosScenario{
		Name: "complex",
		Failures: []FailureType{
			FailureNetworkPartition,
			FailurePodKill,
			FailureHighLatency,
		},
		Duration:    100 * time.Millisecond,
		Recovery:    200 * time.Millisecond,
		ExpectRecover: true,
	}

	if err := engine.RunScenario(ctx, scenario); err != nil {
		t.Fatalf("chaos scenario failed: %v", err)
	}

	if engine.GetState() != StateRecovered {
		t.Errorf("expected state recovered, got %s", engine.GetState())
	}

	events := engine.GetEvents()
	if len(events) < 5 {
		t.Errorf("expected at least 5 events, got %d", len(events))
	}
}

// TestChaos_AllFailureTypes verifies all failure types have strings.
func TestChaos_AllFailureTypes(t *testing.T) {
	failures := []FailureType{
		FailureNone,
		FailureNetworkPartition,
		FailurePodKill,
		FailureRegionUnreachable,
		FailureHighLatency,
		FailurePacketLoss,
	}

	for _, f := range failures {
		if f.String() == "" {
			t.Error("failure type has empty string")
		}
	}
}

// TestChaos_FailureTypeUnknown verifies unknown failure type.
func TestChaos_FailureTypeUnknown(t *testing.T) {
	unknown := FailureType(999)
	if unknown.String() != "unknown" {
		t.Errorf("expected unknown, got %s", unknown.String())
	}
}

// TestChaos_ChaosStateUnknown verifies unknown chaos state.
func TestChaos_ChaosStateUnknown(t *testing.T) {
	unknown := ChaosState(999)
	if unknown.String() != "unknown" {
		t.Errorf("expected unknown, got %s", unknown.String())
	}
}
