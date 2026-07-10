package orchestrator

import (
	"os"
	"path/filepath"
	"testing"

	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
	"github.com/Vikaspal8923/Locdist/master/jobs"
	"github.com/Vikaspal8923/Locdist/master/workers"
)

func TestPrepareCreatesJobAndShards(t *testing.T) {
	projectRoot := t.TempDir()
	writeFile(t, filepath.Join(projectRoot, "train.py"), "print('train')\n")
	writeFile(t, filepath.Join(projectRoot, "dataset", "train.jsonl"), "{\"x\":1}\n{\"x\":2}\n{\"x\":3}\n")
	writeFile(t, filepath.Join(projectRoot, "ldgcc.yaml"), `
job:
  name: test-job
entrypoint: train.py
dataset:
  train: dataset/train.jsonl
workers:
  count: 2
`)

	workerManager := workers.New()
	registerWorker(t, workerManager, "worker-b")
	registerWorker(t, workerManager, "worker-a")

	preparer := NewPreparer(
		jobs.New(),
		workerManager,
		filepath.Join(t.TempDir(), "jobs"),
	)
	job, err := preparer.Prepare(projectRoot)
	if err != nil {
		t.Fatalf("prepare job: %v", err)
	}

	if job.Status != jobs.StatusPrepared {
		t.Fatalf("expected prepared job, got %q", job.Status)
	}
	if job.Name != "test-job" {
		t.Fatalf("unexpected job name: %q", job.Name)
	}
	if len(job.Workers) != 2 || len(job.Shards) != 2 {
		t.Fatalf("unexpected assignments: %#v", job)
	}
	if job.Workers[0].WorkerID != "worker-a" {
		t.Fatalf("expected deterministic worker order, got %#v", job.Workers)
	}
	if _, err := os.Stat(job.Shards[0].Path); err != nil {
		t.Fatalf("expected shard file: %v", err)
	}
}

func TestPrepareCreatesYOLOSplitShards(t *testing.T) {
	projectRoot := t.TempDir()
	writeFile(t, filepath.Join(projectRoot, "train.py"), "print('train')\n")
	writeFile(t, filepath.Join(projectRoot, "dataset", "train", "images", "a.jpg"), "image-a")
	writeFile(t, filepath.Join(projectRoot, "dataset", "train", "images", "b.jpg"), "image-b")
	writeFile(t, filepath.Join(projectRoot, "dataset", "train", "labels", "a.txt"), "0 0.5 0.5 0.2 0.2\n")
	writeFile(t, filepath.Join(projectRoot, "dataset", "train", "labels", "b.txt"), "1 0.4 0.4 0.1 0.1\n")
	writeFile(t, filepath.Join(projectRoot, "ldgcc.yaml"), `
entrypoint: train.py
dataset:
  train: dataset/train
  type: yolo_split
workers:
  count: 2
`)

	workerManager := workers.New()
	registerWorker(t, workerManager, "worker-b")
	registerWorker(t, workerManager, "worker-a")

	preparer := NewPreparer(
		jobs.New(),
		workerManager,
		filepath.Join(t.TempDir(), "jobs"),
	)
	job, err := preparer.Prepare(projectRoot)
	if err != nil {
		t.Fatalf("prepare job: %v", err)
	}

	if len(job.Shards) != 2 {
		t.Fatalf("unexpected shards: %#v", job.Shards)
	}
	if _, err := os.Stat(filepath.Join(job.Shards[0].Path, "images")); err != nil {
		t.Fatalf("expected YOLO shard images dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(job.Shards[0].Path, "labels")); err != nil {
		t.Fatalf("expected YOLO shard labels dir: %v", err)
	}
}

func registerWorker(
	t *testing.T,
	manager *workers.Manager,
	workerID string,
) {
	t.Helper()
	if err := manager.ReservePairing(workerID, "master-a", "token-"+workerID); err != nil {
		t.Fatalf("reserve pairing: %v", err)
	}
	if _, err := manager.Register(
		&gradient.RegisterWorkerRequest{
			WorkerId:     workerID,
			Host:         "127.0.0.1",
			GrpcPort:     "50051",
			MasterId:     "master-a",
			PairingToken: "token-" + workerID,
		},
	); err != nil {
		t.Fatalf("register worker: %v", err)
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}
