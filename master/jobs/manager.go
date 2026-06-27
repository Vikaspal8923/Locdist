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

func (m *Manager) PrepareJob(job JobState) error {
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
	m.currentJob = &job
	return nil
}

func (m *Manager) CurrentJob() (*JobState, error) {

	if m.currentJob == nil {
		return nil, fmt.Errorf("no active job")
	}

	return m.currentJob, nil
}
