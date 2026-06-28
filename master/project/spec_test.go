package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSpec(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "train.py"), "print('train')\n")
	writeFile(t, filepath.Join(root, "dataset", "train.jsonl"), "{}\n")
	writeFile(t, filepath.Join(root, DefaultSpecFile), `
job:
  name: movie-review-training

entrypoint: train.py

dataset:
  train: dataset/train.jsonl

workers:
  count: 3

outputs:
  - model/model.pt
  - results/
`)

	spec, err := LoadSpec(root)
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	if spec.Job.Name != "movie-review-training" {
		t.Fatalf("unexpected job name: %q", spec.Job.Name)
	}
	if spec.Entrypoint != "train.py" {
		t.Fatalf("unexpected entrypoint: %q", spec.Entrypoint)
	}
	if spec.Dataset.Train != "dataset/train.jsonl" {
		t.Fatalf("unexpected dataset: %q", spec.Dataset.Train)
	}
	if spec.Workers.Count != 3 {
		t.Fatalf("unexpected worker count: %d", spec.Workers.Count)
	}
	if len(spec.Outputs) != 2 || spec.Outputs[0] != "model/model.pt" || spec.Outputs[1] != "results/" {
		t.Fatalf("unexpected outputs: %v", spec.Outputs)
	}
}

func TestLoadSpecRejectsUnsafeOutput(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "train.py"), "x")
	writeFile(t, filepath.Join(root, "dataset", "train.jsonl"), "{}\n")
	writeFile(t, filepath.Join(root, DefaultSpecFile), "entrypoint: train.py\ndataset:\n  train: dataset/train.jsonl\nworkers:\n  count: 1\noutputs:\n  - ../secret\n")
	if _, err := LoadSpec(root); err == nil {
		t.Fatal("expected unsafe output to fail")
	}
}

func TestLoadSpecRejectsMissingWorkerCount(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "train.py"), "print('train')\n")
	writeFile(t, filepath.Join(root, "dataset", "train.jsonl"), "{}\n")
	writeFile(t, filepath.Join(root, DefaultSpecFile), `
entrypoint: train.py
dataset:
  train: dataset/train.jsonl
`)

	if _, err := LoadSpec(root); err == nil {
		t.Fatal("expected missing workers.count to fail")
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
