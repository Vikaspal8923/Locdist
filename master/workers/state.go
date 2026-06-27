package workers

import (
	"time"

	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
)

type Availability string

const (
	AvailabilityOnline  Availability = "ONLINE"
	AvailabilityStale   Availability = "STALE"
	AvailabilityOffline Availability = "OFFLINE"
)

type State struct {
	WorkerID     string
	Host         string
	GRPCPort     string
	Status       gradient.WorkerStatus
	JobID        string
	Availability Availability
	LastSeen     time.Time
}
