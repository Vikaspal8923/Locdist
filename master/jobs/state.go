package jobs

import (
	"encoding/json"
	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
	"github.com/Vikaspal8923/Locdist/master/project"
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
	JobID string `json:"job_id"`

	ExpectedWorkers int `json:"expected_workers"`

	Name          string                    `json:"name"`
	ProjectRoot   string                    `json:"project_root"`
	Entrypoint    string                    `json:"entrypoint"`
	DatasetPath   string                    `json:"dataset_path"`
	Outputs       []string                  `json:"outputs,omitempty"`
	Communication project.CommunicationSpec `json:"communication,omitempty"`
	Workers       []WorkerAssignment        `json:"workers"`
	Shards        []ShardAssignment         `json:"shards"`
	Setup         map[string]WorkerSetup    `json:"setup"`
	Run           map[string]WorkerRun      `json:"run"`

	Status Status `json:"status"`
}

type WorkerRun struct {
	Status       gradient.JobRunStatus `json:"-"`
	ErrorMessage string                `json:"error_message,omitempty"`
	LogPath      string                `json:"log_path,omitempty"`
}

type WorkerFinalResult struct {
	Status       gradient.JobRunStatus `json:"-"`
	ErrorMessage string                `json:"error_message,omitempty"`
	ExitCode     int32                 `json:"exit_code"`
	LogTail      string                `json:"log_tail,omitempty"`
}

type Summary struct {
	JobID      string                       `json:"job_id"`
	Status     Status                       `json:"status"`
	Reason     string                       `json:"reason"`
	FinishedAt time.Time                    `json:"finished_at"`
	Workers    map[string]WorkerFinalResult `json:"workers"`
}

type WorkerSetup struct {
	Status       gradient.JobSetupStatus `json:"-"`
	ErrorMessage string                  `json:"error_message,omitempty"`
	LogPath      string                  `json:"log_path,omitempty"`
}

type WorkerAssignment struct {
	WorkerID string `json:"worker_id"`
	Host     string `json:"host"`
	GRPCPort string `json:"grpc_port"`
}

type ShardAssignment struct {
	WorkerID string `json:"worker_id"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
	Count    int    `json:"count"`
	Path     string `json:"path"`
}

func (value WorkerSetup) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Status       string `json:"status"`
		ErrorMessage string `json:"error_message,omitempty"`
		LogPath      string `json:"log_path,omitempty"`
	}{value.Status.String(), value.ErrorMessage, value.LogPath})
}
func (value WorkerRun) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Status       string `json:"status"`
		ErrorMessage string `json:"error_message,omitempty"`
		LogPath      string `json:"log_path,omitempty"`
	}{value.Status.String(), value.ErrorMessage, value.LogPath})
}
func (value WorkerFinalResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Status       string `json:"status"`
		ErrorMessage string `json:"error_message,omitempty"`
		ExitCode     int32  `json:"exit_code"`
		LogTail      string `json:"log_tail,omitempty"`
	}{value.Status.String(), value.ErrorMessage, value.ExitCode, value.LogTail})
}
