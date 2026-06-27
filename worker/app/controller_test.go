package app

import (
	"fmt"
	"testing"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	"github.com/Vikaspal8923/Locdist/worker/pairing"
	"github.com/Vikaspal8923/Locdist/worker/service"
)

type fakeLifecycle struct {
	running bool
	paired  bool
	err     error
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
func (f *fakeLifecycle) PendingPairing() (*gradient.PairWorkerRequest, bool) {
	return nil, false
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
