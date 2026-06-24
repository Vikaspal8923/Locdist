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
