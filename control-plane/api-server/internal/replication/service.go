// Package replication exposes the generated cross-region gRPC contracts.
package replication

import (
	"context"
	"errors"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	replicationdomain "io.astrasync/control-plane/replication"
	replicationmetrics "io.astrasync/control-plane/replication/metrics"
	replicationpromotion "io.astrasync/control-plane/replication/promotion"
	replicationrecovery "io.astrasync/control-plane/replication/recovery"
)

var ErrBackendUnavailable = errors.New("replication backend is not configured")

// TopologyRegion is the API-facing topology representation.
type TopologyRegion struct {
	Name                  string
	Role                  controlv1.RegionRole
	APIServerEndpoint     string
	PostgresReference     string
	ObjectStorageEndpoint string
	ObjectStorageBucket   string
	WALPrefix             string
}

// PromotionBackend executes an operator promotion and reports its status.
type PromotionBackend interface {
	Promote(context.Context, *controlv1.PromoteRegionRequest) (*controlv1.PromoteRegionResponse, error)
	Status(context.Context, *controlv1.GetPromotionStatusRequest) (*controlv1.PromotionStatus, error)
}

// RecoveryBackend executes checkpoint-coupled recovery for a promotion.
type RecoveryBackend interface {
	RecoverForPromotion(context.Context, *controlv1.RecoverForPromotionRequest) (*controlv1.RecoverForPromotionResponse, error)
}

// Service implements the generated cross-region services. Checkpoint events
// are published by the runtime bridge and delivered to bounded subscribers.
type Service struct {
	controlv1.UnimplementedRegionTopologyServiceServer
	controlv1.UnimplementedReplicationServiceServer
	controlv1.UnimplementedRegionPromotionServiceServer
	controlv1.UnimplementedRegionRecoveryServiceServer

	mu           sync.RWMutex
	regions      []TopologyRegion
	subscribers  map[string]map[chan *controlv1.CheckpointEvent]struct{}
	subscription map[chan *controlv1.CheckpointEvent]checkpointSubscription
	acknowledged map[string]int64
	backend      PromotionBackend
	recovery     RecoveryBackend
	deduplicator replicationdomain.CheckpointDeduplicator
	metrics      *replicationmetrics.Recorder
}

type checkpointSubscription struct {
	jobID            string
	resumeFrom       int64
	filterBySequence bool
}

// Option configures a replication service.
type Option func(*Service)

// WithMetrics attaches the multi-region metrics recorder.
func WithMetrics(recorder *replicationmetrics.Recorder) Option {
	return func(service *Service) { service.metrics = recorder }
}

// WithCheckpointDeduplicator attaches durable checkpoint admission tracking.
func WithCheckpointDeduplicator(deduplicator replicationdomain.CheckpointDeduplicator) Option {
	return func(service *Service) { service.deduplicator = deduplicator }
}

// NewService creates a replication service with an immutable topology snapshot.
func (s *Service) SetPromotionBackend(backend PromotionBackend) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.backend = backend
}

// SetRecoveryBackend attaches the checkpoint-coupled recovery implementation.
func (s *Service) SetRecoveryBackend(backend RecoveryBackend) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recovery = backend
}

func NewService(regions []TopologyRegion, backend PromotionBackend, options ...Option) *Service {
	copyRegions := append([]TopologyRegion(nil), regions...)
	service := &Service{
		regions:      copyRegions,
		subscribers:  make(map[string]map[chan *controlv1.CheckpointEvent]struct{}),
		subscription: make(map[chan *controlv1.CheckpointEvent]checkpointSubscription),
		acknowledged: make(map[string]int64),
		backend:      backend,
	}
	for _, option := range options {
		option(service)
	}
	return service
}

// PublishCheckpoint publishes a checkpoint event to subscribers for a region.
func (s *Service) PublishCheckpoint(ctx context.Context, event *controlv1.CheckpointEvent) (err error) {
	startedAt := time.Now()
	peerRegion := "_unknown"
	if event != nil && event.GetWalEntry() != nil && event.GetWalEntry().GetRegion() != "" {
		peerRegion = event.GetWalEntry().GetRegion()
	}
	defer func() {
		if s.metrics == nil {
			return
		}
		outcome := "success"
		if err != nil {
			outcome = "failure"
		}
		s.metrics.ObserveEvent(peerRegion, checkpointEventType(event), outcome, time.Since(startedAt))
	}()
	if event == nil || event.GetWalEntry() == nil {
		return status.Error(codes.InvalidArgument, "checkpoint event and WAL entry are required")
	}
	target := event.GetWalEntry().GetRegion()
	s.mu.RLock()
	defer s.mu.RUnlock()
	for region, subscribers := range s.subscribers {
		if target != "" && target != region {
			continue
		}
		for ch := range subscribers {
			subscription := s.subscription[ch]
			if subscription.jobID != "" && subscription.jobID != event.GetWalEntry().GetJobId() {
				continue
			}
			if subscription.filterBySequence && event.GetWalEntry().GetSequence() <= subscription.resumeFrom {
				continue
			}
			select {
			case ch <- event:
			case <-ctx.Done():
				return ctx.Err()
			default:
				return status.Error(codes.ResourceExhausted, "replication subscriber queue is full")
			}
		}
	}
	return nil
}

func checkpointEventType(event *controlv1.CheckpointEvent) string {
	if event == nil {
		return "unknown"
	}
	switch event.GetEventType() {
	case controlv1.CheckpointEventType_CHECKPOINT_EVENT_TYPE_CHECKPOINT_COMPLETED:
		return "checkpoint"
	case controlv1.CheckpointEventType_CHECKPOINT_EVENT_TYPE_CHECKPOINT_GAP:
		return "gap"
	default:
		return "unknown"
	}
}

// GetRegionTopology returns the configured topology snapshot.
func (s *Service) GetRegionTopology(context.Context, *controlv1.GetRegionTopologyRequest) (*controlv1.GetRegionTopologyResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	regions := make([]*controlv1.Region, 0, len(s.regions))
	for _, region := range s.regions {
		regions = append(regions, &controlv1.Region{
			Name: region.Name, Role: region.Role, ApiServerEndpoint: region.APIServerEndpoint,
			PostgresReference: region.PostgresReference, ObjectStorageEndpoint: region.ObjectStorageEndpoint,
			ObjectStorageBucket: region.ObjectStorageBucket, WalPrefix: region.WALPrefix,
		})
	}
	return &controlv1.GetRegionTopologyResponse{Regions: regions}, nil
}

// StreamTopologyUpdates is intentionally fail-closed until a topology watcher backend is attached.
func (s *Service) StreamTopologyUpdates(*controlv1.StreamTopologyUpdatesRequest, controlv1.RegionTopologyService_StreamTopologyUpdatesServer) error {
	return status.Error(codes.Unimplemented, "topology update backend is not configured")
}

// PushCheckpoint accepts one checkpoint event from a primary region.
func (s *Service) PushCheckpoint(ctx context.Context, request *controlv1.PushCheckpointRequest) (response *controlv1.PushCheckpointResponse, err error) {
	startedAt := time.Now()
	peerRegion := "_unknown"
	if request != nil && request.GetSourceRegion() != "" {
		peerRegion = request.GetSourceRegion()
	}
	defer func() {
		if s.metrics == nil {
			return
		}
		outcome := "success"
		if err != nil {
			outcome = "failure"
		}
		var event *controlv1.CheckpointEvent
		if request != nil {
			event = request.GetEvent()
		}
		s.metrics.ObserveEvent(peerRegion, checkpointEventType(event), outcome, time.Since(startedAt))
	}()
	if request == nil || request.GetEvent() == nil || request.GetEvent().GetWalEntry() == nil || request.GetSourceRegion() == "" || request.GetTargetRegion() == "" {
		return nil, status.Error(codes.InvalidArgument, "event, source region, and target region are required")
	}
	if request.GetEvent().GetWalEntry().GetRegion() != request.GetSourceRegion() {
		return nil, status.Error(codes.InvalidArgument, "WAL entry region must match source region")
	}
	if request.GetEvent().GetWalEntry().GetSequence() < 0 || request.GetEvent().GetWalEntry().GetEpoch() < 0 {
		return nil, status.Error(codes.InvalidArgument, "checkpoint sequence and epoch must not be negative")
	}
	sequence := request.GetEvent().GetWalEntry().GetSequence()
	jobID := request.GetEvent().GetWalEntry().GetJobId()
	if s.deduplicator != nil {
		accepted, claimErr := s.deduplicator.ClaimCheckpoint(ctx, request.GetSourceRegion(), request.GetTargetRegion(), jobID, sequence, request.GetEvent().GetWalEntry().GetEpoch(), request.GetEvent().GetWalEntry().GetCheckpointUri(), request.GetEvent().GetWalEntry().GetCrc32C())
		if claimErr != nil {
			if errors.Is(claimErr, replicationdomain.ErrCheckpointAdmissionConflict) {
				return nil, status.Error(codes.AlreadyExists, "checkpoint sequence was already admitted with different content")
			}
			if errors.Is(claimErr, replicationdomain.ErrCheckpointAdmissionInProgress) {
				return nil, status.Error(codes.ResourceExhausted, "checkpoint sequence delivery is in progress")
			}
			return nil, status.Error(codes.Internal, "checkpoint admission failed")
		}
		if !accepted {
			return &controlv1.PushCheckpointResponse{AcknowledgedSequence: sequence}, nil
		}
	}
	s.mu.RLock()
	subscribers := make([]chan *controlv1.CheckpointEvent, 0, len(s.subscribers[request.GetTargetRegion()]))
	for subscriber := range s.subscribers[request.GetTargetRegion()] {
		subscribers = append(subscribers, subscriber)
	}
	s.mu.RUnlock()
	for _, subscriber := range subscribers {
		s.mu.RLock()
		subscription := s.subscription[subscriber]
		s.mu.RUnlock()
		if subscription.jobID != "" && subscription.jobID != jobID {
			continue
		}
		if subscription.filterBySequence && sequence <= subscription.resumeFrom {
			continue
		}
		select {
		case subscriber <- request.GetEvent():
		case <-ctx.Done():
			if s.deduplicator != nil {
				_ = s.deduplicator.ReleaseCheckpoint(context.Background(), request.GetSourceRegion(), request.GetTargetRegion(), jobID, sequence)
			}
			return nil, ctx.Err()
		default:
			if s.deduplicator != nil {
				_ = s.deduplicator.ReleaseCheckpoint(context.Background(), request.GetSourceRegion(), request.GetTargetRegion(), jobID, sequence)
			}
			return nil, status.Error(codes.ResourceExhausted, "replication subscriber queue is full")
		}
	}
	s.mu.Lock()
	if sequence > s.acknowledged[request.GetTargetRegion()] {
		s.acknowledged[request.GetTargetRegion()] = sequence
	}
	s.mu.Unlock()
	if s.deduplicator != nil {
		if commitErr := s.deduplicator.CommitCheckpoint(ctx, request.GetSourceRegion(), request.GetTargetRegion(), jobID, sequence); commitErr != nil {
			_ = s.deduplicator.ReleaseCheckpoint(context.Background(), request.GetSourceRegion(), request.GetTargetRegion(), jobID, sequence)
			return nil, status.Error(codes.Internal, "checkpoint admission commit failed")
		}
	}
	return &controlv1.PushCheckpointResponse{AcknowledgedSequence: sequence}, nil
}

// StreamCheckpoints subscribes a secondary region to checkpoint events.
func (s *Service) StreamCheckpoints(request *controlv1.StreamCheckpointsRequest, stream controlv1.ReplicationService_StreamCheckpointsServer) error {
	if request == nil || request.GetRegion() == "" {
		return status.Error(codes.InvalidArgument, "region is required")
	}
	if request.GetResumeFromSequence() < 0 {
		return status.Error(codes.InvalidArgument, "resume sequence must not be negative")
	}
	ch := make(chan *controlv1.CheckpointEvent, 128)
	s.mu.Lock()
	if s.subscribers[request.GetRegion()] == nil {
		s.subscribers[request.GetRegion()] = make(map[chan *controlv1.CheckpointEvent]struct{})
	}
	s.subscribers[request.GetRegion()][ch] = struct{}{}
	s.subscription[ch] = checkpointSubscription{
		jobID:            request.GetJobId(),
		resumeFrom:       request.GetResumeFromSequence(),
		filterBySequence: request.GetResumeFromSequence() > 0,
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subscribers[request.GetRegion()], ch)
		delete(s.subscription, ch)
		close(ch)
		s.mu.Unlock()
	}()
	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case event := <-ch:
			if event == nil {
				return nil
			}
			if err := stream.Send(event); err != nil {
				return err
			}
		}
	}
}

// ReportReplicationStatus records the last replicated sequence for a region.
func (s *Service) ReportReplicationStatus(_ context.Context, request *controlv1.ReportReplicationStatusRequest) (response *controlv1.ReportReplicationStatusResponse, err error) {
	startedAt := time.Now()
	peerRegion := "_unknown"
	if request != nil && request.GetRegion() != "" {
		peerRegion = request.GetRegion()
	}
	defer func() {
		if s.metrics == nil {
			return
		}
		outcome := "success"
		if err != nil {
			outcome = "failure"
		}
		s.metrics.ObserveEvent(peerRegion, "health", outcome, time.Since(startedAt))
	}()
	if request == nil || request.GetRegion() == "" || request.GetReplicationLagMillis() < 0 || request.GetLastReplicatedSequence() < 0 {
		return nil, status.Error(codes.InvalidArgument, "valid region, lag, and sequence are required")
	}
	s.mu.Lock()
	if request.GetLastReplicatedSequence() > s.acknowledged[request.GetRegion()] {
		s.acknowledged[request.GetRegion()] = request.GetLastReplicatedSequence()
	}
	acknowledged := s.acknowledged[request.GetRegion()]
	s.mu.Unlock()
	instruction := controlv1.ReplicationInstruction_REPLICATION_INSTRUCTION_CONTINUE
	if !request.GetIsHealthy() {
		instruction = controlv1.ReplicationInstruction_REPLICATION_INSTRUCTION_PAUSE
	}
	return &controlv1.ReportReplicationStatusResponse{AcknowledgedSequence: acknowledged, Instruction: instruction}, nil
}

// PromoteRegion delegates an operator promotion to the configured backend.
func (s *Service) PromoteRegion(ctx context.Context, request *controlv1.PromoteRegionRequest) (response *controlv1.PromoteRegionResponse, err error) {
	startedAt := time.Now()
	targetRegion := "_unknown"
	if request != nil && request.GetTargetRegion() != "" {
		targetRegion = request.GetTargetRegion()
	}
	defer func() {
		if s.metrics == nil {
			return
		}
		outcome := "success"
		if err != nil {
			outcome = "failure"
		}
		s.metrics.ObservePromotion(targetRegion, outcome, time.Since(startedAt))
	}()
	if s.backend == nil {
		return nil, status.Error(codes.FailedPrecondition, ErrBackendUnavailable.Error())
	}
	response, err = s.backend.Promote(ctx, request)
	if err != nil {
		return nil, mapPromotionError(err)
	}
	return response, nil
}

// GetPromotionStatus delegates status lookup to the configured backend.
func (s *Service) GetPromotionStatus(ctx context.Context, request *controlv1.GetPromotionStatusRequest) (*controlv1.PromotionStatus, error) {
	if s.backend == nil {
		return nil, status.Error(codes.FailedPrecondition, ErrBackendUnavailable.Error())
	}
	response, err := s.backend.Status(ctx, request)
	if err != nil {
		return nil, mapPromotionError(err)
	}
	return response, nil
}

// RecoverForPromotion executes recovery on the target region after promotion.
func (s *Service) RecoverForPromotion(ctx context.Context, request *controlv1.RecoverForPromotionRequest) (*controlv1.RecoverForPromotionResponse, error) {
	if s.recovery == nil {
		return nil, status.Error(codes.FailedPrecondition, ErrBackendUnavailable.Error())
	}
	if request == nil || request.GetJobId() == "" || request.GetSourceRegion() == "" || request.GetTargetRegion() == "" || request.GetPromotionId() == "" || request.GetIdempotencyKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "job, source region, target region, promotion, and idempotency identifiers are required")
	}
	if len(request.GetIdempotencyKey()) < 16 || len(request.GetIdempotencyKey()) > 128 {
		return nil, status.Error(codes.InvalidArgument, "idempotency key must contain between 16 and 128 characters")
	}
	if request.GetSourceRegion() == request.GetTargetRegion() {
		return nil, status.Error(codes.InvalidArgument, "source and target regions must differ")
	}
	if request.GetNewEpoch() < 0 {
		return nil, status.Error(codes.InvalidArgument, "new epoch must not be negative")
	}
	if !s.hasRegion(request.GetSourceRegion()) || !s.hasRegion(request.GetTargetRegion()) {
		return nil, status.Error(codes.InvalidArgument, "source and target regions must be configured")
	}
	response, err := s.recovery.RecoverForPromotion(ctx, request)
	if err != nil {
		return nil, mapRecoveryError(err)
	}
	return response, nil
}

func mapPromotionError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	switch {
	case errors.Is(err, replicationpromotion.ErrInvalidRequest):
		return status.Error(codes.InvalidArgument, "promotion request is invalid")
	case errors.Is(err, replicationpromotion.ErrJobNotFound):
		return status.Error(codes.NotFound, "promotion job was not found")
	case errors.Is(err, replicationpromotion.ErrEpochConflict):
		return status.Error(codes.Aborted, "promotion epoch conflicts with the current job epoch")
	case errors.Is(err, replicationpromotion.ErrNotStandbyRegion),
		errors.Is(err, replicationpromotion.ErrAlreadyPromoting),
		errors.Is(err, replicationpromotion.ErrCapabilityTimeout),
		errors.Is(err, replicationpromotion.ErrCapabilityFailed),
		errors.Is(err, replicationpromotion.ErrRecoveryFailed),
		errors.Is(err, replicationpromotion.ErrPromotionAborted),
		errors.Is(err, replicationrecovery.ErrCheckpointNotFound),
		errors.Is(err, replicationrecovery.ErrCheckpointNotReplicated),
		errors.Is(err, replicationrecovery.ErrCheckpointCorrupted),
		errors.Is(err, replicationrecovery.ErrCheckpointEpochMismatch),
		errors.Is(err, replicationrecovery.ErrCheckpointSeqMismatch),
		errors.Is(err, replicationrecovery.ErrRecoveryAborted):
		return status.Error(codes.FailedPrecondition, "promotion prerequisites are not satisfied")
	default:
		return status.Error(codes.Internal, "promotion failed")
	}
}

func mapRecoveryError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	switch {
	case errors.Is(err, replicationrecovery.ErrCheckpointNotFound):
		return status.Error(codes.NotFound, "checkpoint was not found")
	case errors.Is(err, replicationrecovery.ErrCheckpointNotReplicated),
		errors.Is(err, replicationrecovery.ErrCheckpointCorrupted),
		errors.Is(err, replicationrecovery.ErrCheckpointEpochMismatch),
		errors.Is(err, replicationrecovery.ErrCheckpointSeqMismatch),
		errors.Is(err, replicationrecovery.ErrRecoveryAborted):
		return status.Error(codes.FailedPrecondition, "recovery prerequisites are not satisfied")
	default:
		return status.Error(codes.Internal, "recovery failed")
	}
}

func (s *Service) hasRegion(name string) bool {
	for _, region := range s.regions {
		if region.Name == name {
			return true
		}
	}
	return false
}
