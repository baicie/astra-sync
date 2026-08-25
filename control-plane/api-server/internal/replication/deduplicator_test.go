package replication

import (
	"context"
	"sync"

	replicationdomain "io.astrasync/control-plane/replication"
)

type testDeduplicator struct {
	mu      sync.Mutex
	entries map[string]bool
	pending map[string]bool
}

func newTestDeduplicator() *testDeduplicator {
	return &testDeduplicator{entries: make(map[string]bool), pending: make(map[string]bool)}
}

func (d *testDeduplicator) ClaimCheckpoint(_ context.Context, sourceRegion, targetRegion, jobID string, sequence, _ int64, _ string, _ uint32) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := sourceRegion + ":" + targetRegion + ":" + jobID
	if d.entries[key] {
		return false, nil
	}
	if d.pending[key] {
		return false, replicationdomain.ErrCheckpointAdmissionInProgress
	}
	d.pending[key] = true
	return true, nil
}

func (d *testDeduplicator) CommitCheckpoint(_ context.Context, sourceRegion, targetRegion, jobID string, _ int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := sourceRegion + ":" + targetRegion + ":" + jobID
	delete(d.pending, key)
	d.entries[key] = true
	return nil
}

func (d *testDeduplicator) ReleaseCheckpoint(_ context.Context, sourceRegion, targetRegion, jobID string, _ int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.pending, sourceRegion+":"+targetRegion+":"+jobID)
	return nil
}
