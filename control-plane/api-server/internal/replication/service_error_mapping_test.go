package replication

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	replicationpromotion "io.astrasync/control-plane/replication/promotion"
	replicationrecovery "io.astrasync/control-plane/replication/recovery"
)

func TestPromoteRegionMapsDomainErrorsToStableCodes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code codes.Code
	}{
		{name: "invalid_request", err: replicationpromotion.ErrInvalidRequest, code: codes.InvalidArgument},
		{name: "job_not_found", err: replicationpromotion.ErrJobNotFound, code: codes.NotFound},
		{name: "epoch_conflict", err: replicationpromotion.ErrEpochConflict, code: codes.Aborted},
		{name: "standby_prerequisite", err: replicationpromotion.ErrNotStandbyRegion, code: codes.FailedPrecondition},
		{name: "wrapped_capability_failure", err: fmt.Errorf("revalidation: %w", replicationpromotion.ErrCapabilityFailed), code: codes.FailedPrecondition},
		{name: "unknown_backend_failure", err: errors.New("database details must not escape"), code: codes.Internal},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			service := NewService(nil, promotionErrorBackend{err: testCase.err})
			_, err := service.PromoteRegion(context.Background(), &controlv1.PromoteRegionRequest{TargetRegion: "eu-west-1"})
			if status.Code(err) != testCase.code {
				t.Fatalf("status code = %s, want %s", status.Code(err), testCase.code)
			}
			if testCase.code == codes.Internal && status.Convert(err).Message() != "promotion failed" {
				t.Fatalf("internal message = %q, want sanitized message", status.Convert(err).Message())
			}
		})
	}
}

func TestRecoverForPromotionMapsDomainErrorsToStableCodes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code codes.Code
	}{
		{name: "checkpoint_missing", err: replicationrecovery.ErrCheckpointNotFound, code: codes.NotFound},
		{name: "checkpoint_corrupted", err: replicationrecovery.ErrCheckpointCorrupted, code: codes.FailedPrecondition},
		{name: "wrapped_recovery_abort", err: fmt.Errorf("recovery: %w", replicationrecovery.ErrRecoveryAborted), code: codes.FailedPrecondition},
		{name: "unknown_backend_failure", err: errors.New("object store details must not escape"), code: codes.Internal},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			service := NewService(recoveryRegions(), nil)
			service.SetRecoveryBackend(recoveryErrorBackend{err: testCase.err})
			_, err := service.RecoverForPromotion(context.Background(), validRecoveryRequest())
			if status.Code(err) != testCase.code {
				t.Fatalf("status code = %s, want %s", status.Code(err), testCase.code)
			}
			if testCase.code == codes.Internal && status.Convert(err).Message() != "recovery failed" {
				t.Fatalf("internal message = %q, want sanitized message", status.Convert(err).Message())
			}
		})
	}
}

func TestReplicationErrorMappingPreservesStatusAndContextErrors(t *testing.T) {
	statusErr := status.Error(codes.ResourceExhausted, "queue is full")
	if got := mapPromotionError(statusErr); status.Code(got) != codes.ResourceExhausted {
		t.Fatalf("preserved status code = %s, want %s", status.Code(got), codes.ResourceExhausted)
	}
	if got := mapRecoveryError(context.DeadlineExceeded); !errors.Is(got, context.DeadlineExceeded) {
		t.Fatalf("context error = %v, want deadline exceeded", got)
	}
}

type promotionErrorBackend struct{ err error }

func (b promotionErrorBackend) Promote(context.Context, *controlv1.PromoteRegionRequest) (*controlv1.PromoteRegionResponse, error) {
	return nil, b.err
}

func (b promotionErrorBackend) Status(context.Context, *controlv1.GetPromotionStatusRequest) (*controlv1.PromotionStatus, error) {
	return nil, b.err
}

type recoveryErrorBackend struct{ err error }

func (b recoveryErrorBackend) RecoverForPromotion(context.Context, *controlv1.RecoverForPromotionRequest) (*controlv1.RecoverForPromotionResponse, error) {
	return nil, b.err
}
