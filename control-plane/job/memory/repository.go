package memory

import (
	"context"
	"sort"
	"sync"

	"io.astrasync/control-plane/job"
)

type Repository struct {
	mu   sync.RWMutex
	jobs map[job.Key]job.Job
}

func New() *Repository {
	return &Repository{jobs: make(map[job.Key]job.Job)}
}

func (r *Repository) Create(_ context.Context, candidate job.Job) (job.Job, error) {
	if err := candidate.Validate(); err != nil {
		return job.Job{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.jobs[candidate.Key]; exists {
		return job.Job{}, job.ErrAlreadyExists
	}
	stored := candidate.Clone()
	r.jobs[candidate.Key] = stored
	return stored.Clone(), nil
}

func (r *Repository) Get(_ context.Context, key job.Key) (job.Job, error) {
	if err := key.Validate(); err != nil {
		return job.Job{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	stored, exists := r.jobs[key]
	if !exists {
		return job.Job{}, job.ErrNotFound
	}
	return stored.Clone(), nil
}

func (r *Repository) List(_ context.Context, namespace string, page job.Page) (job.PageResult, error) {
	if err := (job.Key{Namespace: namespace, Name: "validation"}).Validate(); err != nil {
		return job.PageResult{}, err
	}
	if err := page.Validate(); err != nil {
		return job.PageResult{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]job.Job, 0)
	for key, stored := range r.jobs {
		if key.Namespace == namespace {
			items = append(items, stored.Clone())
		}
	}
	sort.Slice(items, func(left, right int) bool { return items[left].Key.Name < items[right].Key.Name })
	total := len(items)
	start := (page.Number - 1) * page.Size
	if start >= total {
		return job.PageResult{Jobs: []job.Job{}, Total: total}, nil
	}
	end := min(start+page.Size, total)
	return job.PageResult{Jobs: items[start:end], Total: total}, nil
}

func (r *Repository) Update(_ context.Context, candidate job.Job, expectedVersion int64) (job.Job, error) {
	if err := candidate.Validate(); err != nil {
		return job.Job{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.jobs[candidate.Key]
	if !exists {
		return job.Job{}, job.ErrNotFound
	}
	if current.Version != expectedVersion {
		return job.Job{}, job.ErrConflict
	}
	if current.UID != candidate.UID || !current.CreatedAt.Equal(candidate.CreatedAt) {
		return job.Job{}, job.ErrConflict
	}
	candidate.Version = current.Version + 1
	r.jobs[candidate.Key] = candidate.Clone()
	return candidate.Clone(), nil
}

func (r *Repository) Delete(_ context.Context, key job.Key, expectedVersion int64) error {
	if err := key.Validate(); err != nil {
		return err
	}
	if expectedVersion <= 0 {
		return job.ErrConflict
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.jobs[key]
	if !exists {
		return job.ErrNotFound
	}
	if current.Version != expectedVersion {
		return job.ErrConflict
	}
	delete(r.jobs, key)
	return nil
}
