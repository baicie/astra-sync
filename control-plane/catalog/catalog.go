// Package catalog owns immutable deployment connector inventories and atomic activation.
package catalog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	ErrNotFound          = errors.New("connector catalog not found")
	ErrRevisionCollision = errors.New("connector catalog revision collision")
)

var revisionPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type DescriptorSnapshot struct {
	Revision        string
	Name            string
	ArtifactVersion string
	Payload         []byte
}

type Snapshot struct {
	InventoryRevision string
	CompilerRevision  string
	ExecutionProfile  string
	Payload           []byte
	Descriptors       []DescriptorSnapshot
	ActivatedAt       time.Time
}

func (s Snapshot) Validate() error {
	if !revisionPattern.MatchString(s.InventoryRevision) || !revisionPattern.MatchString(s.CompilerRevision) {
		return fmt.Errorf("catalog revisions must be lowercase SHA-256 identifiers")
	}
	if strings.TrimSpace(s.ExecutionProfile) == "" || len(s.ExecutionProfile) > 128 {
		return fmt.Errorf("execution profile must contain between 1 and 128 characters")
	}
	if len(s.Payload) == 0 || len(s.Payload) > 4*1024*1024 {
		return fmt.Errorf("catalog payload must contain between 1 byte and 4 MiB")
	}
	if len(s.Descriptors) == 0 || len(s.Descriptors) > 256 {
		return fmt.Errorf("catalog must contain between 1 and 256 descriptors")
	}
	previousName := ""
	seenRevisions := make(map[string]struct{}, len(s.Descriptors))
	for _, descriptor := range s.Descriptors {
		if !revisionPattern.MatchString(descriptor.Revision) ||
			strings.TrimSpace(descriptor.Name) == "" || len(descriptor.Name) > 128 ||
			strings.TrimSpace(descriptor.ArtifactVersion) == "" || len(descriptor.ArtifactVersion) > 128 ||
			len(descriptor.Payload) == 0 || len(descriptor.Payload) > 512*1024 {
			return fmt.Errorf("catalog contains an invalid descriptor snapshot")
		}
		if descriptor.Name <= previousName {
			return fmt.Errorf("catalog descriptors must use unique canonical name ordering")
		}
		if _, duplicate := seenRevisions[descriptor.Revision]; duplicate {
			return fmt.Errorf("catalog contains duplicate descriptor revisions")
		}
		seenRevisions[descriptor.Revision] = struct{}{}
		previousName = descriptor.Name
	}
	if s.ActivatedAt.IsZero() {
		return fmt.Errorf("catalog activation timestamp must be set")
	}
	return nil
}

func (s Snapshot) Clone() Snapshot {
	result := s
	result.Payload = bytes.Clone(s.Payload)
	result.Descriptors = make([]DescriptorSnapshot, len(s.Descriptors))
	for index, descriptor := range s.Descriptors {
		result.Descriptors[index] = descriptor
		result.Descriptors[index].Payload = bytes.Clone(descriptor.Payload)
	}
	return result
}

type AuditEvent struct {
	EventID         string
	ActorID         string
	RequestID       string
	OldRevision     string
	NewRevision     string
	DescriptorCount int
	Outcome         string
	OccurredAt      time.Time
}

func (e AuditEvent) Validate() error {
	if strings.TrimSpace(e.EventID) == "" || strings.TrimSpace(e.ActorID) == "" ||
		strings.TrimSpace(e.RequestID) == "" || !revisionPattern.MatchString(e.NewRevision) ||
		e.DescriptorCount <= 0 || (e.Outcome != "CHANGED" && e.Outcome != "NO_CHANGE") ||
		e.OccurredAt.IsZero() {
		return fmt.Errorf("catalog activation audit event is invalid")
	}
	if e.OldRevision != "" && !revisionPattern.MatchString(e.OldRevision) {
		return fmt.Errorf("catalog activation old revision is invalid")
	}
	return nil
}

type Repository interface {
	Activate(context.Context, Snapshot, AuditEvent) (bool, error)
	Current(context.Context, string) (Snapshot, error)
	ListRecent(context.Context, string, int) ([]Snapshot, error)
}

type Validator interface {
	Validate([]byte, time.Time) (Snapshot, error)
}

type Reconciler struct {
	repository Repository
	validator  Validator
	clock      func() time.Time
	eventID    func() string
}

func NewReconciler(repository Repository, validator Validator, clock func() time.Time, eventID func() string) (*Reconciler, error) {
	if repository == nil || validator == nil || clock == nil || eventID == nil {
		return nil, fmt.Errorf("catalog reconciler dependencies must not be nil")
	}
	return &Reconciler{repository: repository, validator: validator, clock: clock, eventID: eventID}, nil
}

func (r *Reconciler) Reconcile(
	ctx context.Context, payload []byte, actorID, requestID string,
) (Snapshot, bool, error) {
	now := r.clock().UTC()
	candidate, err := r.validator.Validate(bytes.Clone(payload), now)
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("validate connector inventory: %w", err)
	}
	oldRevision := ""
	current, currentErr := r.repository.Current(ctx, candidate.ExecutionProfile)
	if currentErr == nil {
		oldRevision = current.InventoryRevision
	} else if !errors.Is(currentErr, ErrNotFound) {
		return Snapshot{}, false, fmt.Errorf("read active connector inventory: %w", currentErr)
	}
	outcome := "CHANGED"
	if oldRevision == candidate.InventoryRevision {
		outcome = "NO_CHANGE"
	}
	event := AuditEvent{
		EventID:         r.eventID(),
		ActorID:         actorID,
		RequestID:       requestID,
		OldRevision:     oldRevision,
		NewRevision:     candidate.InventoryRevision,
		DescriptorCount: len(candidate.Descriptors),
		Outcome:         outcome,
		OccurredAt:      now,
	}
	changed, err := r.repository.Activate(ctx, candidate, event)
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("activate connector inventory: %w", err)
	}
	return candidate.Clone(), changed, nil
}

func SortDescriptors(values []DescriptorSnapshot) []DescriptorSnapshot {
	result := append([]DescriptorSnapshot(nil), values...)
	sort.Slice(result, func(left, right int) bool { return result[left].Name < result[right].Name })
	return result
}
