package pairing

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
)

func TestAcceptPersistsPairingAndRejectsSecondMaster(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pairing.json")
	manager, err := NewManager(NewFileStore(path))
	if err != nil {
		t.Fatalf("create pairing manager: %v", err)
	}

	response := requestPairing(t, manager, pairingRequest("master-a"))
	if response.GetDecision() !=
		gradient.PairingDecision_PAIRING_DECISION_ACCEPTED {
		t.Fatalf("expected accepted pairing, got %s", response.GetDecision())
	}

	reloaded, err := NewManager(NewFileStore(path))
	if err != nil {
		t.Fatalf("reload pairing manager: %v", err)
	}
	record, ok := reloaded.Record()
	if !ok {
		t.Fatal("expected pairing to be persisted")
	}
	if record.MasterID != "master-a" {
		t.Fatalf("unexpected Master: %q", record.MasterID)
	}

	second, err := reloaded.Request(
		context.Background(),
		pairingRequest("master-b"),
	)
	if err != nil {
		t.Fatalf("request second pairing: %v", err)
	}
	if second.GetDecision() !=
		gradient.PairingDecision_PAIRING_DECISION_REJECTED {
		t.Fatal("expected second Master to be rejected")
	}
}

func TestResetPreviousConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pairing.json")
	manager, err := NewManager(NewFileStore(path))
	if err != nil {
		t.Fatalf("create pairing manager: %v", err)
	}

	requestPairing(t, manager, pairingRequest("master-a"))
	if err := manager.Reset(); err != nil {
		t.Fatalf("reset pairing: %v", err)
	}
	if _, ok := manager.Record(); ok {
		t.Fatal("expected pairing record to be removed")
	}

	reloaded, err := NewManager(NewFileStore(path))
	if err != nil {
		t.Fatalf("reload pairing manager: %v", err)
	}
	if _, ok := reloaded.Record(); ok {
		t.Fatal("deleted pairing returned after reload")
	}
}

func requestPairing(
	t *testing.T,
	manager *Manager,
	request *gradient.PairWorkerRequest,
) *gradient.PairWorkerResponse {
	t.Helper()

	result := make(chan *gradient.PairWorkerResponse, 1)
	errors := make(chan error, 1)
	go func() {
		response, err := manager.Request(
			context.Background(),
			request,
		)
		if err != nil {
			errors <- err
			return
		}
		result <- response
	}()

	deadline := time.Now().Add(time.Second)
	for {
		if _, pending := manager.Pending(); pending {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pairing request did not become pending")
		}
		time.Sleep(time.Millisecond)
	}

	if err := manager.Accept(); err != nil {
		t.Fatalf("accept pairing: %v", err)
	}

	select {
	case err := <-errors:
		t.Fatalf("pairing request failed: %v", err)
	case response := <-result:
		return response
	case <-time.After(time.Second):
		t.Fatal("pairing response timed out")
	}
	return nil
}

func pairingRequest(masterID string) *gradient.PairWorkerRequest {
	return &gradient.PairWorkerRequest{
		RequestId:      "request-" + masterID,
		MasterId:       masterID,
		MasterName:     masterID,
		MasterHost:     "127.0.0.1",
		MasterGrpcPort: "60051",
		WorkerId:       "worker-" + masterID,
		PairingToken:   "token-" + masterID,
	}
}
