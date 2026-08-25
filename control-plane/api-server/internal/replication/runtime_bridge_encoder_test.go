package replication

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	controlv1 "io.astrasync/control-plane/api-server/gen/go/v1"
	"io.astrasync/control-plane/replication/recovery"
)

func TestCheckpointEventEncoderPreservesWALEntry(t *testing.T) {
	encoder := NewCheckpointEventEncoder("eu-west-1")
	created := time.Date(2026, time.August, 25, 4, 0, 0, 0, time.UTC)
	event, err := encoder(&recovery.WALEntry{
		Sequence: 9, Region: "us-east-1", Epoch: 3, CheckpointURI: "checkpoint://9", JobID: "job-a", Timestamp: created, CRC32C: 17,
	})
	if err != nil {
		t.Fatalf("encode checkpoint: %v", err)
	}
	decoded := new(controlv1.CheckpointEvent)
	if err := proto.Unmarshal(event.Payload, decoded); err != nil {
		t.Fatalf("decode checkpoint: %v", err)
	}
	entry := decoded.GetWalEntry()
	if entry.GetSequence() != 9 || entry.GetRegion() != "us-east-1" || entry.GetEpoch() != 3 || entry.GetJobId() != "job-a" || entry.GetCheckpointUri() != "checkpoint://9" || entry.GetCrc32C() != 17 {
		t.Fatalf("decoded WAL entry = %+v", entry)
	}
	if event.TargetRegion != "eu-west-1" {
		t.Fatalf("target region = %q, want eu-west-1", event.TargetRegion)
	}
}
