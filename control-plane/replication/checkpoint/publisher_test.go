package checkpoint

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
	"io.astrasync/control-plane/replication/objectstore"
	"io.astrasync/control-plane/replication/wal"
)

type epochReader struct {
	epoch int64
}

func (r epochReader) GetEpoch(context.Context, string) (int64, error) { return r.epoch, nil }

func TestWALPublisherPublishesActiveEpoch(t *testing.T) {
	store, err := objectstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writer, err := wal.NewWriter(context.Background(), store, zap.NewNop(), "us-east-1", "", "replication/wal", wal.WithBatchSize(1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close(context.Background()) })
	publisher, err := NewWALPublisher(writer, epochReader{epoch: 3})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := publisher.Publish(context.Background(), Record{JobID: "job-a", Epoch: 3, CheckpointURI: "checkpoint://3", CompletedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if entry.Sequence != 1 || entry.Region != "us-east-1" {
		t.Fatalf("entry = %+v", entry)
	}
}

func TestWALPublisherRejectsFencedEpoch(t *testing.T) {
	store, err := objectstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writer, err := wal.NewWriter(context.Background(), store, zap.NewNop(), "us-east-1", "", "replication/wal")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close(context.Background()) })
	publisher, err := NewWALPublisher(writer, epochReader{epoch: 4})
	if err != nil {
		t.Fatal(err)
	}
	_, err = publisher.Publish(context.Background(), Record{JobID: "job-a", Epoch: 3, CheckpointURI: "checkpoint://3"})
	if !errors.Is(err, ErrEpochFenced) {
		t.Fatalf("error = %v, want ErrEpochFenced", err)
	}
}
