package tests

import (
	"context"
	"net"
	"testing"

	"github.com/Vikaspal8923/Locdist/master/aggregator"
	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
	mastergrpc "github.com/Vikaspal8923/Locdist/master/grpc"

	grpcclient "google.golang.org/grpc"
	grpcserver "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestMasterIntegration(
	t *testing.T,
) {

	listener, err := net.Listen(
		"tcp",
		"127.0.0.1:0",
	)
	if err != nil {
		t.Fatalf(
			"failed to create listener: %v",
			err,
		)
	}

	server := grpcserver.NewServer()

	aggregatorService := aggregator.New()

	gradient.RegisterWorkerBridgeServer(
		server,
		mastergrpc.NewMasterServer(
			aggregatorService,
		),
	)

	go server.Serve(listener)

	defer server.Stop()

	conn, err := grpcclient.NewClient(
		listener.Addr().String(),
		grpcclient.WithTransportCredentials(
			insecure.NewCredentials(),
		),
	)

	if err != nil {
		t.Fatalf(
			"failed to connect: %v",
			err,
		)
	}

	defer conn.Close()

	client := gradient.NewWorkerBridgeClient(
		conn,
	)

	request := &gradient.GradientSubmission{
		RuntimeVersion: 1,
		JobId:          "job-123",
		WorkerId:       "worker-a",
		Chunks: []*gradient.GradientChunk{
			{},
		},
	}

	response, err := client.SynchronizeGradients(
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

	if response.AggregationRound != 1 {
		t.Fatalf(
			"unexpected aggregation round",
		)
	}

	if len(response.Chunks) != 1 {
		t.Fatalf(
			"unexpected chunk count",
		)
	}
}
