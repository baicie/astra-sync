package runtime_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"

	"io.astrasync/control-plane/replication/channel"
	"io.astrasync/control-plane/replication/recovery"
	runtimepkg "io.astrasync/control-plane/replication/runtime"
)

func TestReplicatorStopsOnWALGap(t *testing.T) {
	reader := &gapReader{}
	r, err := runtimepkg.NewReplicator(zap.NewNop(), reader, noopSender{}, func(entry *recovery.WALEntry) (*channel.Event, error) {
		return &channel.Event{Type: channel.EventTypeCheckpoint, SourceRegion: entry.Region, TargetRegion: "eu-west-1"}, nil
	}, runtimepkg.ReplicatorConfig{PollInterval: time.Millisecond, RetryInitial: time.Millisecond, RetryMax: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	err = r.Run(context.Background(), 0)
	if !errors.Is(err, runtimepkg.ErrInvalidReplicator) {
		t.Fatalf("error = %v, want invalid replicator", err)
	}
}

type gapReader struct{}

func (gapReader) ReadEntries(context.Context, int64) ([]*recovery.WALEntry, error) {
	return []*recovery.WALEntry{{Sequence: 1}, {Sequence: 3}}, nil
}
func (gapReader) GetLatestCheckpoint(context.Context) (*recovery.CheckpointManifest, error) {
	return nil, nil
}

type noopSender struct{}

func (noopSender) SendEvent(context.Context, *channel.Event) error { return nil }
