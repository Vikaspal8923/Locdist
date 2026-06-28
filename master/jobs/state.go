package jobs

import gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"

type Status string

const (
	StatusPrepared Status = "prepared"
	StatusRunning  Status = "running"
	StatusFailed   Status = "failed"
	StatusFinished Status = "finished"
)

type JobState struct {
	JobID string

	ExpectedWorkers int

	Name        string
	ProjectRoot string
	Entrypoint  string
	DatasetPath string
	Workers     []WorkerAssignment
	Shards      []ShardAssignment
	Setup       map[string]WorkerSetup

	Status Status
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
