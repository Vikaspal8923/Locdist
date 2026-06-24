package tests

import (
	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
)

type FakeSynchronizer struct{}

func (f *FakeSynchronizer) Synchronize(
	request *gradient.GradientSubmission,
) (*gradient.AggregatedGradientResponse, error) {

	return &gradient.AggregatedGradientResponse{
		RuntimeVersion:       request.GetRuntimeVersion(),
		JobId:                request.GetJobId(),
		ParticipatingWorkers: 1,
		AggregationRound:     1,
		Chunks:               request.GetChunks(),
	}, nil
}
