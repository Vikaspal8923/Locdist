package tests

import (
	"context"
	"path/filepath"
	"testing"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	workergrpc "github.com/Vikaspal8923/Locdist/worker/grpc"
	"github.com/Vikaspal8923/Locdist/worker/pairing"
	workersetup "github.com/Vikaspal8923/Locdist/worker/setup"
	"github.com/Vikaspal8923/Locdist/worker/workspace"
)

func TestSetupJobRejectsInvalidMasterCredentials(t *testing.T) {
	store := pairing.NewFileStore(filepath.Join(t.TempDir(), "pairing.json"))
	if err := store.Save(pairing.Record{WorkerID: "worker-1", MasterID: "master-1", PairingToken: "secret"}); err != nil {
		t.Fatal(err)
	}
	pairingManager, err := pairing.NewManager(store)
	if err != nil {
		t.Fatal(err)
	}
	workspaceManager := workspace.New(filepath.Join(t.TempDir(), "workspaces"))
	handler := workergrpc.NewWorkerBridgeServer(nil, pairingManager)
	handler.SetSetupManager(workersetup.New(workspaceManager))

	_, err = handler.SetupJob(context.Background(), &gradient.SetupJobRequest{
		JobId: "job-1", WorkerId: "worker-1", MasterId: "master-1", PairingToken: "wrong",
	})
	if err == nil {
		t.Fatal("expected invalid pairing credentials to be rejected")
	}
}
