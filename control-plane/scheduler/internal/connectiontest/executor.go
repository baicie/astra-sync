package connectiontest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"io.astrasync/control-plane/connection"
	"io.astrasync/control-plane/scheduler/internal/materialization"
)

type WorkRepository interface {
	ClaimTests(context.Context, string, int, time.Duration, time.Time) ([]connection.TestWork, error)
	CompleteTest(context.Context, string, string, connection.TestCompletion, time.Time) (connection.TestOperation, error)
	ExpireTests(context.Context, time.Time) (int64, error)
}

type ExecutorConfig struct {
	ExecutorID        string
	Concurrency       int
	ClaimBatch        int
	ClaimInterval     time.Duration
	LeaseDuration     time.Duration
	ProbeTimeout      time.Duration
	CompletionTimeout time.Duration
}

func (c ExecutorConfig) Validate() error {
	if len(c.ExecutorID) == 0 || len(c.ExecutorID) > connection.MaximumTestExecutorID ||
		c.Concurrency <= 0 || c.Concurrency > 64 || c.ClaimBatch <= 0 ||
		c.ClaimBatch > c.Concurrency || c.ClaimBatch > connection.MaximumTestClaimBatch {
		return fmt.Errorf("Connection test executor identity or concurrency is invalid")
	}
	if c.ClaimInterval < 100*time.Millisecond || c.ClaimInterval > time.Minute ||
		c.ProbeTimeout < time.Second || c.ProbeTimeout > 2*time.Minute ||
		c.CompletionTimeout <= 0 || c.CompletionTimeout > 10*time.Second ||
		c.LeaseDuration <= c.ProbeTimeout+c.CompletionTimeout ||
		c.LeaseDuration > connection.MaximumTestLease {
		return fmt.Errorf("Connection test executor timing is invalid")
	}
	return nil
}

type Executor struct {
	repository WorkRepository
	provider   materialization.CredentialProvider
	registry   *Registry
	guard      *EgressGuard
	config     ExecutorConfig
	clock      func() time.Time
}

func NewExecutor(
	repository WorkRepository,
	provider materialization.CredentialProvider,
	registry *Registry,
	guard *EgressGuard,
	configuration ExecutorConfig,
	clock func() time.Time,
) (*Executor, error) {
	if repository == nil || provider == nil || registry == nil || guard == nil || clock == nil {
		return nil, fmt.Errorf("Connection test executor dependencies must not be nil")
	}
	if err := configuration.Validate(); err != nil {
		return nil, err
	}
	return &Executor{
		repository: repository, provider: provider, registry: registry,
		guard: guard, config: configuration, clock: clock,
	}, nil
}

func (e *Executor) Run(ctx context.Context) error {
	for {
		claimed, err := e.RunOnce(ctx)
		if err != nil {
			return err
		}
		if claimed == e.config.ClaimBatch {
			continue
		}
		timer := time.NewTimer(e.config.ClaimInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil
		case <-timer.C:
		}
	}
}

func (e *Executor) RunOnce(ctx context.Context) (int, error) {
	if ctx.Err() != nil {
		return 0, nil
	}
	now := e.clock().UTC()
	if _, err := e.repository.ExpireTests(ctx, now); err != nil {
		return 0, fmt.Errorf("reconcile Connection test deadlines: %w", err)
	}
	work, err := e.repository.ClaimTests(
		ctx, e.config.ExecutorID, e.config.ClaimBatch, e.config.LeaseDuration, now,
	)
	if err != nil {
		return 0, fmt.Errorf("claim Connection tests: %w", err)
	}
	var group sync.WaitGroup
	errorsFound := make(chan error, len(work))
	for _, item := range work {
		item := item
		group.Add(1)
		go func() {
			defer group.Done()
			if err := e.execute(ctx, item); err != nil {
				errorsFound <- err
			}
		}()
	}
	group.Wait()
	close(errorsFound)
	var result error
	for err := range errorsFound {
		result = errors.Join(result, err)
	}
	return len(work), result
}

func (e *Executor) execute(ctx context.Context, work connection.TestWork) error {
	now := e.clock().UTC()
	deadline := minTime(work.Operation.DeadlineAt, now.Add(e.config.ProbeTimeout))
	completionBoundary := work.LeaseExpiresAt.Add(-e.config.CompletionTimeout)
	deadline = minTime(deadline, completionBoundary)
	result := TimedOutProbe(connection.TestPhasePolicy)
	if deadline.After(now) {
		attemptContext, cancel := context.WithDeadline(ctx, deadline)
		result = e.probe(attemptContext, work)
		attemptError := attemptContext.Err()
		cancel()
		if ctx.Err() != nil {
			result = CanceledProbe(connection.TestPhaseHandshake)
		} else if attemptError != nil {
			result = TimedOutProbe(connection.TestPhaseHandshake)
		}
	}
	completion, err := result.Completion()
	if err != nil {
		completion, _ = FailedProbe(
			connection.TestPhaseHandshake, connection.TestResultExecutorUnavailable,
			"connection.test.executor",
		).Completion()
	}
	completionContext, cancel := context.WithTimeout(
		context.WithoutCancel(ctx), e.config.CompletionTimeout,
	)
	defer cancel()
	_, err = e.repository.CompleteTest(
		completionContext, work.Operation.OperationID, e.config.ExecutorID,
		completion, e.clock().UTC(),
	)
	if errors.Is(err, connection.ErrTestLeaseLost) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("complete Connection test %s: %w", work.Operation.OperationID, err)
	}
	return nil
}

func (e *Executor) probe(ctx context.Context, work connection.TestWork) ProbeResult {
	credentialFields := make(map[string][]byte)
	defer clearFields(credentialFields)
	if work.Generation.Generation.SecretLocator.Provider != "" {
		values, err := e.provider.Resolve(ctx, materialization.CredentialRequest{
			TenantID: work.Generation.TenantID, TenantNamespace: work.Generation.TenantNamespace,
			Locator: work.Generation.Generation.SecretLocator.Clone(),
		})
		if err != nil {
			if ctx.Err() != nil {
				return TimedOutProbe(connection.TestPhaseAuthentication)
			}
			return FailedProbe(
				connection.TestPhaseAuthentication, connection.TestResultSecretUnavailable,
				"connection.test.secret",
			)
		}
		defer values.Close()
		credentialFields, err = values.Fields()
		if err != nil {
			return FailedProbe(
				connection.TestPhaseAuthentication, connection.TestResultSecretUnavailable,
				"connection.test.secret",
			)
		}
		defer clearFields(credentialFields)
	}
	configuration, err := NewConfiguration(
		work.Generation.Generation.Settings, credentialFields,
	)
	if err != nil {
		return FailedProbe(
			connection.TestPhaseHandshake, connection.TestResultHandshakeFailed,
			"connection.test.configuration",
		)
	}
	defer configuration.Close()
	return e.registry.Execute(
		ctx, work.Generation.Connector, configuration, e.guard,
		work.Operation.EgressPolicy.Clone(),
	)
}

func clearFields(values map[string][]byte) {
	for key, value := range values {
		clear(value)
		delete(values, key)
	}
}

func minTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}
