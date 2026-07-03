package runtimebridge

import (
	"sync"
	"time"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	internalerrors "github.com/Vikaspal8923/Locdist/worker/internal/errors"
)

type Service struct {
	mu           sync.RWMutex
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
	response, _, err := s.SynchronizeWithMetrics(request)
	return response, err
}

func (s *Service) SynchronizeWithMetrics(
	request *gradient.GradientSubmission,
) (*gradient.AggregatedGradientResponse, map[string]any, error) {
	start := time.Now()

	if request.GetRuntimeVersion() == 0 {
		return nil, nil, internalerrors.ErrInvalidRuntimeVersion
	}

	if request.GetJobId() == "" {
		return nil, nil, internalerrors.ErrMissingJobID
	}

	if request.GetWorkerId() == "" {
		return nil, nil, internalerrors.ErrMissingWorkerID
	}

	if len(request.GetChunks()) == 0 {
		return nil, nil, internalerrors.ErrMissingChunks
	}
	validationDone := time.Now()

	s.mu.RLock()
	synchronizer := s.synchronizer
	s.mu.RUnlock()

	response, err := synchronizer.Synchronize(
		request,
	)
	done := time.Now()
	return response, map[string]any{
		"worker_bridge_validation_ms": float64(validationDone.Sub(start).Microseconds()) / 1000.0,
		"worker_to_master_rpc_ms":     float64(done.Sub(validationDone).Microseconds()) / 1000.0,
	}, err
}

func (s *Service) SetSynchronizer(synchronizer Synchronizer) {
	s.mu.Lock()
	s.synchronizer = synchronizer
	s.mu.Unlock()
}
