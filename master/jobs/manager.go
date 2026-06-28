package jobs

import (
	"fmt"
	"sync"

	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
)

type Manager struct {
	mu         sync.RWMutex
	currentJob *JobState
}

func New() *Manager {
	return &Manager{}
}

func (m *Manager) StartJob(
	jobID string,
	expectedWorkers int,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.currentJob != nil {
		return fmt.Errorf("a job is already running")
	}

	m.currentJob = &JobState{
		JobID:           jobID,
		ExpectedWorkers: expectedWorkers,
		Status:          StatusRunning,
	}

	return nil
}

func (m *Manager) PrepareJob(job JobState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.currentJob != nil {
		return fmt.Errorf("a job is already active")
	}
	if job.JobID == "" {
		return fmt.Errorf("job_id is required")
	}
	if job.ExpectedWorkers <= 0 {
		return fmt.Errorf("expected workers must be greater than zero")
	}
	if len(job.Workers) != job.ExpectedWorkers {
		return fmt.Errorf("worker assignments must match expected workers")
	}
	if len(job.Shards) != job.ExpectedWorkers {
		return fmt.Errorf("shard assignments must match expected workers")
	}
	job.Status = StatusPrepared
	job.Setup = make(map[string]WorkerSetup, len(job.Workers))
	for _, worker := range job.Workers {
		job.Setup[worker.WorkerID] = WorkerSetup{Status: gradient.JobSetupStatus_JOB_SETUP_STATUS_WORKSPACE_RECEIVED}
	}
	m.currentJob = &job
	return nil
}

func (m *Manager) CurrentJob() (*JobState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.currentJob == nil {
		return nil, fmt.Errorf("no active job")
	}

	copy := *m.currentJob
	copy.Workers = append([]WorkerAssignment(nil), m.currentJob.Workers...)
	copy.Shards = append([]ShardAssignment(nil), m.currentJob.Shards...)
	copy.Setup = make(map[string]WorkerSetup, len(m.currentJob.Setup))
	for workerID, setup := range m.currentJob.Setup {
		copy.Setup[workerID] = setup
	}
	return &copy, nil
}

func (m *Manager) UpdateSetup(jobID, workerID string, setup WorkerSetup) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.currentJob == nil || m.currentJob.JobID != jobID {
		return fmt.Errorf("job %q is not active", jobID)
	}
	if _, ok := m.currentJob.Setup[workerID]; !ok {
		return fmt.Errorf("worker %q is not assigned to job", workerID)
	}
	m.currentJob.Setup[workerID] = setup
	return nil
}

func (m *Manager) AllWorkersReady(jobID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.currentJob == nil || m.currentJob.JobID != jobID || len(m.currentJob.Setup) != m.currentJob.ExpectedWorkers {
		return false
	}
	for _, setup := range m.currentJob.Setup {
		if setup.Status != gradient.JobSetupStatus_JOB_SETUP_STATUS_READY {
			return false
		}
	}
	return true
}
