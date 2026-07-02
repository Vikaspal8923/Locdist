package discovery

import (
	"testing"

	"github.com/hashicorp/mdns"
)

func TestWorkerEntryFilter(t *testing.T) {
	if !isWorkerEntry(
		&mdns.ServiceEntry{
			Name: "Vikas-Laptop._ldgcc-worker._tcp.local.",
		},
	) {
		t.Fatal("LDGCC Worker entry was rejected")
	}

	if isWorkerEntry(
		&mdns.ServiceEntry{
			Name: "Nearby._nearbypresence._tcp.local.",
		},
	) {
		t.Fatal("unrelated mDNS service was accepted")
	}
}

func TestLocalWorkerFromState(t *testing.T) {
	state := localWorkerAppState{
		Running:    true,
		Connection: "PAIRED_ONLINE",
	}
	state.Config.WorkerName = "Desk GPU"
	state.Config.GRPCPort = "50051"

	worker, ok := localWorkerFromState(state, "127.0.0.1")
	if !ok {
		t.Fatal("expected running local Worker to be discovered")
	}
	if worker.Address != "127.0.0.1" || worker.GRPCPort != 50051 {
		t.Fatalf("unexpected Worker address: %s:%d", worker.Address, worker.GRPCPort)
	}
	if worker.PairingStatus != "paired" {
		t.Fatalf("expected paired local Worker, got %q", worker.PairingStatus)
	}
}

func TestLocalWorkerFromStateRejectsStoppedWorker(t *testing.T) {
	state := localWorkerAppState{Running: false}
	state.Config.WorkerName = "Desk GPU"
	state.Config.GRPCPort = "50051"

	if _, ok := localWorkerFromState(state, "127.0.0.1"); ok {
		t.Fatal("stopped local Worker should not be discovered")
	}
}

func TestMergeWorkersSkipsSameLocalWorkerDuplicate(t *testing.T) {
	workers := mergeWorkers(
		[]Worker{
			{
				ID:       ID("Desk GPU", "192.168.1.20", 50051),
				Instance: "Desk GPU",
				Address:  "192.168.1.20",
				GRPCPort: 50051,
			},
		},
		Worker{
			ID:       ID("Desk GPU", "127.0.0.1", 50051),
			Instance: "Desk GPU",
			Address:  "127.0.0.1",
			GRPCPort: 50051,
		},
	)

	if len(workers) != 1 {
		t.Fatalf("expected duplicate local Worker to be skipped, got %d Workers", len(workers))
	}
}
