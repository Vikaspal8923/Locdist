package tests

import (
	"context"
	"testing"
	"time"

	"github.com/Vikaspal8923/Locdist/master/aggregator"
	"github.com/Vikaspal8923/Locdist/master/coordinator"
	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
	mastergrpc "github.com/Vikaspal8923/Locdist/master/grpc"
	"github.com/Vikaspal8923/Locdist/master/jobs"
	"github.com/Vikaspal8923/Locdist/master/workers"
)

func TestWorkerRegistrationAndStatus(t *testing.T) {
	manager := workers.New()
	reservePairing(t, manager, "worker-a")
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
			WorkerId:     "worker-a",
			Host:         "192.168.1.20",
			GrpcPort:     "50051",
			MasterId:     "master-a",
			PairingToken: "token-a",
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
	reservePairing(t, manager, "worker-a")

	for _, host := range []string{"192.168.1.20", "192.168.1.21"} {
		_, err := manager.Register(
			&gradient.RegisterWorkerRequest{
				WorkerId:     "worker-a",
				Host:         host,
				GrpcPort:     "50051",
				MasterId:     "master-a",
				PairingToken: "token-a",
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
	reservePairing(t, manager, "worker-a")

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
			WorkerId:     "worker-a",
			Host:         "127.0.0.1",
			GrpcPort:     "50051",
			MasterId:     "master-a",
			PairingToken: "token-a",
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

func TestWorkerHeartbeatAvailabilityLifecycle(t *testing.T) {
	manager := workers.New()
	reservePairing(t, manager, "worker-a")

	_, err := manager.Register(
		&gradient.RegisterWorkerRequest{
			WorkerId:     "worker-a",
			Host:         "127.0.0.1",
			GrpcPort:     "50051",
			MasterId:     "master-a",
			PairingToken: "token-a",
		},
	)
	if err != nil {
		t.Fatalf("register worker: %v", err)
	}

	worker, _ := manager.Worker("worker-a")
	if worker.Availability != workers.AvailabilityOnline {
		t.Fatalf("expected online after registration, got %q", worker.Availability)
	}

	manager.Sweep(time.Now().Add(7*time.Second), 6*time.Second, 12*time.Second)
	worker, _ = manager.Worker("worker-a")
	if worker.Availability != workers.AvailabilityStale {
		t.Fatalf("expected stale worker, got %q", worker.Availability)
	}

	manager.Sweep(time.Now().Add(13*time.Second), 6*time.Second, 12*time.Second)
	worker, _ = manager.Worker("worker-a")
	if worker.Availability != workers.AvailabilityOffline {
		t.Fatalf("expected offline worker, got %q", worker.Availability)
	}

	_, err = manager.Heartbeat(
		&gradient.WorkerHeartbeat{
			WorkerId:     "worker-a",
			MasterId:     "master-a",
			PairingToken: "token-a",
			Status:       gradient.WorkerStatus_WORKER_STATUS_RUNNING,
			JobId:        "job-123",
		},
	)
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	worker, _ = manager.Worker("worker-a")
	if worker.Availability != workers.AvailabilityOnline {
		t.Fatalf("expected online after heartbeat, got %q", worker.Availability)
	}
	if worker.JobID != "job-123" {
		t.Fatalf("expected heartbeat job, got %q", worker.JobID)
	}

	if err := manager.GoingOffline(
		&gradient.WorkerOfflineRequest{
			WorkerId:     "worker-a",
			MasterId:     "master-a",
			PairingToken: "token-a",
		},
	); err != nil {
		t.Fatalf("going offline: %v", err)
	}
	worker, _ = manager.Worker("worker-a")
	if worker.Availability != workers.AvailabilityOffline {
		t.Fatalf("expected explicit offline, got %q", worker.Availability)
	}
}

func reservePairing(
	t *testing.T,
	manager *workers.Manager,
	workerID string,
) {
	t.Helper()
	if err := manager.ReservePairing(
		workerID,
		"master-a",
		"token-a",
	); err != nil {
		t.Fatalf("reserve pairing: %v", err)
	}
}
