package scheduler

import (
	"fmt"
)

type ResourceManager interface {
	AllocateWorker(slot *SlotRequirement) (*WorkerAllocation, error)
	ReleaseWorker(workerID string) error
	GetAvailableSlots() int
}

type SlotRequirement struct {
	CPUCores        float64
	HeapMemoryMB    int64
	OffHeapMemoryMB int64
	NetworkMB       int64
	Labels          map[string]string
}

type WorkerAllocation struct {
	WorkerID string
	SlotID   string
	Host     string
	Port     int
}

type TaskScheduler struct {
	workers map[string]*Worker
}

func NewTaskScheduler() *TaskScheduler {
	return &TaskScheduler{
		workers: make(map[string]*Worker),
	}
}

func (s *TaskScheduler) Schedule(requirements *SlotRequirement) (*WorkerAllocation, error) {
	// Find a suitable worker
	for _, worker := range s.workers {
		if worker.CanAllocate(requirements) {
			slot := worker.Allocate(requirements)
			return &WorkerAllocation{
				WorkerID: worker.ID,
				SlotID:   slot.ID,
				Host:     worker.Host,
				Port:     worker.Port,
			}, nil
		}
	}
	return nil, fmt.Errorf("no suitable worker found")
}

type Worker struct {
	ID           string
	Host         string
	Port         int
	TotalSlots   int
	UsedSlots    int
	Capabilities *WorkerCapabilities
}

type WorkerCapabilities struct {
	CPUCores       float64
	MemoryMB       int64
	Labels         map[string]string
	ConnectorTypes []string
}

func (w *Worker) CanAllocate(req *SlotRequirement) bool {
	if w.UsedSlots >= w.TotalSlots {
		return false
	}
	if w.Capabilities.CPUCores < req.CPUCores {
		return false
	}
	return true
}

func (w *Worker) Allocate(req *SlotRequirement) *Slot {
	w.UsedSlots++
	return &Slot{ID: fmt.Sprintf("%s-slot-%d", w.ID, w.UsedSlots)}
}

type Slot struct {
	ID       string
	WorkerID string
}
