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
  type: jsonl

workers:
  count: 3

outputs:
  - model/model.pt
  - results/

communication:
  precision: fp16
  compression:
    type: topk
    mode: per_layer
    top_k: 5%
    error_feedback: true
    warmup_steps: 500
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
	if spec.Dataset.Type != "jsonl" {
		t.Fatalf("unexpected dataset type: %q", spec.Dataset.Type)
	}
	if spec.Workers.Count != 3 {
		t.Fatalf("unexpected worker count: %d", spec.Workers.Count)
	}
	if len(spec.Outputs) != 2 || spec.Outputs[0] != "model/model.pt" || spec.Outputs[1] != "results/" {
		t.Fatalf("unexpected outputs: %v", spec.Outputs)
	}
	if spec.Communication.Precision != "fp16" ||
		spec.Communication.Compression.Type != "topk" ||
		spec.Communication.Compression.Mode != "per_layer" ||
		spec.Communication.Compression.TopK != "5%" ||
		!spec.Communication.Compression.ErrorFeedback ||
		spec.Communication.Compression.WarmupSteps != 500 {
		t.Fatalf("unexpected communication config: %+v", spec.Communication)
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

func TestLoadSpecNormalizesWindowsStyleRelativePaths(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "train.py"), "print('train')\n")
	writeFile(t, filepath.Join(root, "dataset", "train", "class", "1.jpg"), "image\n")
	writeFile(t, filepath.Join(root, DefaultSpecFile), `
entrypoint: src\train.py
dataset:
  train: dataset\train
  type: image_folder
workers:
  count: 1
outputs:
  - outputs\metrics.json
`)

	spec, err := LoadSpec(root)
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	if spec.Entrypoint != "src/train.py" {
		t.Fatalf("entrypoint was not normalized: %q", spec.Entrypoint)
	}
	if spec.Dataset.Train != "dataset/train" {
		t.Fatalf("dataset path was not normalized: %q", spec.Dataset.Train)
	}
	if len(spec.Outputs) != 1 || spec.Outputs[0] != "outputs/metrics.json" {
		t.Fatalf("outputs were not normalized: %v", spec.Outputs)
	}
}

func TestLoadSpecRejectsWindowsAbsolutePath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "train.py"), "x")
	writeFile(t, filepath.Join(root, "dataset", "train.jsonl"), "{}\n")
	writeFile(t, filepath.Join(root, DefaultSpecFile), `
entrypoint: C:\Users\me\train.py
dataset:
  train: dataset/train.jsonl
workers:
  count: 1
`)

	if _, err := LoadSpec(root); err == nil {
		t.Fatal("expected Windows absolute entrypoint to fail")
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
