package jobs

import (
	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
	"time"
)

type Status string

const (
	StatusPrepared  Status = "prepared"
	StatusStarting  Status = "starting"
	StatusRunning   Status = "running"
	StatusFailed    Status = "failed"
	StatusFinished  Status = "finished"
	StatusCancelled Status = "cancelled"
)

type JobState struct {
	JobID string

	ExpectedWorkers int

	Name        string
	ProjectRoot string
	Entrypoint  string
	DatasetPath string
	Outputs     []string
	Workers     []WorkerAssignment
	Shards      []ShardAssignment
	Setup       map[string]WorkerSetup
	Run         map[string]WorkerRun

	Status Status
}

type WorkerRun struct {
	Status       gradient.JobRunStatus
	ErrorMessage string
	LogPath      string
}

type WorkerFinalResult struct {
	Status       gradient.JobRunStatus
	ErrorMessage string
	ExitCode     int32
	LogTail      string
}

type Summary struct {
	JobID      string
	Status     Status
	Reason     string
	FinishedAt time.Time
	Workers    map[string]WorkerFinalResult
}

type WorkerSetup struct {
	Status       gradient.JobSetupStatus
	ErrorMessage string
	LogPath      string
}

type WorkerAssignment struct {
	WorkerID string
	Host     string
	GRPCPort string
}

type ShardAssignment struct {
	WorkerID string
	Start    int
	End      int
	Count    int
	Path     string
}
