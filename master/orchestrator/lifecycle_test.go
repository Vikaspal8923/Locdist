package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
	"github.com/Vikaspal8923/Locdist/master/jobs"
	"github.com/Vikaspal8923/Locdist/master/workers"
)

func TestDisconnectedWorkerFailsCleansAndResetsJob(t *testing.T) {
	jobManager := jobs.New()
	job := jobs.JobState{JobID: "job-1", ExpectedWorkers: 1, Workers: []jobs.WorkerAssignment{{WorkerID: "worker-1"}}, Shards: []jobs.ShardAssignment{{WorkerID: "worker-1"}}}
	if err := jobManager.PrepareJob(job); err != nil {
		t.Fatal(err)
	}
	if err := jobManager.UpdateSetup("job-1", "worker-1", jobs.WorkerSetup{Status: gradient.JobSetupStatus_JOB_SETUP_STATUS_READY}); err != nil {
		t.Fatal(err)
	}
	if err := jobManager.SetStatus("job-1", jobs.StatusRunning); err != nil {
		t.Fatal(err)
	}
	jobsRoot := filepath.Join(t.TempDir(), "ldgcc_jobs")
	jobPath := filepath.Join(jobsRoot, "job-1")
	if err := os.MkdirAll(jobPath, 0o700); err != nil {
		t.Fatal(err)
	}

	coordinator := NewLifecycleCoordinator(jobManager, workers.New(), jobsRoot)
	summary, err := coordinator.Monitor(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != jobs.StatusFailed {
		t.Fatalf("status = %s", summary.Status)
	}
	if _, err := jobManager.CurrentJob(); err == nil {
		t.Fatal("active job was not reset")
	}
	if _, err := os.Stat(jobPath); !os.IsNotExist(err) {
		t.Fatal("Master job data was not removed")
	}
	if archived, ok := jobManager.LastSummary(); !ok || archived.JobID != "job-1" {
		t.Fatal("failure summary was not archived")
	}
}

func TestTrainingFailurePreservesJobForRetry(t *testing.T) {
	jobManager := jobs.New()
	job := jobs.JobState{JobID: "job-1", ExpectedWorkers: 1, Workers: []jobs.WorkerAssignment{{WorkerID: "worker-1"}}, Shards: []jobs.ShardAssignment{{WorkerID: "worker-1"}}}
	if err := jobManager.PrepareJob(job); err != nil {
		t.Fatal(err)
	}
	if err := jobManager.UpdateSetup("job-1", "worker-1", jobs.WorkerSetup{Status: gradient.JobSetupStatus_JOB_SETUP_STATUS_READY}); err != nil {
		t.Fatal(err)
	}
	if err := jobManager.SetStatus("job-1", jobs.StatusRunning); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	jobsRoot := filepath.Join(root, "ldgcc_jobs")
	jobPath := filepath.Join(jobsRoot, "job-1")
	if err := os.MkdirAll(jobPath, 0o700); err != nil {
		t.Fatal(err)
	}

	coordinator := NewLifecycleCoordinator(jobManager, workers.New(), jobsRoot)
	summary, err := coordinator.finalize(context.Background(), &job, jobs.StatusFailed, "worker failed", map[string]*gradient.JobCommandResponse{
		"worker-1": {
			Status:       gradient.JobRunStatus_JOB_RUN_STATUS_FAILED,
			ErrorMessage: "exit status 1",
			ExitCode:     1,
			LogTail:      []byte("traceback"),
		},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != jobs.StatusFailed {
		t.Fatalf("status = %s", summary.Status)
	}
	current, err := jobManager.CurrentJob()
	if err != nil {
		t.Fatal("active job was not preserved")
	}
	if current.Status != jobs.StatusPrepared {
		t.Fatalf("current status = %s", current.Status)
	}
	if current.Setup["worker-1"].Status != gradient.JobSetupStatus_JOB_SETUP_STATUS_READY {
		t.Fatalf("setup status = %s", current.Setup["worker-1"].Status)
	}
	if current.Run["worker-1"].Status != gradient.JobRunStatus_JOB_RUN_STATUS_UNKNOWN {
		t.Fatalf("run status = %s", current.Run["worker-1"].Status)
	}
	if _, err := os.Stat(jobPath); err != nil {
		t.Fatalf("Master job data was removed: %v", err)
	}
	if archived, ok := jobManager.LastSummary(); !ok || archived.JobID != "job-1" || archived.Status != jobs.StatusFailed {
		t.Fatal("failure summary was not kept")
	}
	if _, err := os.Stat(filepath.Join(root, "ldgcc_results", "job-1", "summary.json")); err != nil {
		t.Fatalf("summary was not written: %v", err)
	}
}
