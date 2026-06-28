package workers

import (
	"encoding/json"
	"time"

	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
)

func (value State) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		WorkerID     string       `json:"worker_id"`
		Host         string       `json:"host"`
		GRPCPort     string       `json:"grpc_port"`
		Status       string       `json:"status"`
		JobID        string       `json:"job_id,omitempty"`
		Availability Availability `json:"availability"`
		LastSeen     time.Time    `json:"last_seen"`
	}{value.WorkerID, value.Host, value.GRPCPort, value.Status.String(), value.JobID, value.Availability, value.LastSeen})
}

type Availability string

const (
	AvailabilityOnline  Availability = "ONLINE"
	AvailabilityStale   Availability = "STALE"
	AvailabilityOffline Availability = "OFFLINE"
)

type State struct {
	WorkerID     string                `json:"worker_id"`
	Host         string                `json:"host"`
	GRPCPort     string                `json:"grpc_port"`
	Status       gradient.WorkerStatus `json:"status"`
	JobID        string                `json:"job_id,omitempty"`
	Availability Availability          `json:"availability"`
	LastSeen     time.Time             `json:"last_seen"`
}
