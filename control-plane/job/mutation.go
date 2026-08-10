package job

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrIdempotencyReused  = errors.New("job idempotency key reused with another request")
	ErrMutationInProgress = errors.New("job mutation with this idempotency key is in progress")
	ErrValidationStale    = errors.New("job validation fence is stale")
)

var mutationRevisionPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type MutationKind string

const (
	MutationCreate MutationKind = "CREATE"
	MutationUpdate MutationKind = "UPDATE"
	MutationDelete MutationKind = "DELETE"
	MutationStart  MutationKind = "START"
	MutationStop   MutationKind = "STOP"
)

type ConnectionRole string

const (
	ConnectionRoleSource ConnectionRole = "SOURCE"
	ConnectionRoleSink   ConnectionRole = "SINK"
)

// ConnectionBinding is the metadata-only result of canonical admission. The
// repository rechecks it while locking the Job and Connection rows.
type ConnectionBinding struct {
	Role                     ConnectionRole
	TenantID                 string
	ReferenceName            string
	ConnectionUID            string
	Connector                string
	Generation               int64
	DescriptorRevision       string
	ConnectionSchemaRevision string
}

func (b ConnectionBinding) Validate() error {
	if b.Role != ConnectionRoleSource && b.Role != ConnectionRoleSink {
		return fmt.Errorf("Connection binding role is invalid")
	}
	if _, err := uuid.Parse(b.TenantID); err != nil {
		return fmt.Errorf("Connection binding tenant ID must be a UUID")
	}
	if err := validateDNSLabel("Connection reference", b.ReferenceName); err != nil {
		return err
	}
	if _, err := uuid.Parse(b.ConnectionUID); err != nil {
		return fmt.Errorf("Connection binding UID must be a UUID")
	}
	if len(b.Connector) > 128 || !connectorName.MatchString(b.Connector) || b.Generation <= 0 ||
		!mutationRevisionPattern.MatchString(b.DescriptorRevision) ||
		!mutationRevisionPattern.MatchString(b.ConnectionSchemaRevision) {
		return fmt.Errorf("Connection binding revision metadata is invalid")
	}
	return nil
}

type ValidationFence struct {
	ValidationID     string
	SpecDigest       string
	CompilerRevision string
	Bindings         []ConnectionBinding
}

func (f ValidationFence) Validate() error {
	if strings.TrimSpace(f.ValidationID) == "" || len(f.ValidationID) > 128 ||
		!mutationRevisionPattern.MatchString(f.SpecDigest) ||
		!mutationRevisionPattern.MatchString(f.CompilerRevision) || len(f.Bindings) > 2 {
		return fmt.Errorf("Job validation fence is invalid")
	}
	roles := make(map[ConnectionRole]struct{}, len(f.Bindings))
	for _, binding := range f.Bindings {
		if err := binding.Validate(); err != nil {
			return err
		}
		if _, duplicate := roles[binding.Role]; duplicate {
			return fmt.Errorf("Job validation fence contains a duplicate Connection role")
		}
		roles[binding.Role] = struct{}{}
	}
	return nil
}

type MutationIdentity struct {
	ActorID        string
	Method         string
	KeyFingerprint string
	RequestDigest  string
	RequestID      string
	AuditEventID   string
	OccurredAt     time.Time
}

func (i MutationIdentity) Validate() error {
	if strings.TrimSpace(i.ActorID) == "" || len(i.ActorID) > 256 ||
		strings.TrimSpace(i.Method) == "" || len(i.Method) > 256 ||
		!mutationRevisionPattern.MatchString(i.KeyFingerprint) ||
		!mutationRevisionPattern.MatchString(i.RequestDigest) ||
		strings.TrimSpace(i.RequestID) == "" || len(i.RequestID) > 128 ||
		strings.TrimSpace(i.AuditEventID) == "" || len(i.AuditEventID) > 128 || i.OccurredAt.IsZero() {
		return fmt.Errorf("Job mutation identity is invalid")
	}
	return nil
}

type Mutation struct {
	Kind            MutationKind
	TenantID        string
	Key             Key
	ExpectedVersion int64
	UID             string
	Spec            *Spec
	Validation      *ValidationFence
	Identity        MutationIdentity
	AuditAttributes map[string]any
}

func (m Mutation) Validate() error {
	if _, err := uuid.Parse(m.TenantID); err != nil {
		return fmt.Errorf("Job mutation tenant ID must be a UUID")
	}
	if err := m.Key.Validate(); err != nil {
		return err
	}
	if err := m.Identity.Validate(); err != nil {
		return err
	}
	if len(m.AuditAttributes) > 32 {
		return fmt.Errorf("Job mutation audit attributes exceed supported bounds")
	}
	switch m.Kind {
	case MutationCreate:
		if m.ExpectedVersion != 0 {
			return fmt.Errorf("Job Create must not contain an expected version")
		}
		if _, err := uuid.Parse(m.UID); err != nil {
			return fmt.Errorf("Job Create UID must be a UUID")
		}
		if m.Spec == nil || m.Validation == nil {
			return fmt.Errorf("Job Create requires a spec and validation fence")
		}
	case MutationUpdate:
		if m.ExpectedVersion <= 0 || m.Spec == nil || m.Validation == nil {
			return fmt.Errorf("Job Update requires a version, spec, and validation fence")
		}
	case MutationStart:
		if m.ExpectedVersion <= 0 || m.Spec != nil || m.Validation == nil {
			return fmt.Errorf("Job Start requires a version and validation fence")
		}
	case MutationStop, MutationDelete:
		if m.ExpectedVersion <= 0 || m.Spec != nil || m.Validation != nil {
			return fmt.Errorf("Job Stop/Delete mutation shape is invalid")
		}
	default:
		return fmt.Errorf("unsupported Job mutation %q", m.Kind)
	}
	if m.Spec != nil {
		if err := m.Spec.Validate(); err != nil {
			return err
		}
	}
	if m.Validation != nil {
		if err := m.Validation.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type MutationOutcome string

const (
	MutationOutcomeChanged  MutationOutcome = "CHANGED"
	MutationOutcomeNoChange MutationOutcome = "NO_CHANGE"
	MutationOutcomeReplayed MutationOutcome = "REPLAYED"
)

type Tombstone struct {
	TenantID     string
	Key          Key
	UID          string
	FinalVersion int64
	DeletedAt    time.Time
}

type MutationResult struct {
	Job       *Job
	Tombstone *Tombstone
	Outcome   MutationOutcome
}

// MutationRepository owns the atomic public mutation boundary. The legacy
// Repository methods remain available to the lifecycle controller.
type MutationRepository interface {
	Repository
	ReplayMutation(context.Context, Mutation) (MutationResult, bool, error)
	ApplyMutation(context.Context, Mutation) (MutationResult, error)
}
