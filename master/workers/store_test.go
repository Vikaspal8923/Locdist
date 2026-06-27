package workers

import (
	"path/filepath"
	"testing"

	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
)

func TestPairingCredentialsSurviveMasterRestart(t *testing.T) {
	store := NewFilePairingStore(
		filepath.Join(t.TempDir(), "master_pairings.json"),
	)
	manager, err := NewPersistent(store)
	if err != nil {
		t.Fatalf("create persistent manager: %v", err)
	}
	if err := manager.ReservePairing(
		"worker-a",
		"master-a",
		"token-a",
	); err != nil {
		t.Fatalf("reserve pairing: %v", err)
	}

	restarted, err := NewPersistent(store)
	if err != nil {
		t.Fatalf("reload persistent manager: %v", err)
	}
	if _, err := restarted.Register(
		&gradient.RegisterWorkerRequest{
			WorkerId:     "worker-a",
			Host:         "127.0.0.1",
			GrpcPort:     "50051",
			MasterId:     "master-a",
			PairingToken: "token-a",
		},
	); err != nil {
		t.Fatalf("registration after Master restart: %v", err)
	}

	if err := restarted.RevokeAuthenticated(
		&gradient.UnpairWorkerRequest{
			WorkerId:     "worker-a",
			MasterId:     "master-a",
			PairingToken: "token-a",
		},
	); err != nil {
		t.Fatalf("revoke pairing: %v", err)
	}

	restartedAgain, err := NewPersistent(store)
	if err != nil {
		t.Fatalf("reload after reset: %v", err)
	}
	if _, err := restartedAgain.Register(
		&gradient.RegisterWorkerRequest{
			WorkerId:     "worker-a",
			Host:         "127.0.0.1",
			GrpcPort:     "50051",
			MasterId:     "master-a",
			PairingToken: "token-a",
		},
	); err == nil {
		t.Fatal("revoked credential remained valid after restart")
	}
}
