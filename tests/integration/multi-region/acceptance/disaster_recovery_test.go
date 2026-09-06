//go:build integration

package acceptance

import (
	"context"
	"testing"
	"time"

	"github.com/baicie/astrasync/tests/integration/multi-region/framework"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
)

func TestDisasterRecoveryDrillKeepsSecondaryAvailableAndFailsClosed(t *testing.T) {
	composeFile := "../docker-compose.yaml"
	f := framework.New(t, framework.WithComposeFile(composeFile), framework.WithInsecureTransportForTest())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := f.Teardown(ctx); err != nil {
			t.Logf("deployment teardown failed: %v", err)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := f.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap deployment: %v", err)
	}

	primaryConn, err := f.GetConnection(ctx, "us-east-1")
	if err != nil {
		t.Fatalf("connect to primary: %v", err)
	}
	if _, err := controlv1.NewRegionTopologyServiceClient(primaryConn).GetRegionTopology(ctx, &controlv1.GetRegionTopologyRequest{RequestingRegion: "us-east-1"}); err != nil {
		t.Fatalf("verify primary topology before fault: %v", err)
	}

	secondaryConn, err := f.GetConnection(ctx, "us-west-1")
	if err != nil {
		t.Fatalf("connect to secondary: %v", err)
	}
	secondaryReplication := controlv1.NewReplicationServiceClient(secondaryConn)
	checkpointSequence := int64(11)
	response, err := secondaryReplication.PushCheckpoint(ctx, &controlv1.PushCheckpointRequest{
		Event:        disasterRecoveryCheckpoint("drill-job", checkpointSequence),
		SourceRegion: "us-east-1",
		TargetRegion: "us-west-1",
	})
	if err != nil {
		t.Fatalf("replicate checkpoint before fault: %v", err)
	}
	if response.GetAcknowledgedSequence() != checkpointSequence {
		t.Fatalf("acknowledged sequence = %d, want %d", response.GetAcknowledgedSequence(), checkpointSequence)
	}
	statusResponse, err := secondaryReplication.ReportReplicationStatus(ctx, &controlv1.ReportReplicationStatusRequest{
		Region: "us-west-1", LastReplicatedSequence: checkpointSequence, IsHealthy: true,
	})
	if err != nil {
		t.Fatalf("report secondary replication status: %v", err)
	}
	if statusResponse.GetAcknowledgedSequence() != checkpointSequence {
		t.Fatalf("replication status sequence = %d, want %d", statusResponse.GetAcknowledgedSequence(), checkpointSequence)
	}

	if err := f.StopRegion(ctx, "us-east-1"); err != nil {
		t.Fatalf("stop primary region: %v", err)
	}
	if err := f.WaitForHTTPUnavailable(ctx, "us-east-1"); err != nil {
		t.Fatalf("wait for primary HTTP outage: %v", err)
	}
	if err := f.WaitForGRPCUnavailable(ctx, "us-east-1"); err != nil {
		t.Fatalf("wait for primary gRPC outage: %v", err)
	}

	topology, err := controlv1.NewRegionTopologyServiceClient(secondaryConn).GetRegionTopology(ctx, &controlv1.GetRegionTopologyRequest{RequestingRegion: "us-west-1"})
	if err != nil {
		t.Fatalf("read secondary topology during primary outage: %v", err)
	}
	assertSecondaryTopology(t, topology)
	statusResponse, err = secondaryReplication.ReportReplicationStatus(ctx, &controlv1.ReportReplicationStatusRequest{
		Region: "us-west-1", LastReplicatedSequence: checkpointSequence, IsHealthy: true,
	})
	if err != nil {
		t.Fatalf("read secondary replication status during primary outage: %v", err)
	}
	if statusResponse.GetAcknowledgedSequence() != checkpointSequence {
		t.Fatalf("replication status sequence during outage = %d, want %d", statusResponse.GetAcknowledgedSequence(), checkpointSequence)
	}

	_, err = controlv1.NewRegionPromotionServiceClient(secondaryConn).PromoteRegion(ctx, &controlv1.PromoteRegionRequest{
		JobId: "drill-job", TargetRegion: "us-west-1", IdempotencyKey: "dr-promotion-key-123456", ExpectedVersion: 1,
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("promotion status code = %s, want %s", status.Code(err), codes.FailedPrecondition)
	}

	_, err = controlv1.NewRegionRecoveryServiceClient(secondaryConn).RecoverForPromotion(ctx, &controlv1.RecoverForPromotionRequest{
		JobId: "drill-job", SourceRegion: "us-east-1", TargetRegion: "us-west-1", NewEpoch: 2,
		PromotionId: "dr-recovery-123456", IdempotencyKey: "dr-recovery-key-123456",
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("recovery status code = %s, want %s", status.Code(err), codes.NotFound)
	}

	if err := f.StartRegion(ctx, "us-east-1"); err != nil {
		t.Fatalf("restart primary region: %v", err)
	}
	if err := f.WaitForHTTPReady(ctx, "us-east-1"); err != nil {
		t.Fatalf("wait for recovered primary HTTP: %v", err)
	}
	if err := f.WaitForGRPCReady(ctx, "us-east-1"); err != nil {
		t.Fatalf("wait for recovered primary gRPC: %v", err)
	}
}

func disasterRecoveryCheckpoint(jobID string, sequence int64) *controlv1.CheckpointEvent {
	return &controlv1.CheckpointEvent{
		EventType: controlv1.CheckpointEventType_CHECKPOINT_EVENT_TYPE_CHECKPOINT_COMPLETED,
		WalEntry: &controlv1.WALEntry{
			Sequence: sequence, Region: "us-east-1", Epoch: 1, JobId: jobID,
			CheckpointUri: "replication/checkpoints/" + jobID,
		},
	}
}

func assertSecondaryTopology(t *testing.T, response *controlv1.GetRegionTopologyResponse) {
	t.Helper()
	regions := make(map[string]*controlv1.Region, len(response.GetRegions()))
	for _, region := range response.GetRegions() {
		regions[region.GetName()] = region
	}
	secondary, ok := regions["us-west-1"]
	if !ok {
		t.Fatal("secondary topology entry is missing")
	}
	primary, ok := regions["us-east-1"]
	if !ok {
		t.Fatal("primary topology entry is missing")
	}
	if secondary.GetRole() != controlv1.RegionRole_REGION_ROLE_STANDBY {
		t.Fatalf("secondary role = %s, want STANDBY", secondary.GetRole())
	}
	if primary.GetRole() != controlv1.RegionRole_REGION_ROLE_PRIMARY {
		t.Fatalf("primary role = %s, want PRIMARY", primary.GetRole())
	}
}
