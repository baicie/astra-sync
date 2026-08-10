package postgres_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"io.astrasync/control-plane/catalog"
	catalogpostgres "io.astrasync/control-plane/catalog/postgres"
)

func TestRepositoryAtomicallyActivatesCatalog(t *testing.T) {
	dataSourceName := os.Getenv("ASTRASYNC_TEST_POSTGRES_URL")
	if dataSourceName == "" {
		t.Skip("ASTRASYNC_TEST_POSTGRES_URL is not configured")
	}
	ctx := context.Background()
	repository, err := catalogpostgres.Open(ctx, dataSourceName)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repository.Close()
	if err := repository.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	suffix := uuid.NewString()
	profile := "catalog-it-" + suffix
	first := integrationSnapshot(profile, "first", suffix, time.Now().UTC())
	firstEvent := integrationAudit("event-"+suffix, first.InventoryRevision, "CHANGED", first.ActivatedAt)
	changed, err := repository.Activate(ctx, first, firstEvent)
	if err != nil || !changed {
		t.Fatalf("activate first inventory: changed=%v err=%v", changed, err)
	}
	changed, err = repository.Activate(
		ctx,
		first,
		integrationAudit("replay-"+suffix, first.InventoryRevision, "NO_CHANGE", first.ActivatedAt.Add(time.Second)),
	)
	if err != nil || changed {
		t.Fatalf("replay inventory: changed=%v err=%v", changed, err)
	}
	current, err := repository.Current(ctx, profile)
	if err != nil || current.InventoryRevision != first.InventoryRevision || len(current.Descriptors) != 1 {
		t.Fatalf("read active inventory: snapshot=%+v err=%v", current, err)
	}

	artifactCollision := integrationSnapshot(profile, "collision", suffix, first.ActivatedAt.Add(time.Minute))
	artifactCollision.Descriptors[0].Name = first.Descriptors[0].Name
	artifactCollision.Descriptors[0].ArtifactVersion = first.Descriptors[0].ArtifactVersion
	if _, err := repository.Activate(
		ctx,
		artifactCollision,
		integrationAudit("collision-"+suffix, artifactCollision.InventoryRevision, "CHANGED", artifactCollision.ActivatedAt),
	); !errors.Is(err, catalog.ErrRevisionCollision) {
		t.Fatalf("expected immutable artifact collision, got %v", err)
	}

	second := integrationSnapshot(profile, "second", suffix, first.ActivatedAt.Add(2*time.Minute))
	secondEvent := integrationAudit(firstEvent.EventID, second.InventoryRevision, "CHANGED", second.ActivatedAt)
	secondEvent.OldRevision = first.InventoryRevision
	if _, err := repository.Activate(ctx, second, secondEvent); err == nil {
		t.Fatal("expected duplicate audit event to roll back activation")
	}
	current, err = repository.Current(ctx, profile)
	if err != nil || current.InventoryRevision != first.InventoryRevision {
		t.Fatalf("failed audit changed active inventory: snapshot=%+v err=%v", current, err)
	}

	database, err := sql.Open("pgx", dataSourceName)
	if err != nil {
		t.Fatalf("open verification database: %v", err)
	}
	defer database.Close()
	var rolledBack int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM astrasync_connector_inventories WHERE inventory_revision = $1`,
		second.InventoryRevision,
	).Scan(&rolledBack); err != nil || rolledBack != 0 {
		t.Fatalf("failed activation was not fully rolled back: count=%d err=%v", rolledBack, err)
	}
	recent, err := repository.ListRecent(ctx, profile, 10)
	if err != nil || len(recent) != 1 || recent[0].InventoryRevision != first.InventoryRevision {
		t.Fatalf("retained inventory: snapshots=%+v err=%v", recent, err)
	}
}

func integrationSnapshot(profile, label, suffix string, activatedAt time.Time) catalog.Snapshot {
	return catalog.Snapshot{
		InventoryRevision: integrationRevision("inventory-" + label + suffix),
		CompilerRevision:  integrationRevision("compiler-" + label + suffix),
		ExecutionProfile:  profile,
		Payload:           []byte("inventory-" + label),
		Descriptors: []catalog.DescriptorSnapshot{{
			Revision:        integrationRevision("descriptor-" + label + suffix),
			Name:            fmt.Sprintf("catalog-it-%s-%s", label, suffix),
			ArtifactVersion: "1.0.0",
			Payload:         []byte("descriptor-" + label),
		}},
		ActivatedAt: activatedAt,
	}
}

func integrationAudit(eventID, revision, outcome string, occurredAt time.Time) catalog.AuditEvent {
	return catalog.AuditEvent{
		EventID:         eventID,
		ActorID:         "service:catalog-reconciler",
		RequestID:       "request-" + eventID,
		NewRevision:     revision,
		DescriptorCount: 1,
		Outcome:         outcome,
		OccurredAt:      occurredAt,
	}
}

func integrationRevision(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("sha256:%x", digest)
}
