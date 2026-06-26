package jobs

import "fmt"

type Manager struct {
	currentJob *JobState
}

func New() *Manager {
	return &Manager{}
}

func (m *Manager) StartJob(
	jobID string,
	expectedWorkers int,
) error {

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

func (m *Manager) CurrentJob() (*JobState, error) {

	if m.currentJob == nil {
		return nil, fmt.Errorf("no active job")
	}

	return m.currentJob, nil
}
