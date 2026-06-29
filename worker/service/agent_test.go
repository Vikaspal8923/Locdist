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
	if advertiser.metadata.Host == "" {
		t.Fatalf("expected advertised host to be set")
	}
}

func TestAdvertisedHostKeepsExplicitLANAddress(t *testing.T) {
	if got := advertisedHost("192.168.1.42"); got != "192.168.1.42" {
		t.Fatalf("expected explicit LAN host to be preserved, got %q", got)
	}
}

func TestAdvertisedHostResolvesAutoHosts(t *testing.T) {
	for _, host := range []string{"", "127.0.0.1", "localhost", "0.0.0.0"} {
		if got := advertisedHost(host); got == "" {
			t.Fatalf("expected non-empty advertised host for %q", host)
		}
	}
}

func TestVirtualInterfaceNamesAreSkipped(t *testing.T) {
	for _, name := range []string{"vboxnet0", "docker0", "br-abcd", "veth123", "vmnet8", "tun0", "tap0"} {
		if !isVirtualInterface(name) {
			t.Fatalf("expected %q to be treated as virtual", name)
		}
	}
	if isVirtualInterface("wlp4s0") {
		t.Fatal("expected Wi-Fi interface to be allowed")
	}
}

func TestAdvertisedNameKeepsCustomName(t *testing.T) {
	if got := advertisedName("Worker A"); got != "Worker A" {
		t.Fatalf("expected custom Worker name to be preserved, got %q", got)
	}
}

func TestAdvertisedNameMakesDefaultNameUnique(t *testing.T) {
	if got := advertisedName("LDGCC Worker"); got == "" || got == "LDGCC Worker" {
		t.Fatalf("expected default Worker name to include host identity, got %q", got)
	}
}
