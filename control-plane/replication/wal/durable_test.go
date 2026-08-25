package wal

import (
	"context"
	"testing"

	"go.uber.org/zap"

	"io.astrasync/control-plane/replication/objectstore"
)

func TestAppendDurableFlushesBeforeReturning(t *testing.T) {
	store, err := objectstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writer, err := NewWriter(context.Background(), store, zap.NewNop(), "east", "", "wal", WithBatchSize(100))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close(context.Background()) })
	if err := writer.AppendDurable(context.Background(), &Entry{Epoch: 1, JobID: "job", CheckpointURI: "checkpoint://1"}); err != nil {
		t.Fatal(err)
	}
	keys, err := store.ListObjects(context.Background(), "wal/east/")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("durable WAL object count = %d, want 1", len(keys))
	}
}
