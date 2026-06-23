package runtimebridge

import (
	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	internalerrors "github.com/Vikaspal8923/Locdist/worker/internal/errors"
)

type Service struct{}

func New() *Service {
	return &Service{}
}

func (s *Service) Synchronize(
	request *gradient.GradientSubmission,
) (*gradient.AggregatedGradientResponse, error) {

	if request.GetRuntimeVersion() == 0 {
		return nil, internalerrors.ErrInvalidRuntimeVersion
	}

	if request.GetJobId() == "" {
		return nil, internalerrors.ErrMissingJobID
	}

	if request.GetWorkerId() == "" {
		return nil, internalerrors.ErrMissingWorkerID
	}

	if len(request.GetChunks()) == 0 {
		return nil, internalerrors.ErrMissingChunks
	}

	response := &gradient.AggregatedGradientResponse{
		RuntimeVersion:       request.GetRuntimeVersion(),
		JobId:                request.GetJobId(),
		ParticipatingWorkers: 1,
		AggregationRound:     1,
		Chunks:               request.GetChunks(),
	}

	return response, nil
}