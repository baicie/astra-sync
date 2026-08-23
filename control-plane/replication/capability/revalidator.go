// Package capability provides sink capability revalidation for cross-region failover.
package capability

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// Capability represents the delivery guarantee capability of a sink.
type Capability int

const (
	// CapabilityExactlyOnce represents exactly-once delivery.
	CapabilityExactlyOnce Capability = iota
	// CapabilityAtLeastOnce represents at-least-once delivery.
	CapabilityAtLeastOnce
	// CapabilityAtMostOnce represents at-most-once delivery.
	CapabilityAtMostOnce
)

// String returns the string representation of the capability.
func (c Capability) String() string {
	switch c {
	case CapabilityExactlyOnce:
		return "exactly-once"
	case CapabilityAtLeastOnce:
		return "at-least-once"
	case CapabilityAtMostOnce:
		return "at-most-once"
	default:
		return "unknown"
	}
}

// RevalidationResult holds the result of a capability revalidation.
type RevalidationResult struct {
	// Whether the sink is reachable.
	Reachable bool
	// The confirmed capability.
	Capability Capability
	// Error message if revalidation failed.
	ErrorMessage string
	// Duration of the revalidation.
	Duration time.Duration
	// Negotiated capability must satisfy this requirement when present.
	RequiredCapability *Capability
}

// RevalidationResultString returns a human-readable string for the result.
func (r *RevalidationResult) String() string {
	if !r.Reachable {
		return fmt.Sprintf("unreachable: %s", r.ErrorMessage)
	}
	return fmt.Sprintf("%s (%s)", r.Capability, r.Duration)
}

// ConnectionInfo holds information about a sink connection.
type ConnectionInfo struct {
	JobID              string
	SinkEndpoint       string
	Database           string
	Table              string
	RequiredCapability *Capability
}

// ConnectionCatalog provides access to connection information.
type ConnectionCatalog interface {
	// GetConnection returns connection information for a job.
	GetConnection(ctx context.Context, jobID string) (*ConnectionInfo, error)
}

// CapabilityNegotiator negotiates sink capabilities.
type CapabilityNegotiator interface {
	// Negotiate attempts to negotiate a capability with the sink.
	Negotiate(ctx context.Context, endpoint string) (Capability, error)
}

// ReachabilityProber optionally performs a bounded transport probe before negotiation.
type ReachabilityProber interface {
	Probe(ctx context.Context, endpoint string) error
}

// AuditLogger logs audit events for capability revalidation.
type AuditLogger interface {
	// LogCapabilityRevalidationStarted logs the start of a revalidation.
	LogCapabilityRevalidationStarted(ctx context.Context, jobID string)
	// LogCapabilityConfirmed logs a successful revalidation.
	LogCapabilityConfirmed(ctx context.Context, jobID string, capability Capability, duration time.Duration)
	// LogCapabilityRejected logs a failed revalidation.
	LogCapabilityRejected(ctx context.Context, jobID string, reason string)
	// LogPromotionAborted logs a promotion abort due to capability failure.
	LogPromotionAborted(ctx context.Context, jobID string, reason string)
}

// Revalidator revalidates sink capabilities for cross-region failover.
type Revalidator struct {
	cfg           Config
	logger        *zap.Logger
	catalog       ConnectionCatalog
	negotiator    CapabilityNegotiator
	auditor       AuditLogger
	total         atomic.Int64
	success       atomic.Int64
	failure       atomic.Int64
	totalDuration atomic.Int64
}

// Config holds the configuration for the revalidator.
type Config struct {
	// Default timeout for revalidation.
	Timeout time.Duration
	// Probe timeout for reachability check.
	ProbeTimeout time.Duration
	// Maximum retries for negotiation.
	MaxRetries int
	// Retry backoff.
	RetryBackoff time.Duration
}

// Option is a functional option for revalidator configuration.
type Option func(*Config)

// WithTimeout sets the revalidation timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Config) {
		c.Timeout = d
	}
}

// WithProbeTimeout sets the probe timeout for reachability check.
func WithProbeTimeout(d time.Duration) Option {
	return func(c *Config) {
		c.ProbeTimeout = d
	}
}

// WithMaxRetries sets the maximum retries for negotiation.
func WithMaxRetries(n int) Option {
	return func(c *Config) {
		c.MaxRetries = n
	}
}

// WithRetryBackoff sets the retry backoff.
func WithRetryBackoff(d time.Duration) Option {
	return func(c *Config) {
		c.RetryBackoff = d
	}
}

// NewRevalidator creates a new capability revalidator.
func NewRevalidator(
	logger *zap.Logger,
	catalog ConnectionCatalog,
	negotiator CapabilityNegotiator,
	auditor AuditLogger,
	opts ...Option,
) *Revalidator {
	cfg := Config{
		Timeout:      60 * time.Second,
		ProbeTimeout: 5 * time.Second,
		MaxRetries:   3,
		RetryBackoff: 2 * time.Second,
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	return &Revalidator{
		cfg:        cfg,
		logger:     logger.With(zap.String("component", "capability-revalidator")),
		catalog:    catalog,
		negotiator: negotiator,
		auditor:    auditor,
	}
}

// Revalidate revalidates the sink capability for a job.
// This implements the promotion.CapabilityRevalidator interface.
func (r *Revalidator) Revalidate(ctx context.Context, jobID string, timeout time.Duration) error {
	return r.revalidate(ctx, jobID, timeout)
}

// RevalidateFor verifies that the negotiated capability satisfies the requested guarantee.
func (r *Revalidator) RevalidateFor(ctx context.Context, jobID string, timeout time.Duration, required Capability) error {
	return r.revalidateWithRequirement(ctx, jobID, timeout, &required)
}

func (r *Revalidator) revalidate(ctx context.Context, jobID string, timeout time.Duration) error {
	return r.revalidateWithRequirement(ctx, jobID, timeout, nil)
}

func (r *Revalidator) revalidateWithRequirement(ctx context.Context, jobID string, timeout time.Duration, explicit *Capability) error {
	start := time.Now()
	r.total.Add(1)
	defer func() { r.totalDuration.Add(time.Since(start).Nanoseconds()) }()

	// Use configured timeout if not specified
	if timeout == 0 {
		timeout = r.cfg.Timeout
	}

	// Create a context with deadline
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Log start
	if r.auditor != nil {
		r.auditor.LogCapabilityRevalidationStarted(ctx, jobID)
	}

	r.logger.Info("starting capability revalidation",
		zap.String("jobID", jobID),
		zap.Duration("timeout", timeout))

	// Step 1: Get connection information
	if r.catalog == nil || r.negotiator == nil {
		r.failure.Add(1)
		return ErrCapabilityNegotiationFailed
	}
	connInfo, err := r.catalog.GetConnection(ctx, jobID)
	if err != nil {
		r.abort(ctx, jobID, fmt.Sprintf("get connection: %v", err))
		return fmt.Errorf("get connection: %w", err)
	}
	if connInfo == nil || connInfo.SinkEndpoint == "" {
		r.failure.Add(1)
		r.abort(ctx, jobID, ErrSinkUnreachable.Error())
		return ErrSinkUnreachable
	}
	if prober, ok := r.negotiator.(ReachabilityProber); ok {
		probeCtx, probeCancel := context.WithTimeout(ctx, r.cfg.ProbeTimeout)
		probeErr := prober.Probe(probeCtx, connInfo.SinkEndpoint)
		probeCancel()
		if probeErr != nil {
			r.failure.Add(1)
			r.abort(ctx, jobID, fmt.Sprintf("probe failed: %v", probeErr))
			return fmt.Errorf("%w: %v", ErrSinkUnreachable, probeErr)
		}
	}
	required := explicit
	if required == nil {
		required = connInfo.RequiredCapability
	}

	r.logger.Debug("got connection info",
		zap.String("jobID", jobID),
		zap.String("endpoint", connInfo.SinkEndpoint))

	// Step 2: Negotiate capability with retries
	var lastErr error
	for attempt := 0; attempt <= r.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			r.logger.Debug("retrying negotiation",
				zap.String("jobID", jobID),
				zap.Int("attempt", attempt),
				zap.Duration("backoff", r.cfg.RetryBackoff))

			select {
			case <-ctx.Done():
				r.abort(ctx, jobID, fmt.Sprintf("context cancelled during retry: %v", ctx.Err()))
				return ctx.Err()
			case <-time.After(r.cfg.RetryBackoff):
			}
		}

		capability, err := r.negotiator.Negotiate(ctx, connInfo.SinkEndpoint)
		if err == nil {
			if required != nil && capability > *required {
				lastErr = fmt.Errorf("%w: negotiated=%s required=%s", ErrCapabilityNegotiationFailed, capability, *required)
				r.logger.Warn("negotiated capability is insufficient", zap.String("jobID", jobID), zap.String("negotiated", capability.String()), zap.String("required", required.String()))
				continue
			}
			duration := time.Since(start)

			r.logger.Info("capability revalidation succeeded",
				zap.String("jobID", jobID),
				zap.String("capability", capability.String()),
				zap.Duration("duration", duration))

			if r.auditor != nil {
				r.auditor.LogCapabilityConfirmed(ctx, jobID, capability, duration)
			}
			r.success.Add(1)

			return nil
		}

		lastErr = err
		r.logger.Warn("negotiation attempt failed",
			zap.String("jobID", jobID),
			zap.Int("attempt", attempt+1),
			zap.Error(err))
	}

	// All retries exhausted
	r.failure.Add(1)
	r.abort(ctx, jobID, fmt.Sprintf("all retries exhausted: %v", lastErr))
	return fmt.Errorf("capability revalidation failed after %d retries: %w", r.cfg.MaxRetries, lastErr)
}

// abort logs the promotion abort and returns an error.
func (r *Revalidator) abort(ctx context.Context, jobID, reason string) {
	r.logger.Error("capability revalidation failed, aborting promotion",
		zap.String("jobID", jobID),
		zap.String("reason", reason))

	if r.auditor != nil {
		r.auditor.LogCapabilityRejected(ctx, jobID, reason)
		r.auditor.LogPromotionAborted(ctx, jobID, reason)
	}
}

// GetRevalidationResult performs a full revalidation and returns the result.
// This is useful for diagnostics and testing.
func (r *Revalidator) GetRevalidationResult(ctx context.Context, jobID string, timeout time.Duration) (*RevalidationResult, error) {
	start := time.Now()

	result := &RevalidationResult{}

	// Get connection info
	if r.catalog == nil || r.negotiator == nil {
		return result, ErrCapabilityNegotiationFailed
	}
	connInfo, err := r.catalog.GetConnection(ctx, jobID)
	if err != nil {
		result.Reachable = false
		result.ErrorMessage = fmt.Sprintf("get connection: %v", err)
		result.Duration = time.Since(start)
		return result, err
	}
	if connInfo == nil || connInfo.SinkEndpoint == "" {
		result.Reachable = false
		result.ErrorMessage = ErrSinkUnreachable.Error()
		result.Duration = time.Since(start)
		return result, ErrSinkUnreachable
	}

	// Negotiate capability
	capability, err := r.negotiator.Negotiate(ctx, connInfo.SinkEndpoint)
	if err != nil {
		result.Reachable = false
		result.ErrorMessage = fmt.Sprintf("negotiate: %v", err)
		result.Duration = time.Since(start)
		return result, err
	}

	result.Reachable = true
	result.Capability = capability
	result.RequiredCapability = connInfo.RequiredCapability
	if connInfo.RequiredCapability != nil && capability > *connInfo.RequiredCapability {
		result.ErrorMessage = fmt.Sprintf("negotiated capability %s does not satisfy required %s", capability, connInfo.RequiredCapability)
		result.Duration = time.Since(start)
		return result, fmt.Errorf("%w: %s", ErrCapabilityNegotiationFailed, result.ErrorMessage)
	}
	result.Duration = time.Since(start)
	return result, nil
}

// RevalidatorMetrics holds metrics for the revalidator.
type RevalidatorMetrics struct {
	TotalRevalidations      int64
	SuccessfulRevalidations int64
	FailedRevalidations     int64
	TotalDuration           time.Duration
}

// Metrics returns the current revalidator metrics.
func (r *Revalidator) Metrics() RevalidatorMetrics {
	return RevalidatorMetrics{
		TotalRevalidations:      r.total.Load(),
		SuccessfulRevalidations: r.success.Load(),
		FailedRevalidations:     r.failure.Load(),
		TotalDuration:           time.Duration(r.totalDuration.Load()),
	}
}

// ErrSinkUnreachable is returned when the sink is not reachable.
var ErrSinkUnreachable = errors.New("capability: sink is not reachable")

// ErrCapabilityNegotiationFailed is returned when capability negotiation fails.
var ErrCapabilityNegotiationFailed = errors.New("capability: negotiation failed")

// ErrTimeout is returned when the revalidation times out.
var ErrTimeout = errors.New("capability: revalidation timeout")
