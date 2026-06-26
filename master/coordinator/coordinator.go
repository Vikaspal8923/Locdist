package coordinator

import (
	"fmt"

	"github.com/Vikaspal8923/Locdist/master/aggregator"
	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
	"github.com/Vikaspal8923/Locdist/master/jobs"
)

type Coordinator struct {
	aggregator *aggregator.Service
	jobManager *jobs.Manager
}

func New(
	aggregatorService *aggregator.Service,
	jobManager *jobs.Manager,
) *Coordinator {

	return &Coordinator{
		aggregator: aggregatorService,
		jobManager: jobManager,
	}
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