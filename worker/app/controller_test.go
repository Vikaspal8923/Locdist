package app

import (
	"fmt"
	"path/filepath"
	"testing"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	"github.com/Vikaspal8923/Locdist/worker/internal/config"
	"github.com/Vikaspal8923/Locdist/worker/pairing"
	"github.com/Vikaspal8923/Locdist/worker/service"
)

type fakeLifecycle struct {
	running bool
	paired  bool
	err     error
	cfg     config.Config
}

func (f *fakeLifecycle) Start() error {
	if f.err != nil {
		return f.err
	}
	f.running = true
	return nil
}

func (f *fakeLifecycle) Stop() error {
	f.running = false
	return nil
}

func (f *fakeLifecycle) AcceptPairing() error { return nil }
func (f *fakeLifecycle) RejectPairing() error { return nil }
func (f *fakeLifecycle) ResetPairing() error  { return nil }
func (f *fakeLifecycle) State() (bool, service.ConnectionState) {
	if f.paired {
		return f.running, service.ConnectionPairedOnline
	}
	return f.running, service.ConnectionUnpaired
}
func (f *fakeLifecycle) Config() config.Config {
	if f.cfg.WorkerName == "" {
		f.cfg = config.Default()
	}
	return f.cfg
}
func (f *fakeLifecycle) UpdateConfig(cfg config.Config) error {
	if f.err != nil {
		return f.err
	}
	f.cfg = cfg
	return nil
}
func (f *fakeLifecycle) PendingPairing() (*gradient.PairWorkerRequest, bool) {
	return nil, false
}

func TestControllerUpdatesConfigWhenStopped(t *testing.T) {
	lifecycle := &fakeLifecycle{}
	path := filepath.Join(t.TempDir(), "worker_config.json")
	controller := NewController(lifecycle, path)

	if err := controller.UpdateConfig(ConfigUpdate{WorkerName: "Desk GPU", GRPCPort: "5100"}); err != nil {
		t.Fatalf("update config: %v", err)
	}
	state := controller.State()
	if state.Config.WorkerName != "Desk GPU" {
		t.Fatalf("expected updated Worker name, got %q", state.Config.WorkerName)
	}
	if state.Config.GRPCPort != "5100" {
		t.Fatalf("expected updated gRPC port, got %q", state.Config.GRPCPort)
	}
}

func TestControllerRejectsConfigUpdateWhileRunning(t *testing.T) {
	lifecycle := &fakeLifecycle{running: true}
	controller := NewController(lifecycle, filepath.Join(t.TempDir(), "worker_config.json"))

	if err := controller.UpdateConfig(ConfigUpdate{WorkerName: "Desk GPU"}); err == nil {
		t.Fatal("expected running Worker config update to fail")
	}
	if controller.State().Error != "stop Worker before changing settings" {
		t.Fatalf("unexpected error: %q", controller.State().Error)
	}
}
func (f *fakeLifecycle) PairingRecord() (*pairing.Record, bool) {
	return nil, false
}

func TestControllerStartsAndStopsWorker(t *testing.T) {
	lifecycle := &fakeLifecycle{}
	controller := NewController(lifecycle)

	if err := controller.Start(); err != nil {
		t.Fatalf("start Worker: %v", err)
	}
	if !controller.State().Running {
		t.Fatal("expected Worker to be running")
	}

	if err := controller.Stop(); err != nil {
		t.Fatalf("stop Worker: %v", err)
	}
	if controller.State().Running {
		t.Fatal("expected Worker to be stopped")
	}
}

func TestControllerExposesStartError(t *testing.T) {
	controller := NewController(
		&fakeLifecycle{err: fmt.Errorf("discovery unavailable")},
	)

	if err := controller.Start(); err == nil {
		t.Fatal("expected start to fail")
	}
	if controller.State().Error != "discovery unavailable" {
		t.Fatalf("unexpected app error: %q", controller.State().Error)
	}
}
