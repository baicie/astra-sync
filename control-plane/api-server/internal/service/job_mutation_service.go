package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/emptypb"

	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/auth"
	"io.astrasync/control-plane/job"
)

type JobMutationValidator interface {
	ValidateForMutation(context.Context, *controlv1.ValidateJobSpecRequest) (MutationValidation, error)
}

func (s *JobService) createJobMutation(
	ctx context.Context, request *controlv1.CreateJobRequest,
) (*controlv1.Job, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	key, err := requestKey(request.GetNamespace(), request.GetName())
	if err != nil {
		return nil, err
	}
	spec, err := fromProtoSpec(request.GetSpec())
	if err != nil {
		return nil, err
	}
	mutation, err := s.newJobMutation(
		ctx, job.MutationCreate, key, 0, request.GetIdempotencyKey(), request,
		controlv1.JobService_CreateJob_FullMethodName, auth.PermissionJobsCreate,
	)
	if err != nil {
		return nil, err
	}
	if replayed, found, err := s.replayJobMutation(ctx, mutation); err != nil || found {
		return mutationJobResponse(replayed, err)
	}
	mutation.UID = s.uidGenerator()
	mutation.Spec = &spec
	result, err := s.applyValidatedJobMutation(ctx, mutation, &controlv1.ValidateJobSpecRequest{
		Namespace: request.GetNamespace(), Name: request.GetName(),
		Purpose: controlv1.JobValidationPurpose_JOB_VALIDATION_PURPOSE_CREATE,
		Spec:    request.GetSpec(),
	})
	return mutationJobResponse(result, err)
}

func (s *JobService) updateJobMutation(
	ctx context.Context, request *controlv1.UpdateJobRequest,
) (*controlv1.Job, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	if request.GetExpectedVersion() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "expected_version must be positive")
	}
	key, err := requestKey(request.GetNamespace(), request.GetName())
	if err != nil {
		return nil, err
	}
	spec, err := fromProtoSpec(request.GetSpec())
	if err != nil {
		return nil, err
	}
	mutation, err := s.newJobMutation(
		ctx, job.MutationUpdate, key, request.GetExpectedVersion(), request.GetIdempotencyKey(), request,
		controlv1.JobService_UpdateJob_FullMethodName, auth.PermissionJobsUpdate,
	)
	if err != nil {
		return nil, err
	}
	if replayed, found, err := s.replayJobMutation(ctx, mutation); err != nil || found {
		return mutationJobResponse(replayed, err)
	}
	mutation.Spec = &spec
	result, err := s.applyValidatedJobMutation(ctx, mutation, &controlv1.ValidateJobSpecRequest{
		Namespace: request.GetNamespace(), Name: request.GetName(),
		Purpose:         controlv1.JobValidationPurpose_JOB_VALIDATION_PURPOSE_UPDATE,
		ExpectedVersion: request.GetExpectedVersion(), Spec: request.GetSpec(),
	})
	return mutationJobResponse(result, err)
}

func (s *JobService) startJobMutation(
	ctx context.Context, request *controlv1.StartJobRequest,
) (*controlv1.Job, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	if request.GetExpectedVersion() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "expected_version must be positive")
	}
	key, err := requestKey(request.GetNamespace(), request.GetName())
	if err != nil {
		return nil, err
	}
	mutation, err := s.newJobMutation(
		ctx, job.MutationStart, key, request.GetExpectedVersion(), request.GetIdempotencyKey(), request,
		controlv1.JobService_StartJob_FullMethodName, auth.PermissionJobsStart,
	)
	if err != nil {
		return nil, err
	}
	if replayed, found, err := s.replayJobMutation(ctx, mutation); err != nil || found {
		return mutationJobResponse(replayed, err)
	}
	result, err := s.applyValidatedJobMutation(ctx, mutation, &controlv1.ValidateJobSpecRequest{
		Namespace: request.GetNamespace(), Name: request.GetName(),
		Purpose:         controlv1.JobValidationPurpose_JOB_VALIDATION_PURPOSE_START,
		ExpectedVersion: request.GetExpectedVersion(),
	})
	return mutationJobResponse(result, err)
}

func (s *JobService) stopJobMutation(
	ctx context.Context, request *controlv1.StopJobRequest,
) (*controlv1.Job, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	if request.GetExpectedVersion() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "expected_version must be positive")
	}
	key, err := requestKey(request.GetNamespace(), request.GetName())
	if err != nil {
		return nil, err
	}
	mutation, err := s.newJobMutation(
		ctx, job.MutationStop, key, request.GetExpectedVersion(), request.GetIdempotencyKey(), request,
		controlv1.JobService_StopJob_FullMethodName, auth.PermissionJobsStop,
	)
	if err != nil {
		return nil, err
	}
	if replayed, found, err := s.replayJobMutation(ctx, mutation); err != nil || found {
		return mutationJobResponse(replayed, err)
	}
	result, err := s.mutations.ApplyMutation(ctx, mutation)
	return mutationJobResponse(result, err)
}

func (s *JobService) deleteJobMutation(
	ctx context.Context, request *controlv1.DeleteJobRequest,
) (*emptypb.Empty, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	if request.GetExpectedVersion() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "expected_version must be positive")
	}
	key, err := requestKey(request.GetNamespace(), request.GetName())
	if err != nil {
		return nil, err
	}
	mutation, err := s.newJobMutation(
		ctx, job.MutationDelete, key, request.GetExpectedVersion(), request.GetIdempotencyKey(), request,
		controlv1.JobService_DeleteJob_FullMethodName, auth.PermissionJobsDelete,
	)
	if err != nil {
		return nil, err
	}
	if _, found, err := s.replayJobMutation(ctx, mutation); err != nil {
		return nil, repositoryError(err)
	} else if found {
		return &emptypb.Empty{}, nil
	}
	result, err := s.mutations.ApplyMutation(ctx, mutation)
	if err != nil {
		if errors.Is(err, job.ErrInvalidTransition) {
			return nil, lifecycleError(err)
		}
		return nil, repositoryError(err)
	}
	if result.Tombstone == nil {
		return nil, status.Error(codes.Internal, "Job deletion result is incomplete")
	}
	return &emptypb.Empty{}, nil
}

func (s *JobService) applyValidatedJobMutation(
	ctx context.Context, mutation job.Mutation, request *controlv1.ValidateJobSpecRequest,
) (job.MutationResult, error) {
	for attempt := 0; attempt < 2; attempt++ {
		validation, err := s.validator.ValidateForMutation(ctx, request)
		if err != nil {
			return job.MutationResult{}, err
		}
		if validation.Result == nil || !validation.Result.GetValid() {
			return job.MutationResult{}, status.Error(
				codes.InvalidArgument, "job specification failed canonical validation")
		}
		fence := validation.Fence
		mutation.Validation = &fence
		mutation.AuditAttributes = map[string]any{
			"validationId":     validation.Result.GetValidationId(),
			"specDigest":       validation.Result.GetSpecDigest(),
			"compilerRevision": validation.Result.GetCompilerRevision(),
			"executionMode":    validation.Result.GetExecutionMode().String(),
		}
		result, err := s.mutations.ApplyMutation(ctx, mutation)
		if !errors.Is(err, job.ErrValidationStale) || attempt == 1 {
			return result, err
		}
	}
	return job.MutationResult{}, job.ErrValidationStale
}

func (s *JobService) replayJobMutation(
	ctx context.Context, mutation job.Mutation,
) (job.MutationResult, bool, error) {
	result, found, err := s.mutations.ReplayMutation(ctx, mutation)
	if err != nil {
		return job.MutationResult{}, false, repositoryError(err)
	}
	return result, found, nil
}

func (s *JobService) newJobMutation(
	ctx context.Context,
	kind job.MutationKind,
	key job.Key,
	expectedVersion int64,
	idempotencyKey string,
	request proto.Message,
	method string,
	permission auth.Permission,
) (job.Mutation, error) {
	if len(idempotencyKey) < minimumIdempotencyKeySize || len(idempotencyKey) > maximumIdempotencyKeySize ||
		strings.ContainsAny(idempotencyKey, "\r\n\x00") {
		return job.Mutation{}, status.Errorf(
			codes.InvalidArgument, "idempotency_key must contain between %d and %d characters",
			minimumIdempotencyKeySize, maximumIdempotencyKeySize,
		)
	}
	tenantID, err := tenantIDForConnectionUse(ctx, key.Namespace)
	if err != nil {
		return job.Mutation{}, status.Error(codes.PermissionDenied, "tenant access denied")
	}
	decision, err := s.authorizer.Authorize(ctx, tenantID, permission)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthenticated) {
			return job.Mutation{}, status.Error(codes.Unauthenticated, "authentication required")
		}
		return job.Mutation{}, status.Error(codes.PermissionDenied, "tenant access denied")
	}
	digest, err := s.jobRequestDigest(request)
	if err != nil {
		return job.Mutation{}, status.Error(codes.Internal, "compute Job request identity")
	}
	actorID := decision.Principal.ID
	if actorID == "" {
		actorID = decision.Principal.Subject
	}
	if actorID == "" {
		actorID = "development"
	}
	return job.Mutation{
		Kind: kind, TenantID: tenantID, Key: key, ExpectedVersion: expectedVersion,
		Identity: job.MutationIdentity{
			ActorID: actorID, Method: method,
			KeyFingerprint: s.jobKeyedDigest([]byte(idempotencyKey)), RequestDigest: digest,
			RequestID: requestID(ctx, s.uidGenerator), AuditEventID: s.uidGenerator(),
			OccurredAt: s.clock().UTC(),
		},
	}, nil
}

func (s *JobService) jobRequestDigest(request proto.Message) (string, error) {
	cloned := proto.Clone(request)
	field := cloned.ProtoReflect().Descriptor().Fields().ByName(protoreflect.Name("idempotency_key"))
	if field != nil {
		cloned.ProtoReflect().Clear(field)
	}
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(cloned)
	if err != nil {
		return "", err
	}
	return s.jobKeyedDigest(payload), nil
}

func (s *JobService) jobKeyedDigest(payload []byte) string {
	mac := hmac.New(sha256.New, s.tokenKey)
	_, _ = mac.Write(payload)
	return fmt.Sprintf("sha256:%x", mac.Sum(nil))
}

func mutationJobResponse(result job.MutationResult, err error) (*controlv1.Job, error) {
	if err != nil {
		if errors.Is(err, job.ErrInvalidTransition) || errors.Is(err, job.ErrStaleEpoch) {
			return nil, lifecycleError(err)
		}
		if status.Code(err) != codes.Unknown {
			return nil, err
		}
		return nil, repositoryError(err)
	}
	if result.Job == nil {
		return nil, status.Error(codes.Internal, "Job mutation result is incomplete")
	}
	return toProtoJob(*result.Job), nil
}
