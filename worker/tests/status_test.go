package tests

import (
	"fmt"
	"testing"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	"github.com/Vikaspal8923/Locdist/worker/status"
)

type fakeStatusReporter struct {
	request *gradient.WorkerStatusUpdate
	err     error
}

func (f *fakeStatusReporter) UpdateStatus(
	request *gradient.WorkerStatusUpdate,
) (*gradient.WorkerStatusResponse, error) {
	f.request = request
	if f.err != nil {
		return nil, f.err
	}
	return &gradient.WorkerStatusResponse{
		WorkerId: request.GetWorkerId(),
		Status:   request.GetStatus(),
	}, nil
}

func TestStatusManagerReportsAndStoresStatus(t *testing.T) {
	reporter := &fakeStatusReporter{}
	manager := status.New("worker-a", reporter)

	err := manager.Set(
		gradient.WorkerStatus_WORKER_STATUS_RUNNING,
		"job-123",
	)
	if err != nil {
		t.Fatalf("set status: %v", err)
	}

	current, jobID := manager.Current()
	if current != gradient.WorkerStatus_WORKER_STATUS_RUNNING {
		t.Fatalf("unexpected status: %s", current)
	}
	if jobID != "job-123" {
		t.Fatalf("unexpected job id: %q", jobID)
	}
	if reporter.request.GetWorkerId() != "worker-a" {
		t.Fatalf("unexpected worker id: %q", reporter.request.GetWorkerId())
	}
}

func TestStatusManagerDoesNotStoreFailedReport(t *testing.T) {
	reporter := &fakeStatusReporter{
		err: fmt.Errorf("master unavailable"),
	}
	manager := status.New("worker-a", reporter)

	if err := manager.Set(
		gradient.WorkerStatus_WORKER_STATUS_IDLE,
		"",
	); err == nil {
		t.Fatal("expected report to fail")
	}

	current, _ := manager.Current()
	if current != gradient.WorkerStatus_WORKER_STATUS_UNKNOWN {
		t.Fatalf("expected unknown status, got %s", current)
	}
}
