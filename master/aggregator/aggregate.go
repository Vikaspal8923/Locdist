package aggregator

import (
	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
	internalerrors "github.com/Vikaspal8923/Locdist/master/internal/errors"
)

type Service struct{}

func New() *Service {
	return &Service{}
}

func (s *Service) Aggregate(
	request *gradient.GradientSubmission,
) (*gradient.AggregatedGradientResponse, error) {

	if request.RuntimeVersion == 0 {
		return nil, internalerrors.ErrInvalidRuntimeVersion
	}

	if request.JobId == "" {
		return nil, internalerrors.ErrMissingJobID
	}

	if request.WorkerId == "" {
		return nil, internalerrors.ErrMissingWorkerID
	}

	if len(request.Chunks) == 0 {
		return nil, internalerrors.ErrMissingChunks
	}

	response := &gradient.AggregatedGradientResponse{
		RuntimeVersion:       request.RuntimeVersion,
		JobId:                request.JobId,
		ParticipatingWorkers: 1,
		AggregationRound:     1,
		Chunks:               request.Chunks,
	}

	return response, nil
}
