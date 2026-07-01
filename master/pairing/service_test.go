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
	request *gradient.PairWorkerRequest
}

func (s *acceptingWorker) PairWorker(
	ctx context.Context,
	request *gradient.PairWorkerRequest,
) (*gradient.PairWorkerResponse, error) {
	s.request = request
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
	worker := &acceptingWorker{}
	gradient.RegisterWorkerBridgeServer(server, worker)
	go server.Serve(listener)
	defer server.Stop()

	address := listener.Addr().(*net.TCPAddr)
	discovered := discovery.NewRegistry()
	discovered.Upsert(
		discovery.Worker{
			ID:            discovery.ID("Worker-Laptop", "127.0.0.1", address.Port),
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
		discovery.ID("Worker-Laptop", "127.0.0.1", address.Port),
	)
	if err != nil {
		t.Fatalf("pair Worker: %v", err)
	}
	if worker.request.GetMasterHost() == "" {
		t.Fatal("expected Master pairing request to include a reachable Master host")
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

func TestReachableHostKeepsExplicitLANAddress(t *testing.T) {
	if got := reachableHost("192.168.1.10"); got != "192.168.1.10" {
		t.Fatalf("expected explicit LAN host to be preserved, got %q", got)
	}
}

func TestReachableHostResolvesAutoHosts(t *testing.T) {
	for _, host := range []string{"", "127.0.0.1", "localhost", "0.0.0.0"} {
		if got := reachableHost(host); got == "" {
			t.Fatalf("expected non-empty reachable host for %q", host)
		}
	}
}

func TestVirtualInterfaceNamesAreSkipped(t *testing.T) {
	for _, name := range []string{"vboxnet0", "docker0", "br-abcd", "veth123", "vmnet8", "tun0", "tap0", "VirtualBox Host-Only Network", "VMware Network Adapter VMnet8", "vEthernet (WSL)", "Npcap Loopback Adapter"} {
		if !isVirtualInterface(name) {
			t.Fatalf("expected %q to be treated as virtual", name)
		}
	}
	if isVirtualInterface("wlp4s0") {
		t.Fatal("expected Wi-Fi interface to be allowed")
	}
}
