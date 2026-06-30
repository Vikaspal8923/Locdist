package workspace

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareExtractsValidatedWorkspace(t *testing.T) {
	manager := New(filepath.Join(t.TempDir(), "workspaces"))
	archive := makeZip(t, map[string]string{"train.py": "print('ok')", "dataset/train.jsonl": "one\n", "job_config.json": "{}"})
	path, err := manager.Prepare("job-1", "train.py", "dataset/train.jsonl", archive)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(path, "dataset", "train.jsonl"))
	if err != nil || string(content) != "one\n" {
		t.Fatalf("unexpected shard: %q, %v", content, err)
	}
	for _, directory := range []string{"logs", "artifacts"} {
		if info, err := os.Stat(filepath.Join(path, directory)); err != nil || !info.IsDir() {
			t.Fatalf("%s was not created", directory)
		}
	}
}

func TestPrepareAcceptsDatasetDirectory(t *testing.T) {
	manager := New(filepath.Join(t.TempDir(), "workspaces"))
	archive := makeZip(t, map[string]string{"train.py": "print('ok')", "dataset/train/caries/1.jpg": "image", "job_config.json": "{}"})
	path, err := manager.Prepare("job-1", "train.py", "dataset/train", archive)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(path, "dataset", "train", "caries")); err != nil || !info.IsDir() {
		t.Fatalf("image class directory was not prepared: %v", err)
	}
}

func TestPrepareFileExtractsValidatedWorkspace(t *testing.T) {
	manager := New(filepath.Join(t.TempDir(), "workspaces"))
	archive := makeZip(t, map[string]string{"train.py": "print('ok')", "dataset/train/class/1.jpg": "image", "job_config.json": "{}"})
	archivePath := filepath.Join(t.TempDir(), "workspace.zip")
	if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := manager.PrepareFile("job-1", "train.py", "dataset/train", archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(path, "dataset", "train", "class", "1.jpg")); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("image shard was not prepared from file: %v", err)
	}
}

func TestPrepareRejectsPathTraversal(t *testing.T) {
	manager := New(filepath.Join(t.TempDir(), "workspaces"))
	archive := makeZip(t, map[string]string{"../outside": "bad", "train.py": "x", "dataset/train.jsonl": "x", "job_config.json": "{}"})
	if _, err := manager.Prepare("job-1", "train.py", "dataset/train.jsonl", archive); err == nil {
		t.Fatal("expected unsafe archive to be rejected")
	}
}

func TestPrepareNewJobClearsPreviousWorkspace(t *testing.T) {
	manager := New(filepath.Join(t.TempDir(), "workspaces"))
	archive := makeZip(t, map[string]string{"train.py": "x", "dataset/train.jsonl": "x", "job_config.json": "{}"})
	oldPath, err := manager.Prepare("job-old", "train.py", "dataset/train.jsonl", archive)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Prepare("job-new", "train.py", "dataset/train.jsonl", archive); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("previous workspace still exists")
	}
}

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
