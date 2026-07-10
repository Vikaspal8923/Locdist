package sharder

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShardYOLOSplitCopiesImageLabelPairs(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "dataset", "train")
	writeImage(t, filepath.Join(source, "images", "a.jpg"))
	writeImage(t, filepath.Join(source, "images", "nested", "b.png"))
	writeLabel(t, filepath.Join(source, "labels", "a.txt"), "0 0.5 0.5 0.2 0.2\n")
	writeLabel(t, filepath.Join(source, "labels", "nested", "b.txt"), "1 0.4 0.4 0.1 0.1\n")

	assignments, err := ShardYOLOSplit(source, "dataset/train", filepath.Join(root, "shards"), []string{"worker-a", "worker-b"})
	if err != nil {
		t.Fatalf("shard yolo split: %v", err)
	}
	if assignments[0].Kind != "directory" || assignments[0].Count != 1 {
		t.Fatalf("unexpected first assignment: %#v", assignments[0])
	}
	if _, err := os.Stat(filepath.Join(assignments[0].Path, "images", "a.jpg")); err != nil {
		t.Fatalf("worker-a missing image: %v", err)
	}
	if _, err := os.Stat(filepath.Join(assignments[0].Path, "labels", "a.txt")); err != nil {
		t.Fatalf("worker-a missing label: %v", err)
	}
	if _, err := os.Stat(filepath.Join(assignments[1].Path, "images", "nested", "b.png")); err != nil {
		t.Fatalf("worker-b missing nested image: %v", err)
	}
	if _, err := os.Stat(filepath.Join(assignments[1].Path, "labels", "nested", "b.txt")); err != nil {
		t.Fatalf("worker-b missing nested label: %v", err)
	}
}

func TestShardYOLOSplitCreatesEmptyLabelWhenMissing(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "dataset", "train")
	writeImage(t, filepath.Join(source, "images", "a.jpg"))
	writeImage(t, filepath.Join(source, "images", "b.jpg"))
	writeLabel(t, filepath.Join(source, "labels", "a.txt"), "0 0.5 0.5 0.2 0.2\n")

	assignments, err := ShardYOLOSplit(source, "dataset/train", filepath.Join(root, "shards"), []string{"worker-a", "worker-b"})
	if err != nil {
		t.Fatalf("shard yolo split: %v", err)
	}
	info, err := os.Stat(filepath.Join(assignments[1].Path, "labels", "b.txt"))
	if err != nil {
		t.Fatalf("worker-b missing empty label: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("expected empty label file, got size %d", info.Size())
	}
}

func writeLabel(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write label: %v", err)
	}
}
