package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/api-server/internal/authn"
	"io.astrasync/control-plane/api-server/internal/service"
	"io.astrasync/control-plane/auth"
	"io.astrasync/control-plane/job"
	jobmemory "io.astrasync/control-plane/job/memory"
)

type recordingJobMutationRepository struct {
	job.Repository
	mutation    job.Mutation
	replay      job.MutationResult
	replayFound bool
	applyCalls  int
}

func (r *recordingJobMutationRepository) ReplayMutation(
	_ context.Context, mutation job.Mutation,
) (job.MutationResult, bool, error) {
	r.mutation = mutation
	return r.replay, r.replayFound, nil
}

func (r *recordingJobMutationRepository) ApplyMutation(
	_ context.Context, mutation job.Mutation,
) (job.MutationResult, error) {
	r.mutation = mutation
	r.applyCalls++
	created, err := job.New(mutation.Key, mutation.UID, *mutation.Spec, mutation.Identity.OccurredAt)
	if err != nil {
		return job.MutationResult{}, err
	}
	return job.MutationResult{Job: &created, Outcome: job.MutationOutcomeChanged}, nil
}

type recordingJobMutationValidator struct {
	calls   int
	request *controlv1.ValidateJobSpecRequest
	result  service.MutationValidation
}

func (v *recordingJobMutationValidator) ValidateForMutation(
	_ context.Context, request *controlv1.ValidateJobSpecRequest,
) (service.MutationValidation, error) {
	v.calls++
	v.request = request
	return v.result, nil
}

func TestTransactionalJobCreatePersistsCanonicalFence(t *testing.T) {
	now := time.Date(2026, 8, 8, 16, 0, 0, 0, time.UTC)
	tenantID := uuid.NewString()
	repository := &recordingJobMutationRepository{Repository: jobmemory.New()}
	validator := &recordingJobMutationValidator{result: validMutationValidation(tenantID)}
	jobService, err := service.NewTransactionalJobService(
		repository, validator, auth.ContextAuthorizer{},
		[]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return now },
		func() string { return "12345678-1234-4234-8234-123456789abc" },
	)
	if err != nil {
		t.Fatalf("create transactional Job service: %v", err)
	}
	ctx := jobMutationContext(t, tenantID, auth.PermissionJobsCreate)
	created, err := jobService.CreateJob(ctx, &controlv1.CreateJobRequest{
		Namespace: "tenant-a", Name: "orders", Spec: csvSpec("input.csv", "output.csv"),
		IdempotencyKey: fixtureIdempotencyKey("create-orders-000001"),
	})
	if err != nil || created.GetUid() == "" {
		t.Fatalf("create Job: result=%+v err=%v", created, err)
	}
	mutation := repository.mutation
	if repository.applyCalls != 1 || validator.calls != 1 || mutation.Kind != job.MutationCreate ||
		mutation.TenantID != tenantID || mutation.Validation == nil ||
		mutation.Validation.CompilerRevision != validator.result.Result.GetCompilerRevision() ||
		mutation.Identity.KeyFingerprint == "create-orders-000001" || mutation.Spec == nil {
		t.Fatalf("canonical mutation was incomplete: %+v", mutation)
	}
}

func TestTransactionalJobIdempotencyReplaySkipsCanonicalCompiler(t *testing.T) {
	now := time.Date(2026, 8, 8, 16, 0, 0, 0, time.UTC)
	tenantID := uuid.NewString()
	stored, err := job.New(
		job.Key{Namespace: "tenant-a", Name: "orders"}, uuid.NewString(),
		job.Spec{
			Source:   job.ConnectorSpec{Connector: "csv", Options: map[string]string{"path": "input.csv"}},
			Sink:     job.ConnectorSpec{Connector: "csv", Options: map[string]string{"path": "output.csv"}},
			Delivery: job.DeliverySpec{Guarantee: job.DeliveryAtMostOnce},
			Runtime:  job.RuntimeSpec{MaxBatchRecords: 100},
		}, now,
	)
	if err != nil {
		t.Fatalf("construct replay Job: %v", err)
	}
	repository := &recordingJobMutationRepository{
		Repository: jobmemory.New(), replayFound: true,
		replay: job.MutationResult{Job: &stored, Outcome: job.MutationOutcomeReplayed},
	}
	validator := &recordingJobMutationValidator{result: validMutationValidation(tenantID)}
	jobService, err := service.NewTransactionalJobService(
		repository, validator, auth.ContextAuthorizer{},
		[]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return now }, uuid.NewString,
	)
	if err != nil {
		t.Fatalf("create transactional Job service: %v", err)
	}
	result, err := jobService.CreateJob(
		jobMutationContext(t, tenantID, auth.PermissionJobsCreate),
		&controlv1.CreateJobRequest{
			Namespace: "tenant-a", Name: "orders", Spec: csvSpec("input.csv", "output.csv"),
			IdempotencyKey: fixtureIdempotencyKey("create-orders-000001"),
		},
	)
	if err != nil || result.GetUid() != stored.UID || validator.calls != 0 || repository.applyCalls != 0 {
		t.Fatalf("idempotency replay was not side-effect free: result=%+v err=%v calls=%d/%d",
			result, err, validator.calls, repository.applyCalls)
	}
}

func validMutationValidation(tenantID string) service.MutationValidation {
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	return service.MutationValidation{
		Result: &controlv1.JobValidationResult{
			ValidationId: "validation-1", Valid: true, SpecDigest: digest,
			CompilerRevision: digest,
			ExecutionMode:    controlv1.ConnectorExecutionMode_CONNECTOR_EXECUTION_MODE_BATCH,
		},
		Fence: job.ValidationFence{
			ValidationID: "validation-1", SpecDigest: digest, CompilerRevision: digest,
			Bindings: []job.ConnectionBinding{},
		},
	}
}

func jobMutationContext(t *testing.T, tenantID string, permissions ...auth.Permission) context.Context {
	t.Helper()
	ctx, err := auth.WithPrincipal(context.Background(), auth.Principal{
		ID: "job-operator", Subject: "job-operator", Active: true, PolicyRevision: "policy-1",
		Memberships: map[string]auth.Membership{tenantID: mustMembershipWithPerms(t, tenantID, true, permissions...)},
	})
	if err != nil {
		t.Fatalf("create Job mutation principal: %v", err)
	}
	return ctx
}

func mustMembership(t *testing.T, tenantID string, active bool) auth.Membership {
	t.Helper()
	membership, err := auth.NewMembership(tenantID, active)
	if err != nil {
		t.Fatalf("create membership without permissions: %v", err)
	}
	return membership
}

func mustMembershipWithPerms(
	t *testing.T, tenantID string, active bool, permissions ...auth.Permission,
) auth.Membership {
	t.Helper()
	membership, err := auth.NewMembership(tenantID, active, permissions...)
	if err != nil {
		t.Fatalf("create membership with permissions: %v", err)
	}
	membership.TenantNamespace = "tenant-a"
	return membership
}

// TestTransactionalJobCreatePrefersAttachedTenantID covers Phase 29
// (ADR-074 §4): when the interceptor has attached a verified tenant-id via
// `authn.WithJobTenantID`, the JobService mutation MUST use that value as
// `Mutation.TenantID`, even if the principal's membership for the
// namespace resolves to a different tenant-id. The trust boundary sits
// in the interceptor; JobService is just a consumer.
//
// The principal in this test has memberships for BOTH the membership-derived
// tenant and the attached tenant. The attached tenant carries the
// permission; the membership-derived tenant does not. This mirrors the
// real flow where the BFF has already verified the principal owns the
// attached tenant's permissions, and the membership-for-scope lookup is a
// convenience fallback for the lifecycle controller's direct calls.
func TestTransactionalJobCreatePrefersAttachedTenantID(t *testing.T) {
	now := time.Date(2026, 9, 8, 16, 0, 0, 0, time.UTC)
	membershipTenantID := uuid.NewString()
	attachedTenantID := uuid.NewString()
	if membershipTenantID == attachedTenantID {
		t.Fatalf("attached and membership tenant IDs must differ: %q", membershipTenantID)
	}
	repository := &recordingJobMutationRepository{Repository: jobmemory.New()}
	validator := &recordingJobMutationValidator{result: validMutationValidation(attachedTenantID)}
	jobService, err := service.NewTransactionalJobService(
		repository, validator, auth.ContextAuthorizer{},
		[]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return now },
		func() string { return "12345678-1234-4234-8234-123456789abc" },
	)
	if err != nil {
		t.Fatalf("create transactional Job service: %v", err)
	}
	attachedMembership, err := auth.NewMembership(attachedTenantID, true, auth.PermissionJobsCreate)
	if err != nil {
		t.Fatalf("attached membership: %v", err)
	}
	attachedMembership.TenantNamespace = "tenant-a"
	// Build a principal whose active memberships span both tenants. The
	// membership for the *attached* tenant carries the permission; the
	// membership for the membership-derived tenant does not.
	ctx, err := auth.WithPrincipal(context.Background(), auth.Principal{
		ID: "job-operator", Subject: "job-operator", Active: true, PolicyRevision: "policy-1",
		Memberships: map[string]auth.Membership{
			attachedTenantID:      attachedMembership,
			membershipTenantID: mustMembership(t, membershipTenantID, true),
		},
	})
	if err != nil {
		t.Fatalf("create principal: %v", err)
	}
	ctx = authn.WithJobTenantID(ctx, attachedTenantID)
	_, err = jobService.CreateJob(ctx, &controlv1.CreateJobRequest{
		Namespace: "tenant-a", Name: "orders", Spec: csvSpec("input.csv", "output.csv"),
		IdempotencyKey: fixtureIdempotencyKey("phase29-attached-000001"),
	})
	if err != nil {
		t.Fatalf("create Job: %v", err)
	}
	if repository.mutation.TenantID != attachedTenantID {
		t.Fatalf("mutation TenantID = %q, want attached %q (membership was %q)",
			repository.mutation.TenantID, attachedTenantID, membershipTenantID)
	}
}

// TestTransactionalJobCreateFallsBackToMembershipWhenNoAttachedTenantID
// covers the Phase 29 fallback path (ADR-074 §4): when the interceptor
// has not run (or no metadata was provided), JobService MUST derive
// Mutation.TenantID from the principal's active membership. The legacy
// behaviour is preserved.
func TestTransactionalJobCreateFallsBackToMembershipWhenNoAttachedTenantID(t *testing.T) {
	now := time.Date(2026, 9, 8, 16, 0, 0, 0, time.UTC)
	tenantID := uuid.NewString()
	repository := &recordingJobMutationRepository{Repository: jobmemory.New()}
	validator := &recordingJobMutationValidator{result: validMutationValidation(tenantID)}
	jobService, err := service.NewTransactionalJobService(
		repository, validator, auth.ContextAuthorizer{},
		[]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return now },
		func() string { return "12345678-1234-4234-8234-123456789abc" },
	)
	if err != nil {
		t.Fatalf("create transactional Job service: %v", err)
	}
	_, err = jobService.CreateJob(
		jobMutationContext(t, tenantID, auth.PermissionJobsCreate),
		&controlv1.CreateJobRequest{
			Namespace: "tenant-a", Name: "orders", Spec: csvSpec("input.csv", "output.csv"),
			IdempotencyKey: fixtureIdempotencyKey("phase29-fallback-000001"),
		},
	)
	if err != nil {
		t.Fatalf("create Job: %v", err)
	}
	if repository.mutation.TenantID != tenantID {
		t.Fatalf("fallback TenantID = %q, want membership-derived %q",
			repository.mutation.TenantID, tenantID)
	}
}
