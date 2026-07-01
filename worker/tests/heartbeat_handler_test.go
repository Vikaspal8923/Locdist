package tests

import (
	"context"
	"path/filepath"
	"testing"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	workergrpc "github.com/Vikaspal8923/Locdist/worker/grpc"
	"github.com/Vikaspal8923/Locdist/worker/pairing"
)

func TestHeartbeatAcknowledgesPairedMaster(t *testing.T) {
	store := pairing.NewFileStore(filepath.Join(t.TempDir(), "pairing.json"))
	if err := store.Save(pairing.Record{WorkerID: "worker-1", MasterID: "master-1", PairingToken: "secret"}); err != nil {
		t.Fatal(err)
	}
	pairingManager, err := pairing.NewManager(store)
	if err != nil {
		t.Fatal(err)
	}
	handler := workergrpc.NewWorkerBridgeServer(nil, pairingManager)

	response, err := handler.Heartbeat(context.Background(), &gradient.WorkerHeartbeat{
		WorkerId: "worker-1", MasterId: "master-1", PairingToken: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !response.GetAccepted() || response.GetServerUnixTime() == 0 {
		t.Fatalf("unexpected heartbeat response: %#v", response)
	}
}

func TestHeartbeatRejectsInvalidMasterCredentials(t *testing.T) {
	store := pairing.NewFileStore(filepath.Join(t.TempDir(), "pairing.json"))
	if err := store.Save(pairing.Record{WorkerID: "worker-1", MasterID: "master-1", PairingToken: "secret"}); err != nil {
		t.Fatal(err)
	}
	pairingManager, err := pairing.NewManager(store)
	if err != nil {
		t.Fatal(err)
	}
	handler := workergrpc.NewWorkerBridgeServer(nil, pairingManager)

	_, err = handler.Heartbeat(context.Background(), &gradient.WorkerHeartbeat{
		WorkerId: "worker-1", MasterId: "master-1", PairingToken: "wrong",
	})
	if err == nil {
		t.Fatal("expected invalid pairing credentials to be rejected")
	}
}
