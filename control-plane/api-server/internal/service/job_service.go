package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	jobv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/auth"
	"io.astrasync/control-plane/job"
)

const (
	defaultPageSize = 50
	maximumPageSize = 100
)

type JobService struct {
	jobv1.UnimplementedJobServiceServer
	repository   job.Repository
	clock        func() time.Time
	uidGenerator func() string
	mutations    job.MutationRepository
	validator    JobMutationValidator
	authorizer   auth.Authorizer
	tokenKey     []byte
}

func NewTransactionalJobService(
	repository job.MutationRepository,
	validator JobMutationValidator,
	authorizer auth.Authorizer,
	tokenKey []byte,
	clock func() time.Time,
	uidGenerator func() string,
) (*JobService, error) {
	if repository == nil || validator == nil || authorizer == nil || len(tokenKey) < 32 {
		return nil, fmt.Errorf("transactional Job service dependencies must not be nil or undersized")
	}
	service, err := NewJobService(repository, clock, uidGenerator)
	if err != nil {
		return nil, err
	}
	service.mutations = repository
	service.validator = validator
	service.authorizer = authorizer
	service.tokenKey = append([]byte(nil), tokenKey...)
	return service, nil
}

func NewJobService(
	repository job.Repository, clock func() time.Time, uidGenerator func() string,
) (*JobService, error) {
	if err := validateDependencies(repository, clock, uidGenerator); err != nil {
		return nil, err
	}
	return &JobService{repository: repository, clock: clock, uidGenerator: uidGenerator}, nil
}

func (s *JobService) CreateJob(ctx context.Context, request *jobv1.CreateJobRequest) (*jobv1.Job, error) {
	if s.mutations != nil {
		return s.createJobMutation(ctx, request)
	}
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
	created, domainErr := job.New(key, s.uidGenerator(), spec, s.clock())
	if domainErr != nil {
		return nil, status.Error(codes.InvalidArgument, domainErr.Error())
	}
	stored, repositoryErr := s.repository.Create(ctx, created)
	if repositoryErr != nil {
		return nil, repositoryError(repositoryErr)
	}
	return toProtoJob(stored), nil
}

func (s *JobService) GetJob(ctx context.Context, request *jobv1.GetJobRequest) (*jobv1.Job, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	key, err := requestKey(request.GetNamespace(), request.GetName())
	if err != nil {
		return nil, err
	}
	stored, repositoryErr := s.repository.Get(ctx, key)
	if repositoryErr != nil {
		return nil, repositoryError(repositoryErr)
	}
	return toProtoJob(stored), nil
}

func (s *JobService) ListJobs(
	ctx context.Context, request *jobv1.ListJobsRequest,
) (*jobv1.ListJobsResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	if _, err := requestKey(request.GetNamespace(), "validation"); err != nil {
		return nil, err
	}
	page, err := requestedPage(request.GetPage(), request.GetPageSize())
	if err != nil {
		return nil, err
	}
	result, repositoryErr := s.repository.List(ctx, request.GetNamespace(), page)
	if repositoryErr != nil {
		return nil, repositoryError(repositoryErr)
	}
	if result.Total > math.MaxInt32 {
		return nil, status.Error(codes.ResourceExhausted, "job count exceeds API range")
	}
	response := &jobv1.ListJobsResponse{Items: make([]*jobv1.Job, len(result.Jobs)), Total: int32(result.Total)}
	for index, stored := range result.Jobs {
		response.Items[index] = toProtoJob(stored)
	}
	return response, nil
}

func (s *JobService) UpdateJob(ctx context.Context, request *jobv1.UpdateJobRequest) (*jobv1.Job, error) {
	if s.mutations != nil {
		return s.updateJobMutation(ctx, request)
	}
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
	current, repositoryErr := s.repository.Get(ctx, key)
	if repositoryErr != nil {
		return nil, repositoryError(repositoryErr)
	}
	if current.Version != request.GetExpectedVersion() {
		return nil, repositoryError(job.ErrConflict)
	}
	next, domainErr := current.ReplaceSpec(spec, s.clock())
	if domainErr != nil {
		return nil, lifecycleError(domainErr)
	}
	updated, repositoryErr := s.repository.Update(ctx, next, current.Version)
	if repositoryErr != nil {
		return nil, repositoryError(repositoryErr)
	}
	return toProtoJob(updated), nil
}

func (s *JobService) DeleteJob(
	ctx context.Context, request *jobv1.DeleteJobRequest,
) (*emptypb.Empty, error) {
	if s.mutations != nil {
		return s.deleteJobMutation(ctx, request)
	}
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
	current, repositoryErr := s.repository.Get(ctx, key)
	if repositoryErr != nil {
		return nil, repositoryError(repositoryErr)
	}
	if current.Version != request.GetExpectedVersion() {
		return nil, repositoryError(job.ErrConflict)
	}
	if domainErr := current.Deletable(); domainErr != nil {
		return nil, lifecycleError(domainErr)
	}
	if repositoryErr := s.repository.Delete(ctx, key, current.Version); repositoryErr != nil {
		return nil, repositoryError(repositoryErr)
	}
	return &emptypb.Empty{}, nil
}

func (s *JobService) StartJob(ctx context.Context, request *jobv1.StartJobRequest) (*jobv1.Job, error) {
	if s.mutations != nil {
		return s.startJobMutation(ctx, request)
	}
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	return s.changeDesiredState(ctx, request.GetNamespace(), request.GetName(), request.GetExpectedVersion(), true)
}

func (s *JobService) StopJob(ctx context.Context, request *jobv1.StopJobRequest) (*jobv1.Job, error) {
	if s.mutations != nil {
		return s.stopJobMutation(ctx, request)
	}
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	return s.changeDesiredState(ctx, request.GetNamespace(), request.GetName(), request.GetExpectedVersion(), false)
}

func (s *JobService) GetJobStatus(
	ctx context.Context, request *jobv1.GetJobStatusRequest,
) (*jobv1.JobStatus, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	key, err := requestKey(request.GetNamespace(), request.GetName())
	if err != nil {
		return nil, err
	}
	stored, repositoryErr := s.repository.Get(ctx, key)
	if repositoryErr != nil {
		return nil, repositoryError(repositoryErr)
	}
	return toProtoStatus(stored.Status), nil
}

func (s *JobService) changeDesiredState(
	ctx context.Context, namespace, name string, expectedVersion int64, start bool,
) (*jobv1.Job, error) {
	if expectedVersion < 0 {
		return nil, status.Error(codes.InvalidArgument, "expected_version must not be negative")
	}
	key, err := requestKey(namespace, name)
	if err != nil {
		return nil, err
	}
	current, repositoryErr := s.repository.Get(ctx, key)
	if repositoryErr != nil {
		return nil, repositoryError(repositoryErr)
	}
	var next job.Job
	var changed bool
	var domainErr error
	if start {
		next, changed, domainErr = current.RequestStart(s.clock())
	} else {
		next, changed, domainErr = current.RequestStop(s.clock())
	}
	if domainErr != nil {
		return nil, lifecycleError(domainErr)
	}
	if !changed {
		return toProtoJob(current), nil
	}
	if expectedVersion > 0 && expectedVersion != current.Version {
		return nil, repositoryError(job.ErrConflict)
	}
	updated, repositoryErr := s.repository.Update(ctx, next, current.Version)
	if repositoryErr != nil {
		if errors.Is(repositoryErr, job.ErrConflict) {
			latest, readErr := s.repository.Get(ctx, key)
			if readErr == nil && desiredCommandSatisfied(latest, start) {
				return toProtoJob(latest), nil
			}
		}
		return nil, repositoryError(repositoryErr)
	}
	return toProtoJob(updated), nil
}

func desiredCommandSatisfied(candidate job.Job, start bool) bool {
	if start {
		return candidate.Status.Desired == job.DesiredRunning &&
			(candidate.Status.State == job.StateInitializing || candidate.Status.State == job.StateRunning)
	}
	return candidate.Status.Desired == job.DesiredStopped
}

func requestKey(namespace, name string) (job.Key, error) {
	key, err := job.NewKey(namespace, name)
	if err != nil {
		return job.Key{}, status.Error(codes.InvalidArgument, err.Error())
	}
	return key, nil
}

func requestedPage(number, size int32) (job.Page, error) {
	if number < 0 || size < 0 {
		return job.Page{}, status.Error(codes.InvalidArgument, "page and page_size must not be negative")
	}
	if number == 0 {
		number = 1
	}
	if size == 0 {
		size = defaultPageSize
	}
	if size > maximumPageSize {
		return job.Page{}, status.Errorf(codes.InvalidArgument, "page_size must not exceed %d", maximumPageSize)
	}
	return job.Page{Number: int(number), Size: int(size)}, nil
}

func repositoryError(err error) error {
	switch {
	case errors.Is(err, job.ErrNotFound):
		return status.Error(codes.NotFound, "job not found")
	case errors.Is(err, job.ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, "job already exists")
	case errors.Is(err, job.ErrConflict):
		return status.Error(codes.Aborted, "job version conflict")
	case errors.Is(err, job.ErrIdempotencyReused):
		return status.Error(codes.AlreadyExists, "IDEMPOTENCY_KEY_REUSED")
	case errors.Is(err, job.ErrMutationInProgress):
		return status.Error(codes.Unavailable, "job mutation is still in progress")
	case errors.Is(err, job.ErrValidationStale):
		return status.Error(codes.Unavailable, "job validation revision changed")
	default:
		return status.Error(codes.Internal, "job repository operation failed")
	}
}

func lifecycleError(err error) error {
	switch {
	case errors.Is(err, job.ErrInvalidTransition):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, job.ErrStaleEpoch):
		return status.Error(codes.Aborted, err.Error())
	default:
		return status.Error(codes.InvalidArgument, err.Error())
	}
}

func fromProtoSpec(source *jobv1.JobSpec) (job.Spec, error) {
	if source == nil || source.GetSource() == nil || source.GetSink() == nil || source.GetDelivery() == nil {
		return job.Spec{}, status.Error(codes.InvalidArgument, "source, sink, and delivery are required")
	}
	delivery, err := fromProtoDelivery(source.GetDelivery().GetGuarantee())
	if err != nil {
		return job.Spec{}, err
	}
	maxBatchRecords := int32(job.DefaultMaxBatchRecords)
	if source.GetRuntime() != nil && source.GetRuntime().GetMaxBatchRecords() != 0 {
		maxBatchRecords = source.GetRuntime().GetMaxBatchRecords()
	}
	result := job.Spec{
		Source: job.ConnectorSpec{
			Connector:     source.GetSource().GetConnector(),
			ConnectionRef: source.GetSource().GetConnectionRef(),
			Options:       cloneStringMap(source.GetSource().GetOptions()),
		},
		Sink: job.ConnectorSpec{
			Connector:     source.GetSink().GetConnector(),
			ConnectionRef: source.GetSink().GetConnectionRef(),
			Options:       cloneStringMap(source.GetSink().GetOptions()),
		},
		Transforms: make([]job.TransformSpec, len(source.GetTransforms())),
		Delivery:   job.DeliverySpec{Guarantee: delivery},
		Runtime:    job.RuntimeSpec{MaxBatchRecords: maxBatchRecords},
	}
	for index, transform := range source.GetTransforms() {
		if transform == nil {
			return job.Spec{}, status.Errorf(codes.InvalidArgument, "transform %d must not be nil", index)
		}
		result.Transforms[index] = job.TransformSpec{
			Type: transform.GetType(), Options: cloneStringMap(transform.GetOptions()),
		}
	}
	if validationErr := result.Validate(); validationErr != nil {
		return job.Spec{}, status.Error(codes.InvalidArgument, validationErr.Error())
	}
	return result, nil
}

func fromProtoDelivery(source jobv1.DeliveryGuarantee) (job.DeliveryGuarantee, error) {
	switch source {
	case jobv1.DeliveryGuarantee_DELIVERY_GUARANTEE_EXACTLY_ONCE:
		return job.DeliveryExactlyOnce, nil
	case jobv1.DeliveryGuarantee_DELIVERY_GUARANTEE_AT_LEAST_ONCE:
		return job.DeliveryAtLeastOnce, nil
	case jobv1.DeliveryGuarantee_DELIVERY_GUARANTEE_AT_MOST_ONCE:
		return job.DeliveryAtMostOnce, nil
	default:
		return "", status.Error(codes.InvalidArgument, "delivery guarantee is required")
	}
}

func toProtoJob(source job.Job) *jobv1.Job {
	return &jobv1.Job{
		Name:      source.Key.Name,
		Namespace: source.Key.Namespace,
		Uid:       source.UID,
		Version:   source.Version,
		CreatedAt: timestamppb.New(source.CreatedAt),
		UpdatedAt: timestamppb.New(source.UpdatedAt),
		Spec:      toProtoSpec(source.Spec),
		Status:    toProtoStatus(source.Status),
	}
}

func toProtoSpec(source job.Spec) *jobv1.JobSpec {
	result := &jobv1.JobSpec{
		Source: &jobv1.ConnectorConfig{
			Connector: source.Source.Connector, ConnectionRef: source.Source.ConnectionRef,
			Options: cloneStringMap(source.Source.Options),
		},
		Sink: &jobv1.ConnectorConfig{
			Connector: source.Sink.Connector, ConnectionRef: source.Sink.ConnectionRef,
			Options: cloneStringMap(source.Sink.Options),
		},
		Transforms: make([]*jobv1.TransformConfig, len(source.Transforms)),
		Delivery:   &jobv1.DeliveryConfig{Guarantee: toProtoDelivery(source.Delivery.Guarantee)},
		Runtime:    &jobv1.RuntimeConfig{MaxBatchRecords: source.Runtime.MaxBatchRecords},
	}
	for index, transform := range source.Transforms {
		result.Transforms[index] = &jobv1.TransformConfig{
			Type: transform.Type, Options: cloneStringMap(transform.Options),
		}
	}
	return result
}

func toProtoDelivery(source job.DeliveryGuarantee) jobv1.DeliveryGuarantee {
	switch source {
	case job.DeliveryExactlyOnce:
		return jobv1.DeliveryGuarantee_DELIVERY_GUARANTEE_EXACTLY_ONCE
	case job.DeliveryAtLeastOnce:
		return jobv1.DeliveryGuarantee_DELIVERY_GUARANTEE_AT_LEAST_ONCE
	case job.DeliveryAtMostOnce:
		return jobv1.DeliveryGuarantee_DELIVERY_GUARANTEE_AT_MOST_ONCE
	default:
		return jobv1.DeliveryGuarantee_DELIVERY_GUARANTEE_UNSPECIFIED
	}
}

func toProtoStatus(source job.Status) *jobv1.JobStatus {
	result := &jobv1.JobStatus{
		DesiredState: toProtoDesiredState(source.Desired),
		State:        toProtoState(source.State),
		Epoch:        source.Epoch,
		RestartCount: source.RestartCount,
		StartTime:    toProtoTimestamp(source.StartTime),
		EndTime:      toProtoTimestamp(source.EndTime),
	}
	if source.LastCheckpoint != nil {
		result.LastCheckpoint = &jobv1.CheckpointInfo{
			Id:         source.LastCheckpoint.ID,
			Timestamp:  timestamppb.New(source.LastCheckpoint.Timestamp),
			StateSize:  source.LastCheckpoint.StateSize,
			DurationMs: source.LastCheckpoint.DurationMS,
		}
	}
	if source.Failure != nil {
		result.Failure = &jobv1.FailureInfo{
			Reason:    source.Failure.Reason,
			RootCause: source.Failure.RootCause,
			Timestamp: timestamppb.New(source.Failure.Timestamp),
			Host:      source.Failure.Host,
		}
	}
	return result
}

func toProtoDesiredState(source job.DesiredState) jobv1.DesiredState {
	switch source {
	case job.DesiredStopped:
		return jobv1.DesiredState_DESIRED_STATE_STOPPED
	case job.DesiredRunning:
		return jobv1.DesiredState_DESIRED_STATE_RUNNING
	default:
		return jobv1.DesiredState_DESIRED_STATE_UNSPECIFIED
	}
}

func toProtoState(source job.State) jobv1.JobState {
	states := map[job.State]jobv1.JobState{
		job.StateCreated:      jobv1.JobState_JOB_STATE_CREATED,
		job.StateInitializing: jobv1.JobState_JOB_STATE_INITIALIZING,
		job.StateRunning:      jobv1.JobState_JOB_STATE_RUNNING,
		job.StateCanceling:    jobv1.JobState_JOB_STATE_CANCELING,
		job.StateCanceled:     jobv1.JobState_JOB_STATE_CANCELED,
		job.StateFinished:     jobv1.JobState_JOB_STATE_FINISHED,
		job.StateFailed:       jobv1.JobState_JOB_STATE_FAILED,
	}
	return states[source]
}

func toProtoTimestamp(source *time.Time) *timestamppb.Timestamp {
	if source == nil {
		return nil
	}
	return timestamppb.New(*source)
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func validateDependencies(repository job.Repository, clock func() time.Time, uidGenerator func() string) error {
	if repository == nil || clock == nil || uidGenerator == nil {
		return fmt.Errorf("job service dependencies must not be nil")
	}
	return nil
}
