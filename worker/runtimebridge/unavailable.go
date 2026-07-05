package runtimebridge

import (
	"fmt"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
)

type UnavailableSynchronizer struct{}

func (UnavailableSynchronizer) Synchronize(
	request *gradient.GradientSubmission,
) (*gradient.AggregatedGradientResponse, error) {
	return nil, fmt.Errorf("worker is not paired with a master")
}

func (UnavailableSynchronizer) SynchronizeBatch(
	request *gradient.GradientSubmission,
) (*gradient.AggregatedGradientResponse, error) {
	return nil, fmt.Errorf("worker is not paired with a master")
}

func (UnavailableSynchronizer) SynchronizeBatchStream(
	request *gradient.GradientSubmission,
	emit func(*gradient.AggregatedGradientChunkResponse) error,
) error {
	return fmt.Errorf("worker is not paired with a master")
}

func (UnavailableSynchronizer) SynchronizeChunk(
	request *gradient.GradientChunkSubmission,
) (*gradient.AggregatedGradientChunkResponse, error) {
	return nil, fmt.Errorf("worker is not paired with a master")
}
