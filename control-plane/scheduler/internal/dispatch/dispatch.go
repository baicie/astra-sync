package dispatch

import (
	"context"
	"errors"
	"fmt"
	"time"

	"io.astrasync/control-plane/job"
)

var ErrLeaseLost = errors.New("dispatch lease lost")

type Phase string

const (
	PhaseClaimed   Phase = "CLAIMED"
	PhaseStarting  Phase = "STARTING"
	PhaseRunning   Phase = "RUNNING"
	PhaseStopping  Phase = "STOPPING"
	PhaseSucceeded Phase = "SUCCEEDED"
	PhaseFailed    Phase = "FAILED"
	PhaseCanceled  Phase = "CANCELED"
)

type Identity struct {
	JobUID string
	Epoch  int64
}

func (i Identity) Validate() error {
	if i.JobUID == "" {
		return fmt.Errorf("job UID must not be blank")
	}
	if i.Epoch <= 0 {
		return fmt.Errorf("execution epoch must be positive")
	}
	return nil
}

type Record struct {
	Identity        Identity
	Key             job.Key
	OwnerID         string
	Phase           Phase
	LeaseExpiresAt  time.Time
	LastHeartbeatAt time.Time
	HeartbeatToken  string
	Attempt         int32
	LastError       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Store interface {
	Migrate(context.Context) error
	Claim(context.Context, string, int, time.Duration, time.Duration, time.Time) ([]Record, error)
	List(context.Context) ([]Record, error)
	Update(context.Context, Identity, string, Phase, string, time.Duration, time.Time) error
	RecordHeartbeat(context.Context, Identity, string, time.Time) error
	FenceExpiredHeartbeat(context.Context, Identity, string, string, time.Duration, time.Duration, time.Time) (bool, error)
	Complete(context.Context, Identity, string, Phase, string, time.Time) error
}

func Active(phase Phase) bool {
	switch phase {
	case PhaseClaimed, PhaseStarting, PhaseRunning, PhaseStopping:
		return true
	default:
		return false
	}
}

func Terminal(phase Phase) bool {
	return phase == PhaseSucceeded || phase == PhaseFailed || phase == PhaseCanceled
}
