package service

import (
	"testing"

	"github.com/Vikaspal8923/Locdist/worker/discovery"
	"github.com/Vikaspal8923/Locdist/worker/internal/config"
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
	agent := New(
		config.Config{
			WorkerName: "Vikas-Laptop",
			Host:       "127.0.0.1",
			Port:       "0",
		},
		advertiser,
	)

	if err := agent.Start(); err != nil {
		t.Fatalf("start unpaired Worker: %v", err)
	}
	defer agent.Stop()

	running, paired := agent.State()
	if !running {
		t.Fatal("expected Worker to be running")
	}
	if paired {
		t.Fatal("expected Worker to remain unpaired")
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
