package coordinator

import (
	"fmt"
	"time"

	"github.com/Vikaspal8923/Locdist/master/aggregator"
	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
	"github.com/Vikaspal8923/Locdist/master/jobs"
	"github.com/Vikaspal8923/Locdist/master/workers"
)

type Coordinator struct {
	aggregator    *aggregator.Service
	jobManager    *jobs.Manager
	workerManager *workers.Manager
}

func (c *Coordinator) Heartbeat(
	request *gradient.WorkerHeartbeat,
) (*gradient.WorkerHeartbeatResponse, error) {
	if _, err := c.workerManager.Heartbeat(request); err != nil {
		return nil, err
	}
	return &gradient.WorkerHeartbeatResponse{
		Accepted:       true,
		ServerUnixTime: time.Now().Unix(),
	}, nil
}

func (c *Coordinator) GoingOffline(
	request *gradient.WorkerOfflineRequest,
) (*gradient.WorkerOfflineResponse, error) {
	if err := c.workerManager.GoingOffline(request); err != nil {
		return nil, err
	}
	return &gradient.WorkerOfflineResponse{Acknowledged: true}, nil
}

func New(
	aggregatorService *aggregator.Service,
	jobManager *jobs.Manager,
	workerManager *workers.Manager,
) *Coordinator {

	return &Coordinator{
		aggregator:    aggregatorService,
		jobManager:    jobManager,
		workerManager: workerManager,
	}
}

func (c *Coordinator) RegisterWorker(
	request *gradient.RegisterWorkerRequest,
) (*gradient.RegisterWorkerResponse, error) {

	worker, err := c.workerManager.Register(request)
	if err != nil {
		return nil, err
	}

	return &gradient.RegisterWorkerResponse{
		WorkerId:   worker.WorkerID,
		Registered: true,
	}, nil
}

func (c *Coordinator) UpdateWorkerStatus(
	request *gradient.WorkerStatusUpdate,
) (*gradient.WorkerStatusResponse, error) {

	worker, err := c.workerManager.UpdateStatus(request)
	if err != nil {
		return nil, err
	}

	return &gradient.WorkerStatusResponse{
		WorkerId: worker.WorkerID,
		Status:   worker.Status,
	}, nil
}

func (c *Coordinator) UnpairWorker(
	request *gradient.UnpairWorkerRequest,
) (*gradient.UnpairWorkerResponse, error) {
	if err := c.workerManager.RevokeAuthenticated(request); err != nil {
		return nil, err
	}
	return &gradient.UnpairWorkerResponse{Unpaired: true}, nil
}

func (c *Coordinator) StartTraining(
	jobID string,
	expectedWorkers int,
) error {

	if expectedWorkers <= 0 {
		return fmt.Errorf("expected workers must be greater than zero")
	}

	return c.jobManager.StartJob(
		jobID,
		expectedWorkers,
	)
}

func (c *Coordinator) SynchronizeGradients(
	request *gradient.GradientSubmission,
) (*gradient.AggregatedGradientResponse, error) {

	job, err := c.jobManager.CurrentJob()
	if err != nil {
		return nil, err
	}

	return c.aggregator.Aggregate(
		request,
		job.ExpectedWorkers,
	)
}

func (c *Coordinator) SynchronizeGradientChunk(
	request *gradient.GradientChunkSubmission,
) (*gradient.AggregatedGradientChunkResponse, error) {
	job, err := c.jobManager.CurrentJob()
	if err != nil {
		return nil, err
	}

	if request.GetJobId() != job.JobID {
		return nil, fmt.Errorf("job %q is not active", request.GetJobId())
	}
	if !workerAssigned(job, request.GetWorkerId()) {
		return nil, fmt.Errorf("worker %q is not assigned to job", request.GetWorkerId())
	}

	return c.aggregator.AggregateChunk(
		request,
		job.ExpectedWorkers,
	)
}

func (c *Coordinator) SynchronizeGradientBatch(
	request *gradient.GradientSubmission,
) (*gradient.AggregatedGradientResponse, error) {
	job, err := c.jobManager.CurrentJob()
	if err != nil {
		return nil, err
	}
	if request.GetJobId() != job.JobID {
		return nil, fmt.Errorf("job %q is not active", request.GetJobId())
	}
	if !workerAssigned(job, request.GetWorkerId()) {
		return nil, fmt.Errorf("worker %q is not assigned to job", request.GetWorkerId())
	}
	return c.aggregator.AggregateChunkBatch(
		request,
		job.ExpectedWorkers,
	)
}

func (c *Coordinator) SynchronizeGradientBatchStream(
	request *gradient.GradientSubmission,
	emit func(*gradient.AggregatedGradientChunkResponse) error,
) error {
	job, err := c.jobManager.CurrentJob()
	if err != nil {
		return err
	}
	if request.GetJobId() != job.JobID {
		return fmt.Errorf("job %q is not active", request.GetJobId())
	}
	if !workerAssigned(job, request.GetWorkerId()) {
		return fmt.Errorf("worker %q is not assigned to job", request.GetWorkerId())
	}
	return c.aggregator.StreamChunkBatch(
		request,
		job.ExpectedWorkers,
		emit,
	)
}

func workerAssigned(job *jobs.JobState, workerID string) bool {
	for _, worker := range job.Workers {
		if worker.WorkerID == workerID {
			return true
		}
	}
	return false
}
