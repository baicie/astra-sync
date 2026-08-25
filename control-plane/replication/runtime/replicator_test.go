package runtime_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"io.astrasync/control-plane/replication/channel"
	"io.astrasync/control-plane/replication/recovery"
	runtimepkg "io.astrasync/control-plane/replication/runtime"
)

func TestReplicatorSendsEntriesInOrderAndAdvancesAfterAcknowledgement(t *testing.T) {
	reader := &replicatorReader{entries: []*recovery.WALEntry{
		{Sequence: 1, Region: "us-east-1", Epoch: 2, JobID: "job-a", CheckpointURI: "checkpoint://1"},
		{Sequence: 2, Region: "us-east-1", Epoch: 2, JobID: "job-a", CheckpointURI: "checkpoint://2"},
	}}
	sender := &recordingEventSender{}
	r, err := runtimepkg.NewReplicator(zap.NewNop(), reader, sender, func(entry *recovery.WALEntry) (*channel.Event, error) {
		return &channel.Event{Type: channel.EventTypeCheckpoint, SourceRegion: entry.Region, TargetRegion: "eu-west-1", Payload: []byte(entry.CheckpointURI)}, nil
	}, runtimepkg.ReplicatorConfig{BatchSize: 2, PollInterval: time.Millisecond, RetryInitial: time.Millisecond, RetryMax: time.Millisecond})
	if err != nil {
		t.Fatalf("create replicator: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	if err := r.Run(ctx, 0); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("run replicator: %v", err)
	}
	if got := sender.sequences(); len(got) != 2 || string(got[0]) != "checkpoint://1" || string(got[1]) != "checkpoint://2" {
		t.Fatalf("sent payloads = %q, want checkpoint://1 then checkpoint://2", got)
	}
	if reader.lastSince() != 2 {
		t.Fatalf("last read sequence = %d, want 2", reader.lastSince())
	}
}

func TestReplicatorRetriesFailedSendWithoutSkippingEntry(t *testing.T) {
	reader := &replicatorReader{entries: []*recovery.WALEntry{{Sequence: 7, Region: "us-east-1", JobID: "job-a", CheckpointURI: "checkpoint://7"}}}
	sender := &recordingEventSender{failures: 1}
	r, err := runtimepkg.NewReplicator(zap.NewNop(), reader, sender, func(entry *recovery.WALEntry) (*channel.Event, error) {
		return &channel.Event{Type: channel.EventTypeCheckpoint, SourceRegion: entry.Region, TargetRegion: "eu-west-1", Payload: []byte(entry.CheckpointURI)}, nil
	}, runtimepkg.ReplicatorConfig{BatchSize: 1, PollInterval: time.Millisecond, RetryInitial: time.Millisecond, RetryMax: time.Millisecond})
	if err != nil {
		t.Fatalf("create replicator: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	if err := r.Run(ctx, 0); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("run replicator: %v", err)
	}
	if sender.attempts() != 2 {
		t.Fatalf("send attempts = %d, want 2", sender.attempts())
	}
	if sender.count() != 1 {
		t.Fatalf("successful sends = %d, want 1", sender.count())
	}
	if reader.lastSince() != 7 {
		t.Fatalf("last read sequence = %d, want 7", reader.lastSince())
	}
}

type replicatorReader struct {
	mu      sync.Mutex
	entries []*recovery.WALEntry
	last    int64
}

func (r *replicatorReader) ReadEntries(_ context.Context, sinceSequence int64) ([]*recovery.WALEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.last = sinceSequence
	result := make([]*recovery.WALEntry, 0, len(r.entries))
	for _, entry := range r.entries {
		if entry.Sequence > sinceSequence {
			result = append(result, entry)
		}
	}
	return result, nil
}

func (r *replicatorReader) GetLatestCheckpoint(context.Context) (*recovery.CheckpointManifest, error) {
	return nil, recovery.ErrCheckpointNotFound
}

func (r *replicatorReader) lastSince() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last
}

type recordingEventSender struct {
	mu        sync.Mutex
	payloads  [][]byte
	failures  int
	attempted int
}

func (s *recordingEventSender) SendEvent(_ context.Context, event *channel.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempted++
	if s.failures > 0 {
		s.failures--
		return errors.New("send failed")
	}
	s.payloads = append(s.payloads, append([]byte(nil), event.Payload...))
	return nil
}

func (s *recordingEventSender) attempts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempted
}

func (s *recordingEventSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.payloads)
}

func (s *recordingEventSender) sequences() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]byte(nil), s.payloads...)
}
