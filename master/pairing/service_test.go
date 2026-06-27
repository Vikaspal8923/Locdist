package pairing

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/Vikaspal8923/Locdist/master/discovery"
	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
	"github.com/Vikaspal8923/Locdist/master/internal/config"
	"github.com/Vikaspal8923/Locdist/master/workers"
	grpcserver "google.golang.org/grpc"
)

type acceptingWorker struct {
	gradient.UnimplementedWorkerBridgeServer
}

func (s *acceptingWorker) PairWorker(
	ctx context.Context,
	request *gradient.PairWorkerRequest,
) (*gradient.PairWorkerResponse, error) {
	return &gradient.PairWorkerResponse{
		RequestId: request.GetRequestId(),
		Decision: gradient.
			PairingDecision_PAIRING_DECISION_ACCEPTED,
	}, nil
}

func TestPairReservesCredentialsForRegistration(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := grpcserver.NewServer()
	gradient.RegisterWorkerBridgeServer(server, &acceptingWorker{})
	go server.Serve(listener)
	defer server.Stop()

	address := listener.Addr().(*net.TCPAddr)
	discovered := discovery.NewRegistry()
	discovered.Upsert(
		discovery.Worker{
			Instance:      "Worker-Laptop",
			Address:       "127.0.0.1",
			GRPCPort:      address.Port,
			PairingStatus: "unpaired",
			LastSeen:      time.Now(),
		},
	)

	workerManager := workers.New()
	service := New(
		config.Config{
			MasterID:   "master-a",
			MasterName: "Master A",
			Host:       "127.0.0.1",
			Port:       "60051",
		},
		discovered,
		workerManager,
	)

	record, err := service.Pair(
		context.Background(),
		"Worker-Laptop",
	)
	if err != nil {
		t.Fatalf("pair Worker: %v", err)
	}

	_, err = workerManager.Register(
		&gradient.RegisterWorkerRequest{
			WorkerId:     record.WorkerID,
			Host:         "127.0.0.1",
			GrpcPort:     "50051",
			MasterId:     "master-a",
			PairingToken: record.Token,
		},
	)
	if err != nil {
		t.Fatalf("authenticated registration failed: %v", err)
	}
}
