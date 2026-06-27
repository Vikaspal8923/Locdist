package workers

import (
	"fmt"
	"sync"

	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
)

type Manager struct {
	mu       sync.RWMutex
	workers  map[string]State
	pairings map[string]Pairing
	store    PairingStore
}

type Pairing struct {
	MasterID string
	Token    string
}

func New() *Manager {
	return &Manager{
		workers:  make(map[string]State),
		pairings: make(map[string]Pairing),
	}
}

func NewPersistent(store PairingStore) (*Manager, error) {
	pairings, err := store.Load()
	if err != nil {
		return nil, fmt.Errorf("load Master pairings: %w", err)
	}
	return &Manager{
		workers:  make(map[string]State),
		pairings: pairings,
		store:    store,
	}, nil
}

func (m *Manager) ReservePairing(
	workerID string,
	masterID string,
	token string,
) error {
	if workerID == "" || masterID == "" || token == "" {
		return fmt.Errorf("complete pairing credentials are required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.pairings[workerID] = Pairing{
		MasterID: masterID,
		Token:    token,
	}
	return m.savePairings()
}

func (m *Manager) RevokePairing(workerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.pairings, workerID)
	delete(m.workers, workerID)
	_ = m.savePairings()
}

func (m *Manager) RevokeAuthenticated(
	request *gradient.UnpairWorkerRequest,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	pairing, ok := m.pairings[request.GetWorkerId()]
	if !ok ||
		pairing.MasterID != request.GetMasterId() ||
		pairing.Token != request.GetPairingToken() {
		return fmt.Errorf("worker pairing credentials are invalid")
	}

	delete(m.pairings, request.GetWorkerId())
	delete(m.workers, request.GetWorkerId())
	return m.savePairings()
}

func (m *Manager) savePairings() error {
	if m.store == nil {
		return nil
	}
	return m.store.Save(m.pairings)
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

	pairing, ok := m.pairings[request.GetWorkerId()]
	if !ok ||
		pairing.MasterID != request.GetMasterId() ||
		pairing.Token != request.GetPairingToken() {
		return State{}, fmt.Errorf("worker pairing credentials are invalid")
	}

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
