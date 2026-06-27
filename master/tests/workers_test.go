package tests

import (
	"context"
	"testing"

	"github.com/Vikaspal8923/Locdist/master/aggregator"
	"github.com/Vikaspal8923/Locdist/master/coordinator"
	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
	mastergrpc "github.com/Vikaspal8923/Locdist/master/grpc"
	"github.com/Vikaspal8923/Locdist/master/jobs"
	"github.com/Vikaspal8923/Locdist/master/workers"
)

func TestWorkerRegistrationAndStatus(t *testing.T) {
	manager := workers.New()
	server := mastergrpc.NewMasterServer(
		coordinator.New(
			aggregator.New(),
			jobs.New(),
			manager,
		),
	)

	registration, err := server.RegisterWorker(
		context.Background(),
		&gradient.RegisterWorkerRequest{
			WorkerId: "worker-a",
			Host:     "192.168.1.20",
			GrpcPort: "50051",
		},
	)
	if err != nil {
		t.Fatalf("register worker: %v", err)
	}
	if !registration.GetRegistered() {
		t.Fatal("expected worker to be registered")
	}

	response, err := server.UpdateWorkerStatus(
		context.Background(),
		&gradient.WorkerStatusUpdate{
			WorkerId: "worker-a",
			Status:   gradient.WorkerStatus_WORKER_STATUS_RUNNING,
			JobId:    "job-123",
		},
	)
	if err != nil {
		t.Fatalf("update worker status: %v", err)
	}
	if response.GetStatus() != gradient.WorkerStatus_WORKER_STATUS_RUNNING {
		t.Fatalf("unexpected status: %s", response.GetStatus())
	}

	worker, ok := manager.Worker("worker-a")
	if !ok {
		t.Fatal("registered worker was not stored")
	}
	if worker.JobID != "job-123" {
		t.Fatalf("expected job-123, got %q", worker.JobID)
	}
}

func TestDuplicateRegistrationReplacesMetadata(t *testing.T) {
	manager := workers.New()

	for _, host := range []string{"192.168.1.20", "192.168.1.21"} {
		_, err := manager.Register(
			&gradient.RegisterWorkerRequest{
				WorkerId: "worker-a",
				Host:     host,
				GrpcPort: "50051",
			},
		)
		if err != nil {
			t.Fatalf("register worker: %v", err)
		}
	}

	worker, _ := manager.Worker("worker-a")
	if worker.Host != "192.168.1.21" {
		t.Fatalf("expected refreshed host, got %q", worker.Host)
	}
	if manager.Count() != 1 {
		t.Fatalf("expected one worker, got %d", manager.Count())
	}
}

func TestWorkerStatusValidation(t *testing.T) {
	manager := workers.New()

	if _, err := manager.UpdateStatus(
		&gradient.WorkerStatusUpdate{
			WorkerId: "worker-a",
			Status:   gradient.WorkerStatus_WORKER_STATUS_IDLE,
		},
	); err == nil {
		t.Fatal("expected unregistered worker update to fail")
	}

	_, err := manager.Register(
		&gradient.RegisterWorkerRequest{
			WorkerId: "worker-a",
			Host:     "127.0.0.1",
			GrpcPort: "50051",
		},
	)
	if err != nil {
		t.Fatalf("register worker: %v", err)
	}

	if _, err := manager.UpdateStatus(
		&gradient.WorkerStatusUpdate{
			WorkerId: "worker-a",
			Status:   gradient.WorkerStatus_WORKER_STATUS_UNKNOWN,
		},
	); err == nil {
		t.Fatal("expected unknown status to fail")
	}
}
