package app

import (
	"fmt"
	"testing"
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

func (f *fakeLifecycle) State() (bool, bool) {
	return f.running, f.paired
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
