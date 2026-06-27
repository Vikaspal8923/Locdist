package scheduler

import (
	"fmt"
	"sort"

	"github.com/Vikaspal8923/Locdist/master/workers"
)

type Assignment struct {
	WorkerID string
	Host     string
	GRPCPort string
}

func SelectOnline(
	states []workers.State,
	required int,
) ([]Assignment, error) {
	if required <= 0 {
		return nil, fmt.Errorf("required worker count must be greater than zero")
	}

	online := make([]workers.State, 0, len(states))
	for _, worker := range states {
		if worker.Availability == workers.AvailabilityOnline {
			online = append(online, worker)
		}
	}

	sort.Slice(online, func(i, j int) bool {
		return online[i].WorkerID < online[j].WorkerID
	})

	if len(online) < required {
		return nil, fmt.Errorf(
			"need %d online workers, only %d available",
			required,
			len(online),
		)
	}

	assignments := make([]Assignment, 0, required)
	for _, worker := range online[:required] {
		assignments = append(assignments, Assignment{
			WorkerID: worker.WorkerID,
			Host:     worker.Host,
			GRPCPort: worker.GRPCPort,
		})
	}
	return assignments, nil
}
