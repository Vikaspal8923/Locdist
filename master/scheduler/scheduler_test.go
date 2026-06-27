package scheduler

import (
	"testing"

	"github.com/Vikaspal8923/Locdist/master/workers"
)

func TestSelectOnline(t *testing.T) {
	assignments, err := SelectOnline(
		[]workers.State{
			{WorkerID: "worker-c", Availability: workers.AvailabilityOnline},
			{WorkerID: "worker-a", Availability: workers.AvailabilityOnline},
			{WorkerID: "worker-b", Availability: workers.AvailabilityOffline},
		},
		2,
	)
	if err != nil {
		t.Fatalf("select online: %v", err)
	}
	if assignments[0].WorkerID != "worker-a" ||
		assignments[1].WorkerID != "worker-c" {
		t.Fatalf("unexpected assignments: %#v", assignments)
	}
}

func TestSelectOnlineRequiresEnoughWorkers(t *testing.T) {
	_, err := SelectOnline(
		[]workers.State{
			{WorkerID: "worker-a", Availability: workers.AvailabilityOnline},
			{WorkerID: "worker-b", Availability: workers.AvailabilityStale},
		},
		2,
	)
	if err == nil {
		t.Fatal("expected insufficient online workers to fail")
	}
}
