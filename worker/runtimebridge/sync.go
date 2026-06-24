package runtimebridge

import (
	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	internalerrors "github.com/Vikaspal8923/Locdist/worker/internal/errors"
)

type Service struct {
	synchronizer Synchronizer
}

func New(
	synchronizer Synchronizer,
) *Service {

	return &Service{
		synchronizer: synchronizer,
	}
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

	return s.synchronizer.Synchronize(
		request,
	)
}
