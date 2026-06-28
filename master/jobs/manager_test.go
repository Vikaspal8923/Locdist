package jobs

import (
	"testing"

	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
)

func TestAllWorkersReadyRequiresEveryAssignment(t *testing.T) {
	manager := New()
	job := JobState{JobID: "job-1", ExpectedWorkers: 2, Workers: []WorkerAssignment{{WorkerID: "one"}, {WorkerID: "two"}}, Shards: []ShardAssignment{{WorkerID: "one"}, {WorkerID: "two"}}}
	if err := manager.PrepareJob(job); err != nil {
		t.Fatal(err)
	}
	if manager.AllWorkersReady("job-1") {
		t.Fatal("new job must not be ready")
	}
	ready := WorkerSetup{Status: gradient.JobSetupStatus_JOB_SETUP_STATUS_READY}
	if err := manager.UpdateSetup("job-1", "one", ready); err != nil {
		t.Fatal(err)
	}
	if manager.AllWorkersReady("job-1") {
		t.Fatal("one ready Worker must not unlock training")
	}
	if err := manager.UpdateSetup("job-1", "two", ready); err != nil {
		t.Fatal(err)
	}
	if !manager.AllWorkersReady("job-1") {
		t.Fatal("all ready Workers should unlock training")
	}
}
