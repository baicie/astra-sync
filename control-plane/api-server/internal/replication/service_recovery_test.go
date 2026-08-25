package replication

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
)

func TestRecoverForPromotionRejectsMissingBackend(t *testing.T) {
	service := NewService(nil, nil)
	_, err := service.RecoverForPromotion(context.Background(), validRecoveryRequest())
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("code = %v, want FailedPrecondition", status.Code(err))
	}
}

func TestRecoverForPromotionValidatesRequest(t *testing.T) {
	service := NewService(recoveryRegions(), nil)
	service.SetRecoveryBackend(recoveryBackendFunc(func(context.Context, *controlv1.RecoverForPromotionRequest) (*controlv1.RecoverForPromotionResponse, error) {
		return &controlv1.RecoverForPromotionResponse{}, nil
	}))
	request := validRecoveryRequest()
	request.NewEpoch = -1
	_, err := service.RecoverForPromotion(context.Background(), request)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestRecoverForPromotionRejectsInvalidTopologyAndIdempotency(t *testing.T) {
	service := NewService(recoveryRegions(), nil)
	service.SetRecoveryBackend(recoveryBackendFunc(func(context.Context, *controlv1.RecoverForPromotionRequest) (*controlv1.RecoverForPromotionResponse, error) {
		return &controlv1.RecoverForPromotionResponse{}, nil
	}))
	cases := map[string]struct {
		mutate func(*controlv1.RecoverForPromotionRequest)
	}{
		"short_idempotency_key": {mutate: func(request *controlv1.RecoverForPromotionRequest) { request.IdempotencyKey = "short" }},
		"same_regions":          {mutate: func(request *controlv1.RecoverForPromotionRequest) { request.TargetRegion = request.SourceRegion }},
		"unknown_source":        {mutate: func(request *controlv1.RecoverForPromotionRequest) { request.SourceRegion = "ap-south-1" }},
		"unknown_target":        {mutate: func(request *controlv1.RecoverForPromotionRequest) { request.TargetRegion = "ap-south-1" }},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			request := validRecoveryRequest()
			testCase.mutate(request)
			_, err := service.RecoverForPromotion(context.Background(), request)
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
			}
		})
	}
}

func TestRecoverForPromotionDelegatesToBackend(t *testing.T) {
	service := NewService(recoveryRegions(), nil)
	service.SetRecoveryBackend(recoveryBackendFunc(func(_ context.Context, request *controlv1.RecoverForPromotionRequest) (*controlv1.RecoverForPromotionResponse, error) {
		return &controlv1.RecoverForPromotionResponse{JobId: request.JobId, PromotionId: request.PromotionId, State: "recovery_complete", RecoveredEpoch: request.NewEpoch}, nil
	}))
	response, err := service.RecoverForPromotion(context.Background(), validRecoveryRequest())
	if err != nil {
		t.Fatal(err)
	}
	if response.GetState() != "recovery_complete" || response.GetRecoveredEpoch() != 7 {
		t.Fatalf("response = %+v", response)
	}
}

type recoveryBackendFunc func(context.Context, *controlv1.RecoverForPromotionRequest) (*controlv1.RecoverForPromotionResponse, error)

func (f recoveryBackendFunc) RecoverForPromotion(ctx context.Context, request *controlv1.RecoverForPromotionRequest) (*controlv1.RecoverForPromotionResponse, error) {
	return f(ctx, request)
}

func validRecoveryRequest() *controlv1.RecoverForPromotionRequest {
	return &controlv1.RecoverForPromotionRequest{JobId: "job-a", SourceRegion: "us-east-1", TargetRegion: "eu-west-1", NewEpoch: 7, PromotionId: "promotion-a", IdempotencyKey: "idempotency-key-1234"}
}

func recoveryRegions() []TopologyRegion {
	return []TopologyRegion{{Name: "us-east-1"}, {Name: "eu-west-1"}}
}
