package status

import (
	"sync"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
)

type Reporter interface {
	UpdateStatus(
		request *gradient.WorkerStatusUpdate,
	) (*gradient.WorkerStatusResponse, error)
}

type Manager struct {
	mu       sync.RWMutex
	workerID string
	status   gradient.WorkerStatus
	jobID    string
	reporter Reporter
}

func New(workerID string, reporter Reporter) *Manager {
	return &Manager{
		workerID: workerID,
		reporter: reporter,
	}
}

func (m *Manager) Set(
	workerStatus gradient.WorkerStatus,
	jobID string,
) error {

	_, err := m.reporter.UpdateStatus(
		&gradient.WorkerStatusUpdate{
			WorkerId: m.workerID,
			Status:   workerStatus,
			JobId:    jobID,
		},
	)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.status = workerStatus
	m.jobID = jobID
	m.mu.Unlock()

	return nil
}

func (m *Manager) Current() (gradient.WorkerStatus, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.status, m.jobID
}
