package job

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalidTransition = errors.New("invalid job state transition")
	ErrStaleEpoch        = errors.New("stale execution epoch")
)

func (j Job) ReplaceSpec(spec Spec, now time.Time) (Job, error) {
	if err := spec.Validate(); err != nil {
		return Job{}, err
	}
	if active(j.Status.State) {
		return Job{}, fmt.Errorf("%w: cannot update spec while state is %s", ErrInvalidTransition, j.Status.State)
	}
	next := j.Clone()
	next.Spec = spec.Clone()
	next.UpdatedAt = now.UTC()
	return next, nil
}

func (j Job) Deletable() error {
	if active(j.Status.State) {
		return fmt.Errorf("%w: cannot delete while state is %s", ErrInvalidTransition, j.Status.State)
	}
	return nil
}

func (j Job) RequestStart(now time.Time) (Job, bool, error) {
	next := j.Clone()
	if next.Status.Desired == DesiredRunning &&
		(next.Status.State == StateInitializing || next.Status.State == StateRunning) {
		return next, false, nil
	}

	switch next.Status.State {
	case StateCreated, StateCanceled, StateFinished, StateFailed:
	case StateInitializing, StateRunning:
		if next.Status.Desired == DesiredRunning {
			return next, false, nil
		}
	default:
		return Job{}, false, fmt.Errorf("%w: cannot start from %s", ErrInvalidTransition, next.Status.State)
	}

	now = now.UTC()
	if next.Status.Epoch > 0 {
		next.Status.RestartCount++
	}
	next.Status.Epoch++
	next.Status.Desired = DesiredRunning
	next.Status.State = StateInitializing
	next.Status.StartTime = &now
	next.Status.EndTime = nil
	next.Status.Failure = nil
	next.UpdatedAt = now
	return next, true, nil
}

func (j Job) RequestStop(now time.Time) (Job, bool, error) {
	next := j.Clone()
	if next.Status.Desired == DesiredStopped {
		return next, false, nil
	}

	switch next.Status.State {
	case StateInitializing, StateRunning:
		next.Status.Desired = DesiredStopped
		next.Status.State = StateCanceling
		next.UpdatedAt = now.UTC()
		return next, true, nil
	case StateCanceled, StateFinished, StateFailed:
		next.Status.Desired = DesiredStopped
		next.UpdatedAt = now.UTC()
		return next, true, nil
	default:
		return Job{}, false, fmt.Errorf("%w: cannot stop from %s", ErrInvalidTransition, next.Status.State)
	}
}

func (j Job) Advance(epoch int64, state State, failure *Failure, now time.Time) (Job, bool, error) {
	next := j.Clone()
	if epoch != next.Status.Epoch {
		return Job{}, false, fmt.Errorf("%w: current=%d supplied=%d", ErrStaleEpoch, next.Status.Epoch, epoch)
	}
	if state == next.Status.State {
		return next, false, nil
	}
	if !allowedTransition(next.Status.State, state) {
		return Job{}, false, fmt.Errorf(
			"%w: cannot advance from %s to %s", ErrInvalidTransition, next.Status.State, state)
	}
	if state == StateFailed && failure == nil {
		return Job{}, false, fmt.Errorf("%w: failure details are required", ErrInvalidTransition)
	}
	if state != StateFailed && failure != nil {
		return Job{}, false, fmt.Errorf("%w: failure details require FAILED state", ErrInvalidTransition)
	}

	now = now.UTC()
	next.Status.State = state
	next.Status.Failure = nil
	next.UpdatedAt = now
	if state == StateRunning {
		next.Status.EndTime = nil
	}
	if terminal(state) {
		next.Status.Desired = DesiredStopped
		next.Status.EndTime = &now
	}
	if failure != nil {
		copyFailure := *failure
		copyFailure.Timestamp = copyFailure.Timestamp.UTC()
		next.Status.Failure = &copyFailure
	}
	return next, true, nil
}

func allowedTransition(current, next State) bool {
	switch current {
	case StateInitializing:
		return next == StateRunning || next == StateFailed
	case StateRunning:
		return next == StateFinished || next == StateFailed
	case StateCanceling:
		return next == StateCanceled || next == StateFailed
	default:
		return false
	}
}

func terminal(state State) bool {
	return state == StateCanceled || state == StateFinished || state == StateFailed
}

func active(state State) bool {
	return state == StateInitializing || state == StateRunning || state == StateCanceling
}
