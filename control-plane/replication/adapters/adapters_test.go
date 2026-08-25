package adapters

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"

	"io.astrasync/control-plane/replication/objectstore"
	"io.astrasync/control-plane/replication/recovery"
	"io.astrasync/control-plane/replication/wal"
)

func TestWALReaderReadsSortedExactEntries(t *testing.T) {
	store := newTestObjectStore(t)
	writeWALEntry(t, store, 3, "job-3")
	writeWALEntry(t, store, 1, "job-1")
	writeWALEntry(t, store, 2, "job-2")
	if err := store.PutObject(context.Background(), "replication/wal/us-east-1/not-a-wal.txt", []byte("ignored")); err != nil {
		t.Fatal(err)
	}

	reader, err := NewWALReader(context.Background(), store, zap.NewNop(), "us-east-1", "replication/wal", 0)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := reader.ReadEntries(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Sequence != 2 || entries[1].Sequence != 3 {
		t.Fatalf("entries = %#v, want sequences 2 and 3", entries)
	}
	if entries[0].JobID != "job-2" || entries[1].JobID != "job-3" {
		t.Fatalf("entries = %#v, want matching job IDs", entries)
	}
}

func TestWALReaderRejectsCorruptEntry(t *testing.T) {
	store := newTestObjectStore(t)
	if err := store.PutObject(context.Background(), "replication/wal/us-east-1/0000000000000001.wal", []byte("corrupt")); err != nil {
		t.Fatal(err)
	}
	reader, err := NewWALReader(context.Background(), store, zap.NewNop(), "us-east-1", "replication/wal", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadEntries(context.Background(), 0); err == nil {
		t.Fatal("ReadEntries() returned nil error for corrupt WAL")
	}
}

func TestWALReaderHonorsCancellation(t *testing.T) {
	store := newTestObjectStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader, err := NewWALReader(context.Background(), store, zap.NewNop(), "us-east-1", "replication/wal", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadEntries(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("ReadEntries() error = %v, want context.Canceled", err)
	}
}

func TestCheckpointValidatorRejectsInvalidManifest(t *testing.T) {
	validator := CheckpointValidator{}
	cases := []struct {
		name     string
		manifest *recovery.CheckpointManifest
	}{
		{name: "nil", manifest: nil},
		{name: "blank job", manifest: &recovery.CheckpointManifest{Sequence: 1, CheckpointURI: "manifest.json"}},
		{name: "invalid sequence", manifest: &recovery.CheckpointManifest{JobID: "job", Sequence: 0, CheckpointURI: "manifest.json"}},
		{name: "invalid file", manifest: &recovery.CheckpointManifest{JobID: "job", Sequence: 1, CheckpointURI: "manifest.json", Files: []recovery.CheckpointFile{{Name: "state", URI: "state", Size: -1}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validator.Validate(context.Background(), tc.manifest); !errors.Is(err, recovery.ErrCheckpointCorrupted) {
				t.Fatalf("Validate() error = %v, want ErrCheckpointCorrupted", err)
			}
		})
	}
}

func TestFileStateRestorerReadsAllFiles(t *testing.T) {
	store := newTestObjectStore(t)
	if err := store.PutObject(context.Background(), "state/a", []byte("a")); err != nil {
		t.Fatal(err)
	}
	if err := store.PutObject(context.Background(), "state/b", []byte("b")); err != nil {
		t.Fatal(err)
	}
	restorer, err := NewFileStateRestorer(store)
	if err != nil {
		t.Fatal(err)
	}
	manifest := &recovery.CheckpointManifest{Files: []recovery.CheckpointFile{{Name: "a", URI: "state/a"}, {Name: "b", URI: "state/b"}}}
	if err := restorer.Restore(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
}

func TestFileStoreRejectsTraversalAndSupportsRoundTrip(t *testing.T) {
	store := newTestObjectStore(t)
	if err := store.PutObject(context.Background(), "nested/value", []byte("payload")); err != nil {
		t.Fatal(err)
	}
	data, err := store.GetObject(context.Background(), "nested/value")
	if err != nil || string(data) != "payload" {
		t.Fatalf("GetObject() = %q, %v", data, err)
	}
	for _, key := range []string{"../outside", "/absolute", `..\\outside`} {
		if err := store.PutObject(context.Background(), key, []byte("bad")); !errors.Is(err, objectstore.ErrInvalidKey) {
			t.Fatalf("PutObject(%q) error = %v, want ErrInvalidKey", key, err)
		}
	}
}

func newTestObjectStore(t *testing.T) *objectstore.FileStore {
	t.Helper()
	root := filepath.Join(t.TempDir(), "objects")
	store, err := objectstore.NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func writeWALEntry(t *testing.T, store *objectstore.FileStore, sequence int64, jobID string) {
	t.Helper()
	entry := &wal.Entry{Sequence: sequence, Region: "us-east-1", Epoch: 4, CheckpointURI: "checkpoints/" + jobID, JobID: jobID, Timestamp: time.Unix(sequence, 0).UTC()}
	data, err := entry.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.ToSlash(filepath.Join("replication/wal/us-east-1", formatTestSequence(sequence)+".wal"))
	if err := store.PutObject(context.Background(), key, data); err != nil {
		t.Fatal(err)
	}
}

func formatTestSequence(sequence int64) string {
	return fmt.Sprintf("%016d", sequence)
}
