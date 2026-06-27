package workers

import (
	"fmt"
	"sync"

	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
)

type Manager struct {
	mu      sync.RWMutex
	workers map[string]State
}

func New() *Manager {
	return &Manager{
		workers: make(map[string]State),
	}
}

func (m *Manager) Register(
	request *gradient.RegisterWorkerRequest,
) (State, error) {

	if request.GetWorkerId() == "" {
		return State{}, fmt.Errorf("worker_id is required")
	}

	if request.GetHost() == "" {
		return State{}, fmt.Errorf("worker host is required")
	}

	if request.GetGrpcPort() == "" {
		return State{}, fmt.Errorf("worker grpc_port is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	worker := m.workers[request.GetWorkerId()]
	worker.WorkerID = request.GetWorkerId()
	worker.Host = request.GetHost()
	worker.GRPCPort = request.GetGrpcPort()

	m.workers[worker.WorkerID] = worker

	return worker, nil
}

func (m *Manager) UpdateStatus(
	request *gradient.WorkerStatusUpdate,
) (State, error) {

	if request.GetWorkerId() == "" {
		return State{}, fmt.Errorf("worker_id is required")
	}

	if !validStatus(request.GetStatus()) {
		return State{}, fmt.Errorf("invalid worker status: %s", request.GetStatus())
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	worker, ok := m.workers[request.GetWorkerId()]
	if !ok {
		return State{}, fmt.Errorf("worker %q is not registered", request.GetWorkerId())
	}

	worker.Status = request.GetStatus()
	worker.JobID = request.GetJobId()
	m.workers[worker.WorkerID] = worker

	return worker, nil
}

func (m *Manager) Worker(workerID string) (State, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	worker, ok := m.workers[workerID]
	return worker, ok
}

func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.workers)
}

func validStatus(status gradient.WorkerStatus) bool {
	switch status {
	case gradient.WorkerStatus_WORKER_STATUS_IDLE,
		gradient.WorkerStatus_WORKER_STATUS_PREPARING,
		gradient.WorkerStatus_WORKER_STATUS_INSTALLING,
		gradient.WorkerStatus_WORKER_STATUS_RUNNING,
		gradient.WorkerStatus_WORKER_STATUS_COMPLETED,
		gradient.WorkerStatus_WORKER_STATUS_FAILED:
		return true
	default:
		return false
	}
}
