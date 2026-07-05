package runtimebridge

import gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"

type Synchronizer interface {
	Synchronize(
		request *gradient.GradientSubmission,
	) (*gradient.AggregatedGradientResponse, error)
	SynchronizeBatch(
		request *gradient.GradientSubmission,
	) (*gradient.AggregatedGradientResponse, error)
	SynchronizeBatchStream(
		request *gradient.GradientSubmission,
		emit func(*gradient.AggregatedGradientChunkResponse) error,
	) error
	SynchronizeChunk(
		request *gradient.GradientChunkSubmission,
	) (*gradient.AggregatedGradientChunkResponse, error)
}
