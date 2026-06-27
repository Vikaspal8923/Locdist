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
