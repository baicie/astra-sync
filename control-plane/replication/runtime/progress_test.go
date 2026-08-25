package runtime_test

import (
	"context"
	"sync"
	"testing"

	runtimepkg "io.astrasync/control-plane/replication/runtime"
)

type memoryProgress struct {
	mu    sync.Mutex
	value int64
	loads int
}

func (p *memoryProgress) LoadReplicationProgress(context.Context, string, string) (int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.loads++
	return p.value, nil
}

func (p *memoryProgress) SaveReplicationProgress(_ context.Context, _, _ string, sequence int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if sequence > p.value {
		p.value = sequence
	}
	return nil
}

func TestProgressStoreContractPersistsMonotonicAcknowledgements(t *testing.T) {
	store := new(memoryProgress)
	var progress runtimepkg.ProgressStore = store
	if err := progress.SaveReplicationProgress(context.Background(), "east", "west", 4); err != nil {
		t.Fatalf("save progress: %v", err)
	}
	if err := progress.SaveReplicationProgress(context.Background(), "east", "west", 2); err != nil {
		t.Fatalf("save older progress: %v", err)
	}
	value, err := progress.LoadReplicationProgress(context.Background(), "east", "west")
	if err != nil {
		t.Fatalf("load progress: %v", err)
	}
	if value != 4 {
		t.Fatalf("progress = %d, want 4", value)
	}
}
