package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"io.astrasync/control-plane/catalog"
	"io.astrasync/control-plane/catalog/memory"
)

func TestRepositoryAtomicallyActivatesImmutableSnapshotsAndAudit(t *testing.T) {
	repository := memory.New()
	first := snapshot("1", []byte("first"))
	event := audit("1", "CHANGED")

	changed, err := repository.Activate(context.Background(), first, event)
	if err != nil || !changed {
		t.Fatalf("activate: changed=%v err=%v", changed, err)
	}
	changed, err = repository.Activate(context.Background(), first, audit("1", "NO_CHANGE"))
	if err != nil || changed {
		t.Fatalf("replay activation: changed=%v err=%v", changed, err)
	}
	loaded, err := repository.Current(context.Background(), "standard")
	if err != nil || string(loaded.Payload) != "first" || len(repository.AuditEvents()) != 2 {
		t.Fatalf("current=%+v audit=%+v err=%v", loaded, repository.AuditEvents(), err)
	}

	collision := first
	collision.Payload = []byte("different")
	if _, err := repository.Activate(context.Background(), collision, event); !errors.Is(err, catalog.ErrRevisionCollision) {
		t.Fatalf("expected revision collision, got %v", err)
	}

	artifactCollision := snapshot("2", []byte("second"))
	artifactCollision.Descriptors[0].Revision = "sha256:" + repeat("c", 64)
	artifactCollision.Descriptors[0].Payload = []byte("other descriptor")
	if _, err := repository.Activate(
		context.Background(), artifactCollision, audit("2", "CHANGED"),
	); !errors.Is(err, catalog.ErrRevisionCollision) {
		t.Fatalf("expected connector artifact collision, got %v", err)
	}
}

func snapshot(suffix string, payload []byte) catalog.Snapshot {
	revision := "sha256:" + repeat(suffix, 64)
	return catalog.Snapshot{
		InventoryRevision: revision,
		CompilerRevision:  "sha256:" + repeat("a", 64),
		ExecutionProfile:  "standard",
		Payload:           payload,
		Descriptors: []catalog.DescriptorSnapshot{{
			Revision:        "sha256:" + repeat("b", 64),
			Name:            "csv",
			ArtifactVersion: "1.1.0",
			Payload:         []byte("descriptor"),
		}},
		ActivatedAt: time.Date(2026, 8, 8, 1, 0, 0, 0, time.UTC),
	}
}

func audit(suffix, outcome string) catalog.AuditEvent {
	return catalog.AuditEvent{
		EventID:         "event-" + suffix,
		ActorID:         "catalog-publisher",
		RequestID:       "request-" + suffix,
		NewRevision:     "sha256:" + repeat(suffix, 64),
		DescriptorCount: 1,
		Outcome:         outcome,
		OccurredAt:      time.Date(2026, 8, 8, 1, 0, 0, 0, time.UTC),
	}
}

func repeat(value string, count int) string {
	result := ""
	for range count {
		result += value
	}
	return result
}
