package service

import (
	"path/filepath"
	"testing"

	"github.com/Vikaspal8923/Locdist/worker/discovery"
	"github.com/Vikaspal8923/Locdist/worker/internal/config"
	"github.com/Vikaspal8923/Locdist/worker/pairing"
)

type fakeAdvertiser struct {
	started  bool
	metadata discovery.Metadata
}

func (f *fakeAdvertiser) Start(metadata discovery.Metadata) error {
	f.started = true
	f.metadata = metadata
	return nil
}

func (f *fakeAdvertiser) Stop() error {
	f.started = false
	return nil
}

func TestUnpairedAgentBecomesDiscoverable(t *testing.T) {
	advertiser := &fakeAdvertiser{}
	pairingManager, err := pairing.NewManager(
		pairing.NewFileStore(
			filepath.Join(t.TempDir(), "pairing.json"),
		),
	)
	if err != nil {
		t.Fatalf("create pairing manager: %v", err)
	}
	agent := New(
		config.Config{
			WorkerName: "Vikas-Laptop",
			Host:       "127.0.0.1",
			Port:       "0",
		},
		advertiser,
		pairingManager,
	)

	if err := agent.Start(); err != nil {
		t.Fatalf("start unpaired Worker: %v", err)
	}
	defer agent.Stop()

	running, connection := agent.State()
	if !running {
		t.Fatal("expected Worker to be running")
	}
	if connection != ConnectionUnpaired {
		t.Fatalf("expected unpaired Worker, got %s", connection)
	}
	if !advertiser.started {
		t.Fatal("expected discovery advertisement to start")
	}
	if advertiser.metadata.PairingStatus != "unpaired" {
		t.Fatalf(
			"unexpected pairing status: %q",
			advertiser.metadata.PairingStatus,
		)
	}
}
