package orchestrator

import (
	"fmt"
	"os"
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
	if p.jobManager.HasActiveJob() {
		return nil, fmt.Errorf("a job is already active")
	}
	cleanRoot := filepath.Clean(p.jobsRoot)
	if cleanRoot == "." || cleanRoot == string(filepath.Separator) || cleanRoot == filepath.Clean(projectRoot) {
		return nil, fmt.Errorf("jobs root is unsafe")
	}
	if err := os.RemoveAll(cleanRoot); err != nil {
		return nil, fmt.Errorf("clear previous Master job data: %w", err)
	}
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

	shards, err := shardDataset(projectRoot, spec, filepath.Join(p.jobsRoot, jobID, "shards"), workerIDs)
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
			Kind:     shard.Kind,
		})
	}

	job := jobs.JobState{
		JobID:           jobID,
		Name:            spec.Job.Name,
		ProjectRoot:     projectRoot,
		Entrypoint:      spec.Entrypoint,
		DatasetPath:     spec.Dataset.Train,
		Outputs:         append([]string(nil), spec.Outputs...),
		Communication:   spec.Communication,
		Training:        spec.Training,
		ExpectedWorkers: spec.Workers.Count,
		Workers:         workersForJob,
		Shards:          shardsForJob,
	}
	if err := p.jobManager.PrepareJob(job); err != nil {
		return nil, err
	}
	return p.jobManager.CurrentJob()
}

func shardDataset(projectRoot string, spec project.Spec, outputRoot string, workerIDs []string) ([]sharder.Assignment, error) {
	datasetType := spec.Dataset.Type
	if datasetType == "" {
		datasetType = "jsonl"
	}
	sourcePath := filepath.Join(projectRoot, spec.Dataset.Train)
	switch datasetType {
	case "jsonl":
		return sharder.ShardJSONL(sourcePath, spec.Dataset.Train, outputRoot, workerIDs)
	case "image_folder":
		return sharder.ShardImageFolder(sourcePath, spec.Dataset.Train, outputRoot, workerIDs)
	case "yolo_split":
		return sharder.ShardYOLOSplit(sourcePath, spec.Dataset.Train, outputRoot, workerIDs)
	default:
		return nil, fmt.Errorf("unsupported dataset.type %q", datasetType)
	}
}

func newJobID() string {
	return fmt.Sprintf("job-%d", time.Now().UnixNano())
}
