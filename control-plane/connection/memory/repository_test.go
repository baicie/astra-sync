package memory_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"io.astrasync/control-plane/connection"
	"io.astrasync/control-plane/connection/memory"
)

func TestConnectionTestLeaseFencesCompletionAndRetainsCapturedGeneration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 8, 8, 1, 2, 3, 0, time.UTC)
	repository, stored, operation := seedConnectionTest(t, now)

	updated, changed, err := stored.Replace(
		stored.DisplayName, stored.Description,
		[]connection.Setting{{Key: "hostname", Value: "new.internal", Sensitivity: connection.SensitivityRestricted}},
		digest("descriptor-v2"), digest("schema-v2"), now.Add(time.Minute),
	)
	if err != nil || !changed {
		t.Fatalf("replace Connection generation: changed=%v err=%v", changed, err)
	}
	apply(t, repository, connection.Mutation{
		Kind: connection.MutationUpdate, TenantID: stored.TenantID, Name: stored.Name,
		ExpectedVersion: stored.Version, Candidate: &updated,
		Identity: identity("rotate-generation", now.Add(time.Minute)),
	})

	claimed, err := repository.ClaimTests(ctx, "executor-a", 1, 30*time.Second, now.Add(2*time.Minute))
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim test: count=%d err=%v", len(claimed), err)
	}
	work := claimed[0]
	if work.Operation.OperationID != operation.OperationID || work.Attempt != 1 ||
		work.Generation.Generation.Number != 1 || len(work.Generation.Generation.Settings) != 1 ||
		work.Generation.Generation.Settings[0].Value != "old.internal" {
		t.Fatalf("claim did not retain captured generation: %+v", work)
	}

	blocked, err := repository.ClaimTests(ctx, "executor-b", 1, 30*time.Second, now.Add(2*time.Minute+29*time.Second))
	if err != nil || len(blocked) != 0 {
		t.Fatalf("active lease was not exclusive: count=%d err=%v", len(blocked), err)
	}
	reclaimed, err := repository.ClaimTests(ctx, "executor-b", 1, 30*time.Second, now.Add(2*time.Minute+31*time.Second))
	if err != nil || len(reclaimed) != 1 || reclaimed[0].Attempt != 2 ||
		reclaimed[0].Operation.StartedAt == nil || !reclaimed[0].Operation.StartedAt.Equal(*work.Operation.StartedAt) {
		t.Fatalf("reclaim test: work=%+v err=%v", reclaimed, err)
	}

	completion := connection.TestCompletion{
		State: connection.TestSucceeded, Phase: connection.TestPhaseComplete,
		ResultCode: connection.TestResultOK, Success: true,
	}
	if _, err := repository.CompleteTest(
		ctx, operation.OperationID, "executor-a", completion, now.Add(2*time.Minute+32*time.Second),
	); !errors.Is(err, connection.ErrTestLeaseLost) {
		t.Fatalf("stale executor completion was accepted: %v", err)
	}
	completed, err := repository.CompleteTest(
		ctx, operation.OperationID, "executor-b", completion, now.Add(2*time.Minute+32*time.Second),
	)
	if err != nil || completed.State != connection.TestSucceeded || !completed.Success {
		t.Fatalf("complete reclaimed test: result=%+v err=%v", completed, err)
	}

	audits := repository.AuditEvents()
	lastAudit := audits[len(audits)-1]
	if lastAudit.Action != "TEST_COMPLETE" || lastAudit.ActorID != "service:executor-b" ||
		lastAudit.Attributes["resultCode"] != string(connection.TestResultOK) {
		t.Fatalf("unexpected completion audit: %+v", lastAudit)
	}

	expiredCount, err := repository.ExpireTests(ctx, operation.ExpiresAt.Add(time.Second))
	if err != nil || expiredCount != 1 {
		t.Fatalf("expire terminal test: count=%d err=%v", expiredCount, err)
	}
	expired, err := repository.GetTest(ctx, stored.TenantID, operation.OperationID)
	if err != nil || expired.State != connection.TestExpired || expired.Phase != "" ||
		expired.ResultCode != "" || expired.Success || expired.RemediationKey != "" {
		t.Fatalf("expired details were not scrubbed: result=%+v err=%v", expired, err)
	}
	if count, err := repository.ExpireTests(ctx, operation.ExpiresAt.Add(2*time.Second)); err != nil || count != 0 {
		t.Fatalf("expiry was not idempotent: count=%d err=%v", count, err)
	}
}

func TestConnectionTestClaimIsExclusiveAcrossConcurrentExecutors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 8, 8, 2, 3, 4, 0, time.UTC)
	repository, _, _ := seedConnectionTest(t, now)

	const executorCount = 16
	start := make(chan struct{})
	results := make(chan int, executorCount)
	errorsFound := make(chan error, executorCount)
	var group sync.WaitGroup
	for index := 0; index < executorCount; index++ {
		group.Add(1)
		go func(executor int) {
			defer group.Done()
			<-start
			claimed, err := repository.ClaimTests(
				ctx, fmt.Sprintf("executor-%d", executor), 1, time.Minute, now.Add(time.Second),
			)
			if err != nil {
				errorsFound <- err
				return
			}
			results <- len(claimed)
		}(index)
	}
	close(start)
	group.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("concurrent claim: %v", err)
	}
	claimedCount := 0
	for count := range results {
		claimedCount += count
	}
	if claimedCount != 1 {
		t.Fatalf("expected exactly one claim, got %d", claimedCount)
	}
}

func TestConnectionTestLeaseInputBounds(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 8, 3, 4, 5, 0, time.UTC)
	repository, _, operation := seedConnectionTest(t, now)
	tooLongID := strings.Repeat("x", connection.MaximumTestExecutorID+1)
	if _, err := repository.ClaimTests(
		context.Background(), tooLongID, 1, time.Minute, now.Add(time.Second),
	); err == nil {
		t.Fatal("expected overlong executor ID to be rejected")
	}
	if _, err := repository.ClaimTests(
		context.Background(), "executor", 1, connection.MaximumTestLease+time.Second, now.Add(time.Second),
	); err == nil {
		t.Fatal("expected overlong lease to be rejected")
	}
	if _, err := repository.CompleteTest(
		context.Background(), operation.OperationID, "executor", connection.TestCompletion{
			State: connection.TestExpired,
		}, now.Add(time.Second),
	); err == nil {
		t.Fatal("executor must not be able to complete a test as EXPIRED")
	}
}

func TestConnectionTestAdmissionAndDeadlineAreEnforced(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 8, 8, 4, 5, 6, 0, time.UTC)
	repository, stored, operation := seedConnectionTest(t, now)
	second := operation
	second.OperationID = uuid.NewString()
	second.CreatedAt = operation.CreatedAt.Add(time.Second)
	second.DeadlineAt = operation.DeadlineAt.Add(time.Second)
	second.ExpiresAt = operation.ExpiresAt.Add(time.Second)
	if _, err := repository.Apply(ctx, connection.Mutation{
		Kind: connection.MutationTest, TenantID: stored.TenantID, Name: stored.Name,
		ExpectedVersion: stored.Version, Test: &second,
		Identity: identity("second-active-test", second.CreatedAt),
	}); !errors.Is(err, connection.ErrTestLimitExceeded) {
		t.Fatalf("expected per-Connection active test limit, got %v", err)
	}

	count, err := repository.ExpireTests(ctx, operation.DeadlineAt.Add(time.Second))
	if err != nil || count != 1 {
		t.Fatalf("reconcile test deadline: count=%d err=%v", count, err)
	}
	timedOut, err := repository.GetTest(ctx, stored.TenantID, operation.OperationID)
	if err != nil || timedOut.State != connection.TestTimedOut ||
		timedOut.ResultCode != connection.TestResultDeadlineExceeded ||
		timedOut.RemediationKey != "connection.test.deadline" {
		t.Fatalf("deadline did not produce stable timeout: result=%+v err=%v", timedOut, err)
	}
	audits := repository.AuditEvents()
	lastAudit := audits[len(audits)-1]
	if lastAudit.Action != "TEST_TIMEOUT" || lastAudit.ActorID != "service:connection-test-reconciler" {
		t.Fatalf("unexpected deadline audit: %+v", lastAudit)
	}
}

func seedConnectionTest(
	t *testing.T, now time.Time,
) (*memory.Repository, connection.Connection, connection.TestOperation) {
	t.Helper()
	repository := memory.New()
	tenantID := uuid.NewString()
	stored, err := connection.New(
		tenantID, "orders-db", uuid.NewString(), "jdbc", "Orders", "test",
		connection.Generation{
			Number: 1, DescriptorRevision: digest("descriptor-v1"),
			ConnectionSchemaRevision: digest("schema-v1"),
			Settings: []connection.Setting{{
				Key: "hostname", Value: "old.internal", Sensitivity: connection.SensitivityRestricted,
			}},
			CreatedAt: now,
		}, now,
	)
	if err != nil {
		t.Fatalf("construct Connection: %v", err)
	}
	apply(t, repository, connection.Mutation{
		Kind: connection.MutationCreate, TenantID: tenantID, Name: stored.Name,
		Candidate: &stored, Identity: identity("create", now),
	})
	operation := connection.TestOperation{
		TenantID: tenantID, OperationID: uuid.NewString(), ConnectionUID: stored.UID,
		Generation: 1, DescriptorRevision: stored.Current.DescriptorRevision,
		ActorID: "principal-test", EgressPolicy: connection.DefaultTestEgressPolicy(),
		State: connection.TestQueued, CreatedAt: now.Add(time.Second),
		DeadlineAt: now.Add(10 * time.Minute), ExpiresAt: now.Add(24 * time.Hour),
	}
	apply(t, repository, connection.Mutation{
		Kind: connection.MutationTest, TenantID: tenantID, Name: stored.Name,
		ExpectedVersion: stored.Version, Test: &operation,
		Identity: identity("test", now.Add(time.Second)),
	})
	return repository, stored, operation
}

func apply(t *testing.T, repository *memory.Repository, mutation connection.Mutation) connection.MutationResult {
	t.Helper()
	result, err := repository.Apply(context.Background(), mutation)
	if err != nil {
		t.Fatalf("apply %s: %v", mutation.Kind, err)
	}
	return result
}

func identity(label string, now time.Time) connection.MutationIdentity {
	return connection.MutationIdentity{
		ActorID: "principal-test", Method: "/test/" + label,
		KeyFingerprint: digest("key-" + label), RequestDigest: digest("request-" + label),
		RequestID: "request-" + label, AuditEventID: uuid.NewString(), OccurredAt: now,
	}
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("sha256:%x", sum)
}
