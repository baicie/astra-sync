// Package replication exposes the generated cross-region gRPC contracts.
package replication

import (
	"context"
	"errors"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
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

// Service implements the generated cross-region services. Checkpoint events
// are published by the runtime bridge and delivered to bounded subscribers.
type Service struct {
	controlv1.UnimplementedRegionTopologyServiceServer
	controlv1.UnimplementedReplicationServiceServer
	controlv1.UnimplementedRegionPromotionServiceServer

	mu           sync.RWMutex
	regions      []TopologyRegion
	subscribers  map[string]map[chan *controlv1.CheckpointEvent]struct{}
	acknowledged map[string]int64
	backend      PromotionBackend
}

// NewService creates a replication service with an immutable topology snapshot.
func NewService(regions []TopologyRegion, backend PromotionBackend) *Service {
	copyRegions := append([]TopologyRegion(nil), regions...)
	return &Service{
		regions:      copyRegions,
		subscribers:  make(map[string]map[chan *controlv1.CheckpointEvent]struct{}),
		acknowledged: make(map[string]int64),
		backend:      backend,
	}
}

// PublishCheckpoint publishes a checkpoint event to subscribers for a region.
func (s *Service) PublishCheckpoint(ctx context.Context, event *controlv1.CheckpointEvent) error {
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

// StreamCheckpoints subscribes a secondary region to checkpoint events.
func (s *Service) StreamCheckpoints(request *controlv1.StreamCheckpointsRequest, stream controlv1.ReplicationService_StreamCheckpointsServer) error {
	if request == nil || request.GetRegion() == "" {
		return status.Error(codes.InvalidArgument, "region is required")
	}
	ch := make(chan *controlv1.CheckpointEvent, 128)
	s.mu.Lock()
	if s.subscribers[request.GetRegion()] == nil {
		s.subscribers[request.GetRegion()] = make(map[chan *controlv1.CheckpointEvent]struct{})
	}
	s.subscribers[request.GetRegion()][ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subscribers[request.GetRegion()], ch)
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
func (s *Service) ReportReplicationStatus(_ context.Context, request *controlv1.ReportReplicationStatusRequest) (*controlv1.ReportReplicationStatusResponse, error) {
	if request == nil || request.GetRegion() == "" || request.GetReplicationLagMillis() < 0 || request.GetLastReplicatedSequence() < 0 {
		return nil, status.Error(codes.InvalidArgument, "valid region, lag, and sequence are required")
	}
	s.mu.Lock()
	s.acknowledged[request.GetRegion()] = request.GetLastReplicatedSequence()
	s.mu.Unlock()
	instruction := controlv1.ReplicationInstruction_REPLICATION_INSTRUCTION_CONTINUE
	if !request.GetIsHealthy() {
		instruction = controlv1.ReplicationInstruction_REPLICATION_INSTRUCTION_PAUSE
	}
	return &controlv1.ReportReplicationStatusResponse{AcknowledgedSequence: request.GetLastReplicatedSequence(), Instruction: instruction}, nil
}

// PromoteRegion delegates an operator promotion to the configured backend.
func (s *Service) PromoteRegion(ctx context.Context, request *controlv1.PromoteRegionRequest) (*controlv1.PromoteRegionResponse, error) {
	if s.backend == nil {
		return nil, status.Error(codes.FailedPrecondition, ErrBackendUnavailable.Error())
	}
	return s.backend.Promote(ctx, request)
}

// GetPromotionStatus delegates status lookup to the configured backend.
func (s *Service) GetPromotionStatus(ctx context.Context, request *controlv1.GetPromotionStatusRequest) (*controlv1.PromotionStatus, error) {
	if s.backend == nil {
		return nil, status.Error(codes.FailedPrecondition, ErrBackendUnavailable.Error())
	}
	return s.backend.Status(ctx, request)
}
