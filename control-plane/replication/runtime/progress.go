package runtime

import "context"

// ProgressStore persists the last acknowledged WAL sequence for one region pair.
type ProgressStore interface {
	LoadReplicationProgress(context.Context, string, string) (int64, error)
	SaveReplicationProgress(context.Context, string, string, int64) error
}
