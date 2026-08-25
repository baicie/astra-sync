//go:build integration

package adapters_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"

	_ "github.com/jackc/pgx/v5/stdlib"

	replication "io.astrasync/control-plane/replication"
	adapters "io.astrasync/control-plane/replication/adapters"
	"io.astrasync/control-plane/replication/promotion"
)

func TestPostgreSQLStorePersistsEpochFenceAndPromotionLifecycle(t *testing.T) {
	dataSourceName := os.Getenv("ASTRASYNC_TEST_POSTGRES_URL")
	if dataSourceName == "" {
		t.Skip("ASTRASYNC_TEST_POSTGRES_URL is not configured")
	}
	ctx := context.Background()
	database, err := sql.Open("pgx", dataSourceName)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.PingContext(ctx); err != nil {
		t.Fatalf("ping PostgreSQL: %v", err)
	}
	store, err := adapters.NewPostgreSQLStore(database)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate replication metadata: %v", err)
	}

	jobID := "integration-job-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = database.ExecContext(ctx, `DELETE FROM astrasync_replication_checkpoint_admissions WHERE job_id = $1`, jobID)
		_, _ = database.ExecContext(ctx, `DELETE FROM astrasync_replication_promotions WHERE job_id = $1`, jobID)
		_, _ = database.ExecContext(ctx, `DELETE FROM astrasync_replication_job_epochs WHERE job_id = $1`, jobID)
	})
	firstEpoch, err := store.Assign(ctx, jobID)
	if err != nil || firstEpoch != 1 {
		t.Fatalf("first epoch = %d, err=%v; want 1", firstEpoch, err)
	}
	secondEpoch, err := store.Assign(ctx, jobID)
	if err != nil || secondEpoch != 2 {
		t.Fatalf("second epoch = %d, err=%v; want 2", secondEpoch, err)
	}
	if err := store.Fence(ctx, jobID, secondEpoch); err != nil {
		t.Fatalf("current epoch fence: %v", err)
	}
	if err := store.Fence(ctx, jobID, firstEpoch); !errors.Is(err, promotion.ErrEpochConflict) {
		t.Fatalf("stale epoch fence = %v, want ErrEpochConflict", err)
	}
	if err := store.UpdateEpoch(ctx, jobID, firstEpoch, 3); !errors.Is(err, promotion.ErrEpochConflict) {
		t.Fatalf("stale epoch update = %v, want ErrEpochConflict", err)
	}
	if err := store.UpdateEpoch(ctx, jobID, secondEpoch, 3); err != nil {
		t.Fatalf("current epoch update: %v", err)
	}
	currentEpoch, err := store.GetEpoch(ctx, jobID)
	if err != nil || currentEpoch != 3 {
		t.Fatalf("current epoch = %d, err=%v; want 3", currentEpoch, err)
	}

	promotionRecord := promotion.NewPromotion(jobID, "us-east-1", "eu-west-1", "integration-idempotency-key", 2)
	promotionRecord.SetEpoch(3)
	if err := store.Create(ctx, promotionRecord); err != nil {
		t.Fatalf("create promotion: %v", err)
	}
	if err := store.Create(ctx, promotionRecord); err == nil {
		t.Fatal("duplicate promotion create succeeded")
	}
	loaded, err := store.Get(ctx, jobID, promotionRecord.IdempotencyKey)
	if err != nil || loaded == nil || loaded.NewEpoch != 3 || loaded.State != promotion.StatePending {
		t.Fatalf("loaded promotion = %+v, err=%v", loaded, err)
	}
	if err := promotionRecord.TransitionTo(promotion.StateEpochBumped, ""); err != nil {
		t.Fatalf("transition promotion: %v", err)
	}
	if err := store.Update(ctx, promotionRecord); err != nil {
		t.Fatalf("update promotion: %v", err)
	}
	latest, err := store.GetLatest(ctx, jobID)
	if err != nil || latest == nil || latest.State != promotion.StateEpochBumped {
		t.Fatalf("latest promotion = %+v, err=%v", latest, err)
	}

	accepted, err := store.ClaimCheckpoint(ctx, "us-east-1", "eu-west-1", jobID, 7, 3, "objects/checkpoint-7", 42)
	if err != nil || !accepted {
		t.Fatalf("first checkpoint claim = %v, err=%v; want accepted", accepted, err)
	}
	if err := store.CommitCheckpoint(ctx, "us-east-1", "eu-west-1", jobID, 7); err != nil {
		t.Fatalf("commit checkpoint claim: %v", err)
	}
	accepted, err = store.ClaimCheckpoint(ctx, "us-east-1", "eu-west-1", jobID, 7, 3, "objects/checkpoint-7", 42)
	if err != nil || accepted {
		t.Fatalf("duplicate checkpoint claim = %v, err=%v; want acknowledged duplicate", accepted, err)
	}
	if _, err := store.ClaimCheckpoint(ctx, "us-east-1", "eu-west-1", jobID, 7, 3, "objects/other", 42); !errors.Is(err, replication.ErrCheckpointAdmissionConflict) {
		t.Fatalf("conflicting checkpoint claim = %v, want ErrCheckpointAdmissionConflict", err)
	}
}
