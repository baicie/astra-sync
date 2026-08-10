package memory

import (
	"bytes"
	"context"
	"sort"
	"sync"

	"io.astrasync/control-plane/catalog"
)

type Repository struct {
	mu          sync.RWMutex
	snapshots   map[string]catalog.Snapshot
	active      map[string]string
	auditEvents []catalog.AuditEvent
}

func New() *Repository {
	return &Repository{
		snapshots: make(map[string]catalog.Snapshot),
		active:    make(map[string]string),
	}
}

func (r *Repository) Activate(
	_ context.Context, candidate catalog.Snapshot, event catalog.AuditEvent,
) (bool, error) {
	if err := candidate.Validate(); err != nil {
		return false, err
	}
	if err := event.Validate(); err != nil {
		return false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, found := r.snapshots[candidate.InventoryRevision]; found && !bytes.Equal(existing.Payload, candidate.Payload) {
		return false, catalog.ErrRevisionCollision
	}
	for _, descriptor := range candidate.Descriptors {
		for _, existing := range r.snapshots {
			for _, storedDescriptor := range existing.Descriptors {
				sameRevision := storedDescriptor.Revision == descriptor.Revision
				sameArtifact := storedDescriptor.Name == descriptor.Name &&
					storedDescriptor.ArtifactVersion == descriptor.ArtifactVersion
				if sameRevision && !bytes.Equal(storedDescriptor.Payload, descriptor.Payload) ||
					sameArtifact && storedDescriptor.Revision != descriptor.Revision {
					return false, catalog.ErrRevisionCollision
				}
			}
		}
	}
	changed := r.active[candidate.ExecutionProfile] != candidate.InventoryRevision
	r.snapshots[candidate.InventoryRevision] = candidate.Clone()
	r.active[candidate.ExecutionProfile] = candidate.InventoryRevision
	r.auditEvents = append(r.auditEvents, event)
	return changed, nil
}

func (r *Repository) Current(_ context.Context, executionProfile string) (catalog.Snapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	revision, found := r.active[executionProfile]
	if !found {
		return catalog.Snapshot{}, catalog.ErrNotFound
	}
	return r.snapshots[revision].Clone(), nil
}

func (r *Repository) ListRecent(
	_ context.Context, executionProfile string, limit int,
) ([]catalog.Snapshot, error) {
	if limit <= 0 || limit > 100 {
		return nil, catalog.ErrNotFound
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]catalog.Snapshot, 0)
	for _, snapshot := range r.snapshots {
		if snapshot.ExecutionProfile == executionProfile {
			result = append(result, snapshot.Clone())
		}
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].ActivatedAt.After(result[right].ActivatedAt)
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (r *Repository) AuditEvents() []catalog.AuditEvent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]catalog.AuditEvent(nil), r.auditEvents...)
}
