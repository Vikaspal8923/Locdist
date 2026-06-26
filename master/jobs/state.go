package jobs

type Status string

const (
	StatusRunning  Status = "running"
	StatusFailed   Status = "failed"
	StatusFinished Status = "finished"
)

type JobState struct {
	JobID string

	ExpectedWorkers int

	Status Status
}
