package jobs

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

	Status Status
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
