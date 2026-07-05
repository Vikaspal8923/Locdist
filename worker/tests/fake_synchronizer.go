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

func (f *FakeSynchronizer) SynchronizeBatch(
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

func (f *FakeSynchronizer) SynchronizeBatchStream(
	request *gradient.GradientSubmission,
	emit func(*gradient.AggregatedGradientChunkResponse) error,
) error {
	for _, chunk := range request.GetChunks() {
		if err := emit(&gradient.AggregatedGradientChunkResponse{
			RuntimeVersion:       request.GetRuntimeVersion(),
			JobId:                request.GetJobId(),
			ParticipatingWorkers: 1,
			AggregationRound:     chunk.GetSyncRound(),
			Chunk:                chunk,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (f *FakeSynchronizer) SynchronizeChunk(
	request *gradient.GradientChunkSubmission,
) (*gradient.AggregatedGradientChunkResponse, error) {
	return &gradient.AggregatedGradientChunkResponse{
		RuntimeVersion:       request.GetRuntimeVersion(),
		JobId:                request.GetJobId(),
		ParticipatingWorkers: 1,
		AggregationRound:     1,
		Chunk:                request.GetChunk(),
	}, nil
}
