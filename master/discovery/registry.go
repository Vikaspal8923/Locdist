package discovery

import (
	"sync"
	"time"
)

type Registry struct {
	mu      sync.RWMutex
	workers map[string]Worker
}

func NewRegistry() *Registry {
	return &Registry{
		workers: make(map[string]Worker),
	}
}

func (r *Registry) Upsert(worker Worker) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if worker.ID == "" {
		worker.ID = ID(worker.Instance, worker.Address, worker.GRPCPort)
	}
	_, existed := r.workers[worker.ID]
	r.workers[worker.ID] = worker
	return !existed
}

func (r *Registry) Workers() []Worker {
	r.mu.RLock()
	defer r.mu.RUnlock()

	workers := make([]Worker, 0, len(r.workers))
	for _, worker := range r.workers {
		workers = append(workers, worker)
	}
	return workers
}

func (r *Registry) Worker(id string) (Worker, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	worker, ok := r.workers[id]
	return worker, ok
}

func (r *Registry) Prune(before time.Time) []Worker {
	r.mu.Lock()
	defer r.mu.Unlock()

	var removed []Worker
	for instance, worker := range r.workers {
		if worker.LastSeen.Before(before) {
			removed = append(removed, worker)
			delete(r.workers, instance)
		}
	}
	return removed
}
