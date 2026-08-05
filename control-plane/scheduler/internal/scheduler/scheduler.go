package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"io.astrasync/control-plane/job"
	"io.astrasync/control-plane/scheduler/internal/dispatch"
)

type ObservationState string

const (
	ObservationPending   ObservationState = "PENDING"
	ObservationRunning   ObservationState = "RUNNING"
	ObservationSucceeded ObservationState = "SUCCEEDED"
	ObservationFailed    ObservationState = "FAILED"
)

type Observation struct {
	State   ObservationState
	Reason  string
	Message string
}

type ExecutionDispatcher interface {
	Reconcile(context.Context, job.Job, int64) (Observation, error)
	Stop(context.Context, dispatch.Identity) (bool, error)
	Cleanup(context.Context, dispatch.Identity) error
}

type JobRepository interface {
	Get(context.Context, job.Key) (job.Job, error)
	Update(context.Context, job.Job, int64) (job.Job, error)
}

type PermanentError struct {
	Err error
}

func (e *PermanentError) Error() string {
	return e.Err.Error()
}

func (e *PermanentError) Unwrap() error {
	return e.Err
}

func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentError{Err: err}
}

type Reconciler struct {
	dispatches       dispatch.Store
	jobs             JobRepository
	dispatcher       ExecutionDispatcher
	ownerID          string
	maximumActive    int
	leaseDuration    time.Duration
	reconcileEvery   time.Duration
	operationTimeout time.Duration
	clock            func() time.Time
	logger           *slog.Logger
}

type Config struct {
	OwnerID          string
	MaximumActive    int
	LeaseDuration    time.Duration
	ReconcileEvery   time.Duration
	OperationTimeout time.Duration
}

func New(
	config Config,
	dispatches dispatch.Store,
	jobs JobRepository,
	dispatcher ExecutionDispatcher,
	clock func() time.Time,
	logger *slog.Logger,
) (*Reconciler, error) {
	if config.OwnerID == "" {
		return nil, fmt.Errorf("scheduler owner ID must not be blank")
	}
	if config.MaximumActive <= 0 {
		return nil, fmt.Errorf("scheduler maximum active jobs must be positive")
	}
	if config.LeaseDuration <= 0 || config.ReconcileEvery <= 0 || config.ReconcileEvery >= config.LeaseDuration {
		return nil, fmt.Errorf("scheduler intervals must be positive and reconciliation must be shorter than the lease")
	}
	if config.OperationTimeout <= 0 || config.OperationTimeout >= config.LeaseDuration {
		return nil, fmt.Errorf("scheduler operation timeout must be positive and shorter than the lease")
	}
	if dispatches == nil || jobs == nil || dispatcher == nil || clock == nil {
		return nil, fmt.Errorf("scheduler dependencies must not be nil")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Reconciler{
		dispatches:       dispatches,
		jobs:             jobs,
		dispatcher:       dispatcher,
		ownerID:          config.OwnerID,
		maximumActive:    config.MaximumActive,
		leaseDuration:    config.LeaseDuration,
		reconcileEvery:   config.ReconcileEvery,
		operationTimeout: config.OperationTimeout,
		clock:            clock,
		logger:           logger,
	}, nil
}

func (r *Reconciler) Run(ctx context.Context) error {
	if err := r.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
		r.logger.Error("scheduler reconciliation failed", "error", err)
	}
	ticker := time.NewTicker(r.reconcileEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := r.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
				r.logger.Error("scheduler reconciliation failed", "error", err)
			}
		}
	}
}

func (r *Reconciler) Tick(ctx context.Context) error {
	claimContext, cancelClaim := context.WithTimeout(ctx, r.operationTimeout)
	defer cancelClaim()
	records, err := r.dispatches.Claim(
		claimContext, r.ownerID, r.maximumActive, r.leaseDuration, r.clock().UTC())
	if err != nil {
		return err
	}
	var wait sync.WaitGroup
	errorsChannel := make(chan error, len(records))
	for _, record := range records {
		record := record
		wait.Add(1)
		go func() {
			defer wait.Done()
			operationContext, cancel := context.WithTimeout(ctx, r.operationTimeout)
			defer cancel()
			if reconcileErr := r.reconcile(operationContext, record); reconcileErr != nil {
				errorsChannel <- fmt.Errorf(
					"reconcile %s/%s epoch %d: %w",
					record.Key.Namespace,
					record.Key.Name,
					record.Identity.Epoch,
					reconcileErr,
				)
			}
		}()
	}
	wait.Wait()
	close(errorsChannel)
	var failures []error
	for reconcileErr := range errorsChannel {
		failures = append(failures, reconcileErr)
	}
	return errors.Join(failures...)
}

func (r *Reconciler) reconcile(ctx context.Context, record dispatch.Record) error {
	current, err := r.jobs.Get(ctx, record.Key)
	if err != nil {
		return r.recordTransientError(ctx, record, err)
	}
	if current.UID != record.Identity.JobUID || current.Status.Epoch != record.Identity.Epoch {
		stopped, stopErr := r.dispatcher.Stop(ctx, record.Identity)
		if stopErr != nil {
			return r.recordTransientError(ctx, record, stopErr)
		}
		if !stopped {
			return r.keepAlive(ctx, record, dispatch.PhaseStopping, "")
		}
		return r.dispatches.Complete(
			ctx, record.Identity, r.ownerID, dispatch.PhaseCanceled, "stale execution identity", r.clock().UTC())
	}

	if terminalPhase, terminal := phaseForTerminalState(current.Status.State); terminal {
		if cleanupErr := r.dispatcher.Cleanup(ctx, record.Identity); cleanupErr != nil {
			return r.recordTransientError(ctx, record, cleanupErr)
		}
		return r.dispatches.Complete(
			ctx, record.Identity, r.ownerID, terminalPhase, failureText(current.Status.Failure), r.clock().UTC())
	}
	if current.Status.Desired == job.DesiredStopped || current.Status.State == job.StateCanceling {
		return r.cancel(ctx, record)
	}
	if current.Status.Desired != job.DesiredRunning ||
		(current.Status.State != job.StateInitializing && current.Status.State != job.StateRunning) {
		return r.recordTransientError(ctx, record, fmt.Errorf(
			"job has unsupported active lifecycle desired=%s state=%s",
			current.Status.Desired,
			current.Status.State,
		))
	}

	if err := r.keepAlive(ctx, record, dispatch.PhaseStarting, ""); err != nil {
		return err
	}
	record.Phase = dispatch.PhaseStarting
	observation, err := r.dispatcher.Reconcile(ctx, current, record.Identity.Epoch)
	if err != nil {
		var permanent *PermanentError
		if errors.As(err, &permanent) {
			return r.failPermanently(ctx, record, "DispatchRejected", permanent.Error())
		}
		return r.recordTransientError(ctx, record, err)
	}
	switch observation.State {
	case ObservationPending:
		return r.keepAlive(ctx, record, dispatch.PhaseStarting, "")
	case ObservationRunning:
		if err := r.transition(ctx, record, job.StateRunning, nil); err != nil {
			return err
		}
		return r.keepAlive(ctx, record, dispatch.PhaseRunning, "")
	case ObservationSucceeded:
		if err := r.transition(ctx, record, job.StateFinished, nil); err != nil {
			return err
		}
		return r.dispatches.Complete(
			ctx, record.Identity, r.ownerID, dispatch.PhaseSucceeded, "", r.clock().UTC())
	case ObservationFailed:
		reason := observation.Reason
		if reason == "" {
			reason = "CoordinatorFailed"
		}
		return r.failPermanently(ctx, record, reason, observation.Message)
	default:
		return r.recordTransientError(ctx, record, fmt.Errorf("unknown dispatch observation %q", observation.State))
	}
}

func (r *Reconciler) cancel(ctx context.Context, record dispatch.Record) error {
	if err := r.keepAlive(ctx, record, dispatch.PhaseStopping, ""); err != nil {
		return err
	}
	record.Phase = dispatch.PhaseStopping
	stopped, err := r.dispatcher.Stop(ctx, record.Identity)
	if err != nil {
		return r.recordTransientError(ctx, record, err)
	}
	if !stopped {
		return r.keepAlive(ctx, record, dispatch.PhaseStopping, "")
	}
	if err := r.transition(ctx, record, job.StateCanceled, nil); err != nil {
		return err
	}
	return r.dispatches.Complete(
		ctx, record.Identity, r.ownerID, dispatch.PhaseCanceled, "", r.clock().UTC())
}

func (r *Reconciler) failPermanently(
	ctx context.Context, record dispatch.Record, reason string, rootCause string,
) error {
	failure := &job.Failure{
		Reason: reason, RootCause: rootCause, Timestamp: r.clock().UTC(), Host: r.ownerID,
	}
	if err := r.transition(ctx, record, job.StateFailed, failure); err != nil {
		return err
	}
	return r.dispatches.Complete(
		ctx, record.Identity, r.ownerID, dispatch.PhaseFailed, rootCause, r.clock().UTC())
}

func (r *Reconciler) transition(
	ctx context.Context, record dispatch.Record, target job.State, failure *job.Failure,
) error {
	for attempt := 0; attempt < 5; attempt++ {
		current, err := r.jobs.Get(ctx, record.Key)
		if err != nil {
			return err
		}
		if current.UID != record.Identity.JobUID || current.Status.Epoch != record.Identity.Epoch {
			return job.ErrStaleEpoch
		}
		if current.Status.State == target {
			return nil
		}
		if target == job.StateFinished && current.Status.State == job.StateInitializing {
			if err := r.updateState(ctx, current, record.Identity.Epoch, job.StateRunning, nil); err != nil {
				if errors.Is(err, job.ErrConflict) {
					continue
				}
				return err
			}
			continue
		}
		if _, terminal := phaseForTerminalState(current.Status.State); terminal {
			return fmt.Errorf("execution already ended in state %s", current.Status.State)
		}
		if err := r.updateState(ctx, current, record.Identity.Epoch, target, failure); err != nil {
			if errors.Is(err, job.ErrConflict) {
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("job status update exceeded conflict retry limit: %w", job.ErrConflict)
}

func (r *Reconciler) updateState(
	ctx context.Context, current job.Job, epoch int64, target job.State, failure *job.Failure,
) error {
	next, changed, err := current.Advance(epoch, target, failure, r.clock().UTC())
	if err != nil || !changed {
		return err
	}
	_, err = r.jobs.Update(ctx, next, current.Version)
	return err
}

func (r *Reconciler) keepAlive(
	ctx context.Context, record dispatch.Record, phase dispatch.Phase, lastError string,
) error {
	return r.dispatches.Update(
		ctx,
		record.Identity,
		r.ownerID,
		phase,
		lastError,
		r.leaseDuration,
		r.clock().UTC(),
	)
}

func (r *Reconciler) recordTransientError(
	ctx context.Context, record dispatch.Record, operationErr error,
) error {
	leaseErr := r.keepAlive(ctx, record, record.Phase, operationErr.Error())
	if leaseErr != nil && !errors.Is(leaseErr, dispatch.ErrLeaseLost) {
		return errors.Join(operationErr, leaseErr)
	}
	return operationErr
}

func phaseForTerminalState(state job.State) (dispatch.Phase, bool) {
	switch state {
	case job.StateFinished:
		return dispatch.PhaseSucceeded, true
	case job.StateFailed:
		return dispatch.PhaseFailed, true
	case job.StateCanceled:
		return dispatch.PhaseCanceled, true
	default:
		return "", false
	}
}

func failureText(failure *job.Failure) string {
	if failure == nil {
		return ""
	}
	if failure.RootCause == "" {
		return failure.Reason
	}
	return failure.Reason + ": " + failure.RootCause
}
