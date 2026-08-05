package job

import (
	"context"
	"errors"
)

var (
	ErrNotFound      = errors.New("job not found")
	ErrAlreadyExists = errors.New("job already exists")
	ErrConflict      = errors.New("job version conflict")
)

type Page struct {
	Number int
	Size   int
}

func (p Page) Validate() error {
	if p.Number <= 0 || p.Size <= 0 {
		return errors.New("page number and size must be positive")
	}
	return nil
}

type PageResult struct {
	Jobs  []Job
	Total int
}

type Repository interface {
	Create(context.Context, Job) (Job, error)
	Get(context.Context, Key) (Job, error)
	List(context.Context, string, Page) (PageResult, error)
	Update(context.Context, Job, int64) (Job, error)
	Delete(context.Context, Key, int64) error
}
