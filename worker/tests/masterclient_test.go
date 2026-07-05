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

func (s *fakeMasterServer) SynchronizeGradientBatch(
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

func (s *fakeMasterServer) SynchronizeGradientBatchStream(
	request *gradient.GradientSubmission,
	stream gradient.WorkerBridge_SynchronizeGradientBatchStreamServer,
) error {
	for _, chunk := range request.GetChunks() {
		if err := stream.Send(&gradient.AggregatedGradientChunkResponse{
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

func (s *fakeMasterServer) SynchronizeGradientChunk(
	ctx context.Context,
	request *gradient.GradientChunkSubmission,
) (*gradient.AggregatedGradientChunkResponse, error) {
	return &gradient.AggregatedGradientChunkResponse{
		RuntimeVersion:       request.GetRuntimeVersion(),
		JobId:                request.GetJobId(),
		ParticipatingWorkers: 1,
		AggregationRound:     request.GetChunk().GetSyncRound(),
		Chunk:                request.GetChunk(),
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

	batchResponse, err := client.SynchronizeBatch(
		&gradient.GradientSubmission{
			RuntimeVersion: 1,
			JobId:          "job-123",
			WorkerId:       "worker-a",
			Chunks: []*gradient.GradientChunk{
				{
					Metadata: &gradient.ParameterMetadata{
						Name:       "layer-a",
						Shape:      []int64{2},
						Numel:      2,
						Dtype:      "torch.float32",
						LayerOrder: 1,
					},
					HasGrad:   true,
					Data:      []byte{1, 2},
					ByteSize:  2,
					SyncRound: 4,
				},
				{
					Metadata: &gradient.ParameterMetadata{
						Name:       "layer-b",
						Shape:      []int64{2},
						Numel:      2,
						Dtype:      "torch.float32",
						LayerOrder: 2,
					},
					HasGrad:   true,
					Data:      []byte{3, 4},
					ByteSize:  2,
					SyncRound: 4,
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("unexpected batch error: %v", err)
	}
	if len(batchResponse.GetChunks()) != 2 {
		t.Fatalf("expected 2 batch chunks, got %d", len(batchResponse.GetChunks()))
	}

	streamedLayerOrders := make([]uint32, 0, 2)
	err = client.SynchronizeBatchStream(
		&gradient.GradientSubmission{
			RuntimeVersion: 1,
			JobId:          "job-123",
			WorkerId:       "worker-a",
			Chunks: []*gradient.GradientChunk{
				{
					Metadata: &gradient.ParameterMetadata{
						Name:       "layer-a",
						Shape:      []int64{2},
						Numel:      2,
						Dtype:      "torch.float32",
						LayerOrder: 1,
					},
					HasGrad:   true,
					Data:      []byte{1, 2},
					ByteSize:  2,
					SyncRound: 4,
				},
				{
					Metadata: &gradient.ParameterMetadata{
						Name:       "layer-b",
						Shape:      []int64{2},
						Numel:      2,
						Dtype:      "torch.float32",
						LayerOrder: 2,
					},
					HasGrad:   true,
					Data:      []byte{3, 4},
					ByteSize:  2,
					SyncRound: 4,
				},
			},
		},
		func(response *gradient.AggregatedGradientChunkResponse) error {
			streamedLayerOrders = append(
				streamedLayerOrders,
				response.GetChunk().GetMetadata().GetLayerOrder(),
			)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("unexpected stream error: %v", err)
	}
	if len(streamedLayerOrders) != 2 {
		t.Fatalf("expected 2 streamed chunks, got %d", len(streamedLayerOrders))
	}

	chunkResponse, err := client.SynchronizeChunk(
		&gradient.GradientChunkSubmission{
			RuntimeVersion: 1,
			JobId:          "job-123",
			WorkerId:       "worker-a",
			Chunk: &gradient.GradientChunk{
				Metadata: &gradient.ParameterMetadata{
					Name:       "layer",
					Shape:      []int64{2},
					Numel:      2,
					Dtype:      "torch.float32",
					LayerOrder: 4,
				},
				HasGrad:   true,
				Data:      []byte{1, 2, 3, 4},
				ByteSize:  4,
				SyncRound: 9,
			},
		},
	)
	if err != nil {
		t.Fatalf("unexpected chunk error: %v", err)
	}
	if chunkResponse.GetAggregationRound() != 9 {
		t.Fatal("unexpected chunk aggregation round")
	}
	if chunkResponse.GetChunk().GetMetadata().GetLayerOrder() != 4 {
		t.Fatal("unexpected chunk layer order")
	}
}
