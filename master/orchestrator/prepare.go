package orchestrator

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/Vikaspal8923/Locdist/master/jobs"
	"github.com/Vikaspal8923/Locdist/master/project"
	"github.com/Vikaspal8923/Locdist/master/scheduler"
	"github.com/Vikaspal8923/Locdist/master/sharder"
	"github.com/Vikaspal8923/Locdist/master/workers"
)

type Preparer struct {
	jobManager    *jobs.Manager
	workerManager *workers.Manager
	jobsRoot      string
}

func NewPreparer(
	jobManager *jobs.Manager,
	workerManager *workers.Manager,
	jobsRoot string,
) *Preparer {
	return &Preparer{
		jobManager:    jobManager,
		workerManager: workerManager,
		jobsRoot:      jobsRoot,
	}
}

func (p *Preparer) Prepare(projectRoot string) (*jobs.JobState, error) {
	spec, err := project.LoadSpec(projectRoot)
	if err != nil {
		return nil, err
	}

	selected, err := scheduler.SelectOnline(
		p.workerManager.States(),
		spec.Workers.Count,
	)
	if err != nil {
		return nil, err
	}

	jobID := newJobID()
	workerIDs := make([]string, 0, len(selected))
	workersForJob := make([]jobs.WorkerAssignment, 0, len(selected))
	for _, worker := range selected {
		workerIDs = append(workerIDs, worker.WorkerID)
		workersForJob = append(workersForJob, jobs.WorkerAssignment{
			WorkerID: worker.WorkerID,
			Host:     worker.Host,
			GRPCPort: worker.GRPCPort,
		})
	}

	shards, err := sharder.ShardJSONL(
		filepath.Join(projectRoot, spec.Dataset.Train),
		spec.Dataset.Train,
		filepath.Join(p.jobsRoot, jobID, "shards"),
		workerIDs,
	)
	if err != nil {
		return nil, err
	}

	shardsForJob := make([]jobs.ShardAssignment, 0, len(shards))
	for _, shard := range shards {
		shardsForJob = append(shardsForJob, jobs.ShardAssignment{
			WorkerID: shard.WorkerID,
			Start:    shard.Start,
			End:      shard.End,
			Count:    shard.Count,
			Path:     shard.Path,
		})
	}

	job := jobs.JobState{
		JobID:           jobID,
		Name:            spec.Job.Name,
		ProjectRoot:     projectRoot,
		Entrypoint:      spec.Entrypoint,
		DatasetPath:     spec.Dataset.Train,
		ExpectedWorkers: spec.Workers.Count,
		Workers:         workersForJob,
		Shards:          shardsForJob,
	}
	if err := p.jobManager.PrepareJob(job); err != nil {
		return nil, err
	}
	return p.jobManager.CurrentJob()
}

func newJobID() string {
	return fmt.Sprintf("job-%d", time.Now().UnixNano())
}
