package tests

import (
	"context"
	"net"
	"testing"

	"github.com/Vikaspal8923/Locdist/master/aggregator"
	"github.com/Vikaspal8923/Locdist/master/coordinator"
	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
	mastergrpc "github.com/Vikaspal8923/Locdist/master/grpc"
	"github.com/Vikaspal8923/Locdist/master/jobs"
	"github.com/Vikaspal8923/Locdist/master/workers"

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

	aggregatorService := aggregator.New()

	jobManager := jobs.New()

	coordinatorService := coordinator.New(
		aggregatorService,
		jobManager,
		workers.New(),
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

	server := grpcserver.NewServer()
	gradient.RegisterWorkerBridgeServer(
		server,
		mastergrpc.NewMasterServer(
			coordinatorService,
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

	response, err := client.SynchronizeGradients(
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
