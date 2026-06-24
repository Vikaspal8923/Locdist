package tests

import (
	"context"
	"net"
	"testing"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	workergrpc "github.com/Vikaspal8923/Locdist/worker/grpc"
	"github.com/Vikaspal8923/Locdist/worker/runtimebridge"
	grpcserver "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestRuntimeWorkerIntegration(t *testing.T) {

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

	runtimeBridge := runtimebridge.New(
		&FakeSynchronizer{},
	)

	gradient.RegisterWorkerBridgeServer(
		server,
		workergrpc.NewWorkerBridgeServer(
			runtimeBridge,
		),
	)

	go func() {
		_ = server.Serve(listener)
	}()

	defer server.GracefulStop()

	conn, err := grpcserver.NewClient(
		listener.Addr().String(),
		grpcserver.WithTransportCredentials(
			insecure.NewCredentials(),
		),
	)
	if err != nil {
		t.Fatalf(
			"failed to create client: %v",
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
		WorkerId:       "worker-123",
		Chunks: []*gradient.GradientChunk{
			{
				HasGrad:  true,
				Data:     []byte{1, 2, 3, 4},
				ByteSize: 4,
			},
		},
	}

	response, err := client.SynchronizeGradients(
		context.Background(),
		request,
	)
	if err != nil {
		t.Fatalf(
			"rpc failed: %v",
			err,
		)
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
		t.Fatal(
			"gradient data changed during end-to-end flow",
		)
	}
}
