package tests

import (
	"context"
	"testing"

	"github.com/Vikaspal8923/Locdist/master/aggregator"
	"github.com/Vikaspal8923/Locdist/master/coordinator"
	mastergrpc "github.com/Vikaspal8923/Locdist/master/grpc"
	"github.com/Vikaspal8923/Locdist/master/jobs"
)

func TestHandlerSynchronizeGradients(
	t *testing.T,
) {

	aggregatorService := aggregator.New()

	jobManager := jobs.New()

	coordinatorService := coordinator.New(
		aggregatorService,
		jobManager,
	)

	if err := coordinatorService.StartTraining(
		"job-123",
		1,
	); err != nil {
		t.Fatalf(
			"failed to start training: %v",
			err,
		)
	}

	server := mastergrpc.NewMasterServer(
		coordinatorService,
	)

	response, err := server.SynchronizeGradients(
		context.Background(),
		gradientSubmission(
			"worker-a",
			[]float32{1, 3},
		),
	)

	if err != nil {
		t.Fatalf(
			"expected no error, got %v",
			err,
		)
	}

	assertResponse(t, response, 1, 1, []float32{1, 3})
}
