package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/Vikaspal8923/Locdist/master/jobs"
	"github.com/Vikaspal8923/Locdist/master/workers"
)

func TestTrainingStartRequiresEveryWorkerReady(t *testing.T) {
	jobManager := jobs.New()
	job := jobs.JobState{
		JobID: "job-1", ExpectedWorkers: 2,
		Workers: []jobs.WorkerAssignment{{WorkerID: "one"}, {WorkerID: "two"}},
		Shards:  []jobs.ShardAssignment{{WorkerID: "one"}, {WorkerID: "two"}},
	}
	if err := jobManager.PrepareJob(job); err != nil {
		t.Fatal(err)
	}
	coordinator := NewTrainingCoordinator(jobManager, workers.New())
	err := coordinator.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "must be ready") {
		t.Fatalf("unexpected error: %v", err)
	}
}
