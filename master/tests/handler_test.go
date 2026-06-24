package tests

import (
	"context"
	"testing"

	"github.com/Vikaspal8923/Locdist/master/aggregator"
	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
	mastergrpc "github.com/Vikaspal8923/Locdist/master/grpc"
)

func TestHandlerSynchronizeGradients(
	t *testing.T,
) {

	aggregatorService := aggregator.New()

	server := mastergrpc.NewMasterServer(
		aggregatorService,
	)

	request := &gradient.GradientSubmission{
		RuntimeVersion: 1,
		JobId:          "job-123",
		WorkerId:       "worker-a",
		Chunks: []*gradient.GradientChunk{
			{},
		},
	}

	response, err := server.SynchronizeGradients(
		context.Background(),
		request,
	)

	if err != nil {
		t.Fatalf(
			"expected no error, got %v",
			err,
		)
	}

	if response.RuntimeVersion != 1 {
		t.Fatalf(
			"unexpected runtime version",
		)
	}

	if response.JobId != "job-123" {
		t.Fatalf(
			"unexpected job id",
		)
	}

	if response.ParticipatingWorkers != 1 {
		t.Fatalf(
			"unexpected participating workers",
		)
	}
}
