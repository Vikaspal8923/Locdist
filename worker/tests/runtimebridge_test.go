package tests

import (
	"testing"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	internalerrors "github.com/Vikaspal8923/Locdist/worker/internal/errors"
	"github.com/Vikaspal8923/Locdist/worker/runtimebridge"
)

func TestSynchronizeSuccess(t *testing.T) {

	service := runtimebridge.New(
		&FakeSynchronizer{},
	)

	request := &gradient.GradientSubmission{
		RuntimeVersion: 1,
		JobId:          "job-123",
		WorkerId:       "worker-123",
		Chunks: []*gradient.GradientChunk{
			{
				HasGrad:  true,
				Data:     []byte{1, 2, 3, 4},
				ByteSize: 4,
			},
		},
	}

	response, err := service.Synchronize(request)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if response.RuntimeVersion != request.RuntimeVersion {
		t.Fatalf(
			"expected runtime version %d, got %d",
			request.RuntimeVersion,
			response.RuntimeVersion,
		)
	}

	if response.JobId != request.JobId {
		t.Fatalf(
			"expected job id %s, got %s",
			request.JobId,
			response.JobId,
		)
	}

	if response.ParticipatingWorkers != 1 {
		t.Fatalf(
			"expected participating workers 1, got %d",
			response.ParticipatingWorkers,
		)
	}

	if response.AggregationRound != 1 {
		t.Fatalf(
			"expected aggregation round 1, got %d",
			response.AggregationRound,
		)
	}

	if len(response.Chunks) != len(request.Chunks) {
		t.Fatalf(
			"expected %d chunks, got %d",
			len(request.Chunks),
			len(response.Chunks),
		)
	}

	if string(response.Chunks[0].Data) != string(request.Chunks[0].Data) {
		t.Fatal("gradient data changed during identity aggregation")
	}
}

func TestSynchronizeInvalidRuntimeVersion(t *testing.T) {

	service := runtimebridge.New(
		&FakeSynchronizer{},
	)

	request := &gradient.GradientSubmission{
		RuntimeVersion: 0,
		JobId:          "job-123",
		WorkerId:       "worker-123",
		Chunks:         []*gradient.GradientChunk{{}},
	}

	_, err := service.Synchronize(request)

	if err != internalerrors.ErrInvalidRuntimeVersion {
		t.Fatalf(
			"expected %v, got %v",
			internalerrors.ErrInvalidRuntimeVersion,
			err,
		)
	}
}

func TestSynchronizeMissingJobID(t *testing.T) {

	service := runtimebridge.New(
		&FakeSynchronizer{},
	)

	request := &gradient.GradientSubmission{
		RuntimeVersion: 1,
		JobId:          "",
		WorkerId:       "worker-123",
		Chunks:         []*gradient.GradientChunk{{}},
	}

	_, err := service.Synchronize(request)

	if err != internalerrors.ErrMissingJobID {
		t.Fatalf(
			"expected %v, got %v",
			internalerrors.ErrMissingJobID,
			err,
		)
	}
}

func TestSynchronizeMissingWorkerID(t *testing.T) {

	service := runtimebridge.New(
		&FakeSynchronizer{},
	)

	request := &gradient.GradientSubmission{
		RuntimeVersion: 1,
		JobId:          "job-123",
		WorkerId:       "",
		Chunks:         []*gradient.GradientChunk{{}},
	}

	_, err := service.Synchronize(request)

	if err != internalerrors.ErrMissingWorkerID {
		t.Fatalf(
			"expected %v, got %v",
			internalerrors.ErrMissingWorkerID,
			err,
		)
	}
}

func TestSynchronizeMissingChunks(t *testing.T) {

	service := runtimebridge.New(
		&FakeSynchronizer{},
	)

	request := &gradient.GradientSubmission{
		RuntimeVersion: 1,
		JobId:          "job-123",
		WorkerId:       "worker-123",
		Chunks:         nil,
	}

	_, err := service.Synchronize(request)

	if err != internalerrors.ErrMissingChunks {
		t.Fatalf(
			"expected %v, got %v",
			internalerrors.ErrMissingChunks,
			err,
		)
	}
}

func TestSynchronizeChunkSuccess(t *testing.T) {
	service := runtimebridge.New(&FakeSynchronizer{})
	request := &gradient.GradientChunkSubmission{
		RuntimeVersion: 1,
		JobId:          "job-123",
		WorkerId:       "worker-123",
		Chunk: &gradient.GradientChunk{
			Metadata: &gradient.ParameterMetadata{
				Name:       "layer",
				Shape:      []int64{2},
				Numel:      2,
				Dtype:      "torch.float32",
				LayerOrder: 1,
			},
			HasGrad:   true,
			Data:      []byte{1, 2, 3, 4},
			ByteSize:  4,
			SyncRound: 7,
		},
	}

	response, err := service.SynchronizeChunk(request)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if response.GetAggregationRound() != 1 {
		t.Fatalf("expected aggregation round 1, got %d", response.GetAggregationRound())
	}
	if response.GetChunk().GetMetadata().GetLayerOrder() != 1 {
		t.Fatalf("expected layer order 1, got %d", response.GetChunk().GetMetadata().GetLayerOrder())
	}
	if response.GetChunk().GetSyncRound() != 7 {
		t.Fatalf("expected sync round 7, got %d", response.GetChunk().GetSyncRound())
	}
}

func TestSynchronizeBatchSuccess(t *testing.T) {
	service := runtimebridge.New(&FakeSynchronizer{})
	request := &gradient.GradientSubmission{
		RuntimeVersion: 1,
		JobId:          "job-123",
		WorkerId:       "worker-123",
		Chunks: []*gradient.GradientChunk{
			{
				Metadata: &gradient.ParameterMetadata{
					Name:       "layer-1",
					Shape:      []int64{2},
					Numel:      2,
					Dtype:      "torch.float32",
					LayerOrder: 1,
				},
				HasGrad:   true,
				Data:      []byte{1, 2},
				ByteSize:  2,
				SyncRound: 7,
			},
			{
				Metadata: &gradient.ParameterMetadata{
					Name:       "layer-2",
					Shape:      []int64{2},
					Numel:      2,
					Dtype:      "torch.float32",
					LayerOrder: 2,
				},
				HasGrad:   true,
				Data:      []byte{3, 4},
				ByteSize:  2,
				SyncRound: 7,
			},
		},
	}

	response, err := service.SynchronizeBatch(request)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(response.GetChunks()) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(response.GetChunks()))
	}
	if response.GetChunks()[0].GetMetadata().GetLayerOrder() != 1 {
		t.Fatalf("expected first layer order 1, got %d", response.GetChunks()[0].GetMetadata().GetLayerOrder())
	}
	if response.GetChunks()[1].GetMetadata().GetLayerOrder() != 2 {
		t.Fatalf("expected second layer order 2, got %d", response.GetChunks()[1].GetMetadata().GetLayerOrder())
	}
}

func TestSynchronizeBatchStreamSuccess(t *testing.T) {
	service := runtimebridge.New(&FakeSynchronizer{})
	request := &gradient.GradientSubmission{
		RuntimeVersion: 1,
		JobId:          "job-123",
		WorkerId:       "worker-123",
		Chunks: []*gradient.GradientChunk{
			{
				Metadata: &gradient.ParameterMetadata{
					Name:       "layer-1",
					Shape:      []int64{2},
					Numel:      2,
					Dtype:      "torch.float32",
					LayerOrder: 1,
				},
				HasGrad:   true,
				Data:      []byte{1, 2},
				ByteSize:  2,
				SyncRound: 7,
			},
			{
				Metadata: &gradient.ParameterMetadata{
					Name:       "layer-2",
					Shape:      []int64{2},
					Numel:      2,
					Dtype:      "torch.float32",
					LayerOrder: 2,
				},
				HasGrad:   true,
				Data:      []byte{3, 4},
				ByteSize:  2,
				SyncRound: 7,
			},
		},
	}

	var layerOrders []uint32
	err := service.SynchronizeBatchStream(
		request,
		func(response *gradient.AggregatedGradientChunkResponse) error {
			layerOrders = append(layerOrders, response.GetChunk().GetMetadata().GetLayerOrder())
			return nil
		},
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(layerOrders) != 2 {
		t.Fatalf("expected 2 streamed chunks, got %d", len(layerOrders))
	}
}
