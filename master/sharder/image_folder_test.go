package sharder

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShardImageFolderBalancesClasses(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "dataset", "train")
	writeImage(t, filepath.Join(source, "caries", "c1.jpg"))
	writeImage(t, filepath.Join(source, "caries", "c2.jpg"))
	writeImage(t, filepath.Join(source, "calculus", "a1.jpg"))
	writeImage(t, filepath.Join(source, "calculus", "a2.jpg"))

	assignments, err := ShardImageFolder(source, "dataset/train", filepath.Join(root, "shards"), []string{"worker-a", "worker-b"})
	if err != nil {
		t.Fatalf("shard image folder: %v", err)
	}
	if assignments[0].Kind != "directory" || assignments[0].Count != 2 {
		t.Fatalf("unexpected first assignment: %#v", assignments[0])
	}
	if _, err := os.Stat(filepath.Join(assignments[0].Path, "calculus", "a1.jpg")); err != nil {
		t.Fatalf("worker-a missing calculus image: %v", err)
	}
	if _, err := os.Stat(filepath.Join(assignments[0].Path, "caries", "c1.jpg")); err != nil {
		t.Fatalf("worker-a missing caries image: %v", err)
	}
	if _, err := os.Stat(filepath.Join(assignments[1].Path, "calculus", "a2.jpg")); err != nil {
		t.Fatalf("worker-b missing calculus image: %v", err)
	}
	if _, err := os.Stat(filepath.Join(assignments[1].Path, "caries", "c2.jpg")); err != nil {
		t.Fatalf("worker-b missing caries image: %v", err)
	}
}

func writeImage(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("image"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}
}
