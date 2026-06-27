package tests

import (
	"context"
	"net"
	"strconv"
	"testing"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	workerconfig "github.com/Vikaspal8923/Locdist/worker/internal/config"
	"github.com/Vikaspal8923/Locdist/worker/masterclient"

	grpcserver "google.golang.org/grpc"
)

type fakeMasterServer struct {
	gradient.UnimplementedWorkerBridgeServer
}

func (s *fakeMasterServer) RegisterWorker(
	ctx context.Context,
	request *gradient.RegisterWorkerRequest,
) (*gradient.RegisterWorkerResponse, error) {
	return &gradient.RegisterWorkerResponse{
		WorkerId:   request.GetWorkerId(),
		Registered: true,
	}, nil
}

func (s *fakeMasterServer) UpdateWorkerStatus(
	ctx context.Context,
	request *gradient.WorkerStatusUpdate,
) (*gradient.WorkerStatusResponse, error) {
	return &gradient.WorkerStatusResponse{
		WorkerId: request.GetWorkerId(),
		Status:   request.GetStatus(),
	}, nil
}

func (s *fakeMasterServer) SynchronizeGradients(
	ctx context.Context,
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

func TestMasterClient(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	server := grpcserver.NewServer()
	gradient.RegisterWorkerBridgeServer(server, &fakeMasterServer{})
	go server.Serve(listener)
	defer server.Stop()

	cfg := workerconfig.Config{
		MasterHost: "127.0.0.1",
		MasterPort: strconv.Itoa(listener.Addr().(*net.TCPAddr).Port),
	}

	client, err := masterclient.New(cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	registration, err := client.Register(
		&gradient.RegisterWorkerRequest{
			WorkerId: "worker-a",
			Host:     "127.0.0.1",
			GrpcPort: "50051",
		},
	)
	if err != nil {
		t.Fatalf("register worker: %v", err)
	}
	if !registration.GetRegistered() {
		t.Fatal("expected registration to succeed")
	}

	statusResponse, err := client.UpdateStatus(
		&gradient.WorkerStatusUpdate{
			WorkerId: "worker-a",
			Status:   gradient.WorkerStatus_WORKER_STATUS_IDLE,
		},
	)
	if err != nil {
		t.Fatalf("update status: %v", err)
	}
	if statusResponse.GetStatus() != gradient.WorkerStatus_WORKER_STATUS_IDLE {
		t.Fatalf("unexpected worker status: %s", statusResponse.GetStatus())
	}

	request := &gradient.GradientSubmission{
		RuntimeVersion: 1,
		JobId:          "job-123",
		WorkerId:       "worker-a",
		Chunks: []*gradient.GradientChunk{
			{},
		},
	}

	response, err := client.Synchronize(request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if response.GetRuntimeVersion() != 1 {
		t.Fatal("unexpected runtime version")
	}
	if response.GetJobId() != "job-123" {
		t.Fatal("unexpected job id")
	}
	if response.GetParticipatingWorkers() != 1 {
		t.Fatal("unexpected worker count")
	}
}
