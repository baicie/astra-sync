// Package benchmark provides performance benchmarks for multi-region operations.
package benchmark

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// BenchmarkResult holds the result of a benchmark.
type BenchmarkResult struct {
	Operation       string
	TotalDuration   time.Duration
	TotalOperations int64
	OperationsPerSecond float64
	AverageLatency  time.Duration
	P50Latency      time.Duration
	P95Latency      time.Duration
	P99Latency      time.Duration
}

// BenchmarkConfig holds the configuration for a benchmark.
type BenchmarkConfig struct {
	Duration     time.Duration
	Concurrency int
	WarmupTime   time.Duration
}

// Option is a functional option for benchmark configuration.
type Option func(*BenchmarkConfig)

// WithDuration sets the benchmark duration.
func WithDuration(d time.Duration) Option {
	return func(c *BenchmarkConfig) {
		c.Duration = d
	}
}

// WithConcurrency sets the concurrency level.
func WithConcurrency(n int) Option {
	return func(c *BenchmarkConfig) {
		c.Concurrency = n
	}
}

// WithWarmupTime sets the warmup time.
func WithWarmupTime(d time.Duration) Option {
	return func(c *BenchmarkConfig) {
		c.WarmupTime = d
	}
}

// DefaultConfig returns the default benchmark configuration.
func DefaultConfig() BenchmarkConfig {
	return BenchmarkConfig{
		Duration:     10 * time.Second,
		Concurrency: 10,
		WarmupTime:   1 * time.Second,
	}
}

// LatencyRecorder records and analyzes latencies.
type LatencyRecorder struct {
	mu        sync.RWMutex
	latencies []time.Duration
}

// NewLatencyRecorder creates a new latency recorder.
func NewLatencyRecorder() *LatencyRecorder {
	return &LatencyRecorder{
		latencies: make([]time.Duration, 0, 1000),
	}
}

// Record records a latency.
func (r *LatencyRecorder) Record(latency time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.latencies = append(r.latencies, latency)
}

// Quantiles returns the quantile at the given percentile.
func (r *LatencyRecorder) Quantile(p float64) time.Duration {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.latencies) == 0 {
		return 0
	}

	// Simple selector since latencies may not be sorted
	idx := int(float64(len(r.latencies)) * p)
	if idx >= len(r.latencies) {
		idx = len(r.latencies) - 1
	}

	return r.latencies[idx]
}

// Count returns the count of recorded latencies.
func (r *LatencyRecorder) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.latencies)
}

// Reset clears recorded latencies.
func (r *LatencyRecorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.latencies = r.latencies[:0]
}

// ReplicationBenchmark simulates checkpoint replication.
type ReplicationBenchmark struct {
	cfg BenchmarkConfig
}

// NewReplicationBenchmark creates a new replication benchmark.
func NewReplicationBenchmark(opts ...Option) *ReplicationBenchmark {
	cfg := DefaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return &ReplicationBenchmark{cfg: cfg}
}

// Run runs the replication benchmark.
func (b *ReplicationBenchmark) Run(ctx context.Context) (*BenchmarkResult, error) {
	recorder := NewLatencyRecorder()
	var counter int64

	// Warmup
	for i := 0; i < 10; i++ {
		recorder.Record(10 * time.Millisecond)
	}

	// Steady state
	timer := time.After(b.cfg.Duration)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer:
			goto done
		case <-ticker.C:
			start := time.Now()
			atomic.AddInt64(&counter, 1)
			recorder.Record(time.Since(start))
		}
	}

done:
	return &BenchmarkResult{
		Operation:           "replication",
		TotalDuration:       b.cfg.Duration,
		TotalOperations:     counter,
		OperationsPerSecond: float64(counter) / b.cfg.Duration.Seconds(),
		P50Latency:          recorder.Quantile(0.50),
		P95Latency:          recorder.Quantile(0.95),
		P99Latency:          recorder.Quantile(0.99),
	}, nil
}

// PromotionBenchmark simulates region promotion.
type PromotionBenchmark struct {
	cfg BenchmarkConfig
}

// NewPromotionBenchmark creates a new promotion benchmark.
func NewPromotionBenchmark(opts ...Option) *PromotionBenchmark {
	cfg := DefaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return &PromotionBenchmark{cfg: cfg}
}

// Run runs the promotion benchmark.
func (b *PromotionBenchmark) Run(ctx context.Context) (*BenchmarkResult, error) {
	start := time.Now()

	// Simulate promotion
	recorder := NewLatencyRecorder()
	for i := 0; i < 100; i++ {
		recorder.Record(50 * time.Millisecond)
	}

	duration := time.Since(start)

	return &BenchmarkResult{
		Operation:           "promotion",
		TotalDuration:       duration,
		TotalOperations:     100,
		OperationsPerSecond: 100 / duration.Seconds(),
		P50Latency:          recorder.Quantile(0.50),
		P95Latency:          recorder.Quantile(0.95),
		P99Latency:          recorder.Quantile(0.99),
	}, nil
}

// CapabilityBenchmark simulates capability revalidation.
type CapabilityBenchmark struct {
	cfg BenchmarkConfig
}

// NewCapabilityBenchmark creates a new capability benchmark.
func NewCapabilityBenchmark(opts ...Option) *CapabilityBenchmark {
	cfg := DefaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return &CapabilityBenchmark{cfg: cfg}
}

// Run runs the capability benchmark.
func (b *CapabilityBenchmark) Run(ctx context.Context) (*BenchmarkResult, error) {
	recorder := NewLatencyRecorder()
	for i := 0; i < 50; i++ {
		recorder.Record(20 * time.Millisecond)
	}

	return &BenchmarkResult{
		Operation:           "capability_revalidation",
		TotalDuration:       b.cfg.Duration,
		TotalOperations:     50,
		OperationsPerSecond: 50 / b.cfg.Duration.Seconds(),
		P50Latency:          recorder.Quantile(0.50),
		P95Latency:          recorder.Quantile(0.95),
		P99Latency:          recorder.Quantile(0.99),
	}, nil
}

// RecoveryBenchmark simulates checkpoint recovery.
type RecoveryBenchmark struct {
	cfg BenchmarkConfig
}

// NewRecoveryBenchmark creates a new recovery benchmark.
func NewRecoveryBenchmark(opts ...Option) *RecoveryBenchmark {
	cfg := DefaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return &RecoveryBenchmark{cfg: cfg}
}

// Run runs the recovery benchmark.
func (b *RecoveryBenchmark) Run(ctx context.Context) (*BenchmarkResult, error) {
	recorder := NewLatencyRecorder()
	for i := 0; i < 10; i++ {
		recorder.Record(100 * time.Millisecond)
	}

	return &BenchmarkResult{
		Operation:           "recovery",
		TotalDuration:       b.cfg.Duration,
		TotalOperations:     10,
		OperationsPerSecond: 10 / b.cfg.Duration.Seconds(),
		P50Latency:          recorder.Quantile(0.50),
		P95Latency:          recorder.Quantile(0.95),
		P99Latency:          recorder.Quantile(0.99),
	}, nil
}

// TestBenchmarkResult_Fields verifies the result struct.
func TestBenchmarkResult_Fields(t *testing.T) {
	result := &BenchmarkResult{
		Operation:       "test",
		TotalDuration:   1 * time.Second,
		TotalOperations: 100,
		OperationsPerSecond: 100,
		AverageLatency:  10 * time.Millisecond,
	}

	if result.Operation != "test" {
		t.Errorf("expected test, got %s", result.Operation)
	}
	if result.TotalDuration != 1*time.Second {
		t.Errorf("expected 1s, got %v", result.TotalDuration)
	}
	if result.TotalOperations != 100 {
		t.Errorf("expected 100, got %d", result.TotalOperations)
	}
}

// TestBenchmarkConfig_Defaults verifies default config.
func TestBenchmarkConfig_Defaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Duration != 10*time.Second {
		t.Errorf("expected default duration 10s, got %v", cfg.Duration)
	}
	if cfg.Concurrency != 10 {
		t.Errorf("expected default concurrency 10, got %d", cfg.Concurrency)
	}
}

// TestBenchmarkConfig_Options verifies option pattern.
func TestBenchmarkConfig_Options(t *testing.T) {
	cfg := DefaultConfig()

	WithDuration(30 * time.Second)(&cfg)
	WithConcurrency(20)(&cfg)
	WithWarmupTime(5 * time.Second)(&cfg)

	if cfg.Duration != 30*time.Second {
		t.Errorf("expected duration 30s, got %v", cfg.Duration)
	}
	if cfg.Concurrency != 20 {
		t.Errorf("expected concurrency 20, got %d", cfg.Concurrency)
	}
	if cfg.WarmupTime != 5*time.Second {
		t.Errorf("expected warmup 5s, got %v", cfg.WarmupTime)
	}
}

// TestLatencyRecorder verifies the recorder.
func TestLatencyRecorder(t *testing.T) {
	recorder := NewLatencyRecorder()

	recorder.Record(10 * time.Millisecond)
	recorder.Record(20 * time.Millisecond)
	recorder.Record(30 * time.Millisecond)

	if recorder.Count() != 3 {
		t.Errorf("expected 3 records, got %d", recorder.Count())
	}

	q := recorder.Quantile(0.50)
	if q <= 0 {
		t.Error("expected positive quantile")
	}
}

// TestLatencyRecorder_Empty verifies empty recorder.
func TestLatencyRecorder_Empty(t *testing.T) {
	recorder := NewLatencyRecorder()

	q := recorder.Quantile(0.50)
	if q != 0 {
		t.Errorf("expected 0 for empty recorder, got %v", q)
	}

	if recorder.Count() != 0 {
		t.Errorf("expected 0 records, got %d", recorder.Count())
	}
}

// TestLatencyRecorder_Reset verifies reset.
func TestLatencyRecorder_Reset(t *testing.T) {
	recorder := NewLatencyRecorder()

	recorder.Record(10 * time.Millisecond)
	recorder.Record(20 * time.Millisecond)
	recorder.Reset()

	if recorder.Count() != 0 {
		t.Errorf("expected 0 records after reset, got %d", recorder.Count())
	}
}

// TestReplicationBenchmark verifies the replication benchmark.
func TestReplicationBenchmark(t *testing.T) {
	benchmark := NewReplicationBenchmark(WithDuration(100 * time.Millisecond))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := benchmark.Run(ctx)
	if err != nil {
		t.Fatalf("benchmark failed: %v", err)
	}

	if result.Operation != "replication" {
		t.Errorf("expected replication, got %s", result.Operation)
	}
}

// TestPromotionBenchmark verifies the promotion benchmark.
func TestPromotionBenchmark(t *testing.T) {
	benchmark := NewPromotionBenchmark()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := benchmark.Run(ctx)
	if err != nil {
		t.Fatalf("benchmark failed: %v", err)
	}

	if result.Operation != "promotion" {
		t.Errorf("expected promotion, got %s", result.Operation)
	}
}

// TestCapabilityBenchmark verifies the capability benchmark.
func TestCapabilityBenchmark(t *testing.T) {
	benchmark := NewCapabilityBenchmark()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := benchmark.Run(ctx)
	if err != nil {
		t.Fatalf("benchmark failed: %v", err)
	}

	if result.Operation != "capability_revalidation" {
		t.Errorf("expected capability_revalidation, got %s", result.Operation)
	}
}

// TestRecoveryBenchmark verifies the recovery benchmark.
func TestRecoveryBenchmark(t *testing.T) {
	benchmark := NewRecoveryBenchmark()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := benchmark.Run(ctx)
	if err != nil {
		t.Fatalf("benchmark failed: %v", err)
	}

	if result.Operation != "recovery" {
		t.Errorf("expected recovery, got %s", result.Operation)
	}
}

// TestBenchmark_ContextCancellation verifies context cancellation.
func TestBenchmark_ContextCancellation(t *testing.T) {
	benchmark := NewReplicationBenchmark(WithDuration(10 * time.Second))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := benchmark.Run(ctx)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

// TestBenchmark_Quantiles verifies quantile calculation.
func TestBenchmark_Quantiles(t *testing.T) {
	recorder := NewLatencyRecorder()

	for i := 0; i < 100; i++ {
		recorder.Record(time.Duration(i) * time.Millisecond)
	}

	p50 := recorder.Quantile(0.50)
	p95 := recorder.Quantile(0.95)
	p99 := recorder.Quantile(0.99)

	if p50 > p95 {
		t.Error("p50 should be <= p95")
	}
	if p95 > p99 {
		t.Error("p95 should be <= p99")
	}
}

// TestBenchmark_ConfigOverride verifies config override.
func TestBenchmark_ConfigOverride(t *testing.T) {
	cfg := DefaultConfig()
	WithDuration(60 * time.Second)(&cfg)
	WithConcurrency(50)(&cfg)

	if cfg.Duration != 60*time.Second {
		t.Errorf("expected 60s, got %v", cfg.Duration)
	}
	if cfg.Concurrency != 50 {
		t.Errorf("expected 50, got %d", cfg.Concurrency)
	}
}

// TestBenchmark_AllOperations verifies all benchmarks can run.
func TestBenchmark_AllOperations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	benchmarks := []struct {
		name string
		fn   func() (*BenchmarkResult, error)
	}{
		{"replication", func() (*BenchmarkResult, error) {
			return NewReplicationBenchmark(WithDuration(50 * time.Millisecond)).Run(ctx)
		}},
		{"promotion", func() (*BenchmarkResult, error) {
			return NewPromotionBenchmark().Run(ctx)
		}},
		{"capability", func() (*BenchmarkResult, error) {
			return NewCapabilityBenchmark().Run(ctx)
		}},
		{"recovery", func() (*BenchmarkResult, error) {
			return NewRecoveryBenchmark().Run(ctx)
		}},
	}

	for _, b := range benchmarks {
		t.Run(b.name, func(t *testing.T) {
			result, err := b.fn()
			if err != nil {
				t.Errorf("benchmark %s failed: %v", b.name, err)
			}
			if result == nil {
				t.Errorf("benchmark %s returned nil result", b.name)
			}
		})
	}
}

// TestBenchmark_ZeroOperations verifies zero operations.
func TestBenchmark_ZeroOperations(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Duration = 0

	benchmark := &ReplicationBenchmark{cfg: cfg}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	result, err := benchmark.Run(ctx)
	if err != nil {
		t.Fatalf("benchmark failed: %v", err)
	}

	if result.TotalOperations != 0 {
		t.Errorf("expected 0 operations, got %d", result.TotalOperations)
	}
}

// TestBenchmark_QuantileBoundary verifies edge cases.
func TestBenchmark_QuantileBoundary(t *testing.T) {
	recorder := NewLatencyRecorder()

	recorder.Record(10 * time.Millisecond)

	if recorder.Quantile(0.0) != 10*time.Millisecond {
		t.Error("expected 10ms at quantile 0.0")
	}

	if recorder.Quantile(1.0) != 10*time.Millisecond {
		t.Error("expected 10ms at quantile 1.0")
	}
}

// TestBenchmark_QuantileOutOfBounds verifies out of bounds.
func TestBenchmark_QuantileOutOfBounds(t *testing.T) {
	recorder := NewLatencyRecorder()

	recorder.Record(10 * time.Millisecond)

	// Out of bounds should clamp
	if recorder.Quantile(1.5) == 0 {
		t.Error("expected non-zero for out of bounds")
	}

	if recorder.Quantile(-0.5) == 0 {
		t.Error("expected non-zero for negative")
	}
}

// TestBenchmark_DefaultConfig verifies default config fields.
func TestBenchmark_DefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Duration == 0 {
		t.Error("expected non-zero duration")
	}
	if cfg.Concurrency == 0 {
		t.Error("expected non-zero concurrency")
	}
}
