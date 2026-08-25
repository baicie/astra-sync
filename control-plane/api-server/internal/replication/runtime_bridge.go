package replication

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/replication/channel"
	"io.astrasync/control-plane/replication/promotion"
	"io.astrasync/control-plane/replication/recovery"
)

// PromotionManagerBackend adapts the domain promotion manager to the API service.
type PromotionManagerBackend struct {
	manager *promotion.Manager
}

// RecoveryManagerBackend adapts the domain recovery manager to the API service.
type RecoveryManagerBackend struct {
	manager *recovery.Manager
	mu      sync.Mutex
	active  map[string]*recovery.Recovery
}

// NewRecoveryManagerBackend creates an idempotent Recovery RPC backend.
// RemoteRecoveryClient invokes recovery through the peer RegionRecoveryService.
type RemoteRecoveryClient struct {
	connection func() *grpc.ClientConn
}

// NewRemoteRecoveryClient creates a domain-level recovery client over an owned connection.
func NewRemoteRecoveryClient(conn *grpc.ClientConn) (*RemoteRecoveryClient, error) {
	if conn == nil {
		return nil, errors.New("recovery connection is required")
	}
	return NewLazyRemoteRecoveryClient(func() *grpc.ClientConn { return conn }), nil
}

// NewLazyRemoteRecoveryClient defers client creation until the runtime has connected.
func NewLazyRemoteRecoveryClient(connection func() *grpc.ClientConn) *RemoteRecoveryClient {
	return &RemoteRecoveryClient{connection: connection}
}

func (c *RemoteRecoveryClient) Recover(ctx context.Context, jobID, sourceRegion, targetRegion string, newEpoch int64, promotionID string) error {
	conn := c.connection()
	if conn == nil {
		return errors.New("recovery connection is not established")
	}
	_, err := controlv1.NewRegionRecoveryServiceClient(conn).RecoverForPromotion(ctx, &controlv1.RecoverForPromotionRequest{
		JobId: jobID, SourceRegion: sourceRegion, TargetRegion: targetRegion,
		NewEpoch: newEpoch, PromotionId: promotionID, IdempotencyKey: promotionID,
	})
	return err
}

func NewRecoveryManagerBackend(manager *recovery.Manager) (*RecoveryManagerBackend, error) {
	if manager == nil {
		return nil, errors.New("recovery manager is required")
	}
	return &RecoveryManagerBackend{manager: manager, active: make(map[string]*recovery.Recovery)}, nil
}

func (b *RecoveryManagerBackend) RecoverForPromotion(ctx context.Context, request *controlv1.RecoverForPromotionRequest) (*controlv1.RecoverForPromotionResponse, error) {
	key := request.GetJobId() + "\x00" + request.GetPromotionId()
	b.mu.Lock()
	if existing := b.active[key]; existing != nil {
		b.mu.Unlock()
		return recoveryResponse(request, existing), nil
	}
	b.mu.Unlock()
	result, err := b.manager.Recover(ctx, request.GetJobId(), request.GetNewEpoch())
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	b.active[key] = result
	b.mu.Unlock()
	return recoveryResponse(request, result), nil
}

func recoveryResponse(request *controlv1.RecoverForPromotionRequest, result *recovery.Recovery) *controlv1.RecoverForPromotionResponse {
	sequence, epoch := int64(0), result.NewEpoch
	if result.Checkpoint != nil {
		sequence = result.Checkpoint.Sequence
		epoch = result.Checkpoint.Epoch
	}
	return &controlv1.RecoverForPromotionResponse{JobId: result.JobID, PromotionId: request.GetPromotionId(), State: result.State.String(), CheckpointSequence: sequence, RecoveredEpoch: epoch}
}

func NewPromotionManagerBackend(manager *promotion.Manager) (*PromotionManagerBackend, error) {
	if manager == nil {
		return nil, errors.New("promotion manager is required")
	}
	return &PromotionManagerBackend{manager: manager}, nil
}

func (b *PromotionManagerBackend) Promote(ctx context.Context, request *controlv1.PromoteRegionRequest) (*controlv1.PromoteRegionResponse, error) {
	if request == nil {
		return nil, errors.New("promotion request is required")
	}
	result, err := b.manager.Promote(ctx, request.GetJobId(), request.GetTargetRegion(), request.GetIdempotencyKey(), request.GetExpectedVersion())
	if err != nil {
		return nil, err
	}
	return &controlv1.PromoteRegionResponse{
		JobId: request.GetJobId(), PreviousRegion: result.PreviousRegion, NewRegion: result.TargetRegion,
		NewEpoch: result.NewEpoch, Status: promotionStatus(result),
	}, nil
}

func (b *PromotionManagerBackend) Status(ctx context.Context, request *controlv1.GetPromotionStatusRequest) (*controlv1.PromotionStatus, error) {
	if request == nil {
		return nil, errors.New("promotion status request is required")
	}
	result, err := b.manager.GetStatus(ctx, request.GetJobId(), request.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return promotionStatus(result), nil
}

func promotionStatus(result *promotion.Promotion) *controlv1.PromotionStatus {
	state := controlv1.PromotionStatus_STATE_UNSPECIFIED
	switch result.State {
	case promotion.StatePending:
		state = controlv1.PromotionStatus_STATE_PROMOTION_PENDING
	case promotion.StateEpochBumped:
		state = controlv1.PromotionStatus_STATE_EPOCH_BUMPED
	case promotion.StateEpochWritten:
		state = controlv1.PromotionStatus_STATE_EPOCH_WRITTEN
	case promotion.StateCapabilityRevalidating:
		state = controlv1.PromotionStatus_STATE_CAPABILITY_REVALIDATING
	case promotion.StateCapabilityConfirmed:
		state = controlv1.PromotionStatus_STATE_CAPABILITY_CONFIRMED
	case promotion.StateFailoverComplete:
		state = controlv1.PromotionStatus_STATE_FAILOVER_COMPLETE
	case promotion.StatePromotionFailed:
		state = controlv1.PromotionStatus_STATE_PROMOTION_FAILED
	}
	return &controlv1.PromotionStatus{
		JobId: result.JobID, IdempotencyKey: result.IdempotencyKey,
		TargetRegion: result.TargetRegion, PreviousEpoch: result.PreviousEpoch,
		NewEpoch: result.NewEpoch, State: state, ErrorMessage: result.ErrorMessage,
	}
}

// EventSenderFactory creates the outbound checkpoint transport over the peer connection.
type EventSenderFactory struct{}

func (EventSenderFactory) NewEventSender(conn *grpc.ClientConn, sourceRegion, targetRegion string) channel.EventSender {
	return &EventSender{client: controlv1.NewReplicationServiceClient(conn), sourceRegion: sourceRegion, targetRegion: targetRegion}
}

// EventSender pushes checkpoint events to the peer ReplicationService.
type EventSender struct {
	client       controlv1.ReplicationServiceClient
	sourceRegion string
	targetRegion string
}

func (s *EventSender) SendEvent(ctx context.Context, event *channel.Event) error {
	if event == nil || event.Type != channel.EventTypeCheckpoint || len(event.Payload) == 0 {
		return channel.ErrInvalidEvent
	}
	checkpoint := new(controlv1.CheckpointEvent)
	if err := proto.Unmarshal(event.Payload, checkpoint); err != nil {
		return fmt.Errorf("decode checkpoint event: %w", err)
	}
	if checkpoint.GetWalEntry() == nil {
		return channel.ErrInvalidEvent
	}
	response, err := s.client.PushCheckpoint(ctx, &controlv1.PushCheckpointRequest{
		Event: checkpoint, SourceRegion: event.SourceRegion, TargetRegion: event.TargetRegion,
	})
	if err != nil {
		return err
	}
	if response.GetAcknowledgedSequence() != checkpoint.GetWalEntry().GetSequence() {
		return fmt.Errorf("checkpoint acknowledgement sequence %d does not match %d", response.GetAcknowledgedSequence(), checkpoint.GetWalEntry().GetSequence())
	}
	return nil
}

// EventHandlerBridge publishes incoming checkpoint payloads to the API service.
type EventHandlerBridge struct {
	service *Service
}

func NewEventHandlerBridge(service *Service) (*EventHandlerBridge, error) {
	if service == nil {
		return nil, errors.New("replication service is required")
	}
	return &EventHandlerBridge{service: service}, nil
}

// NewCheckpointEventEncoder converts durable WAL entries into pushable protobuf events.
func NewCheckpointEventEncoder(targetRegion string) func(*recovery.WALEntry) (*channel.Event, error) {
	return func(entry *recovery.WALEntry) (*channel.Event, error) {
		if entry == nil || entry.Sequence <= 0 || entry.Region == "" || entry.CheckpointURI == "" || entry.Epoch < 0 {
			return nil, channel.ErrInvalidEvent
		}
		payload, err := proto.Marshal(&controlv1.CheckpointEvent{
			EventType: controlv1.CheckpointEventType_CHECKPOINT_EVENT_TYPE_CHECKPOINT_COMPLETED,
			WalEntry: &controlv1.WALEntry{
				Sequence: entry.Sequence, Region: entry.Region, Epoch: entry.Epoch,
				CheckpointUri: entry.CheckpointURI, JobId: entry.JobID,
				Timestamp: timestamppb.New(entry.Timestamp), Crc32C: entry.CRC32C,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("encode checkpoint event: %w", err)
		}
		return &channel.Event{Type: channel.EventTypeCheckpoint, SourceRegion: entry.Region, TargetRegion: targetRegion, Payload: payload}, nil
	}
}

func (b *EventHandlerBridge) HandleEvent(ctx context.Context, event *channel.Event) error {
	if event == nil || event.Type != channel.EventTypeCheckpoint || len(event.Payload) == 0 {
		return channel.ErrInvalidEvent
	}
	checkpoint := new(controlv1.CheckpointEvent)
	if err := proto.Unmarshal(event.Payload, checkpoint); err != nil {
		return fmt.Errorf("decode checkpoint event: %w", err)
	}
	return b.service.PublishCheckpoint(ctx, checkpoint)
}
