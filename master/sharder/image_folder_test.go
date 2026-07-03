package sharder

import (
	"fmt"
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

func TestShardImageFolderDropsRemainderForEqualCounts(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "dataset", "train")
	writeImage(t, filepath.Join(source, "healthy", "1.jpg"))
	writeImage(t, filepath.Join(source, "healthy", "2.jpg"))
	writeImage(t, filepath.Join(source, "healthy", "3.jpg"))
	writeImage(t, filepath.Join(source, "healthy", "4.jpg"))
	writeImage(t, filepath.Join(source, "healthy", "5.jpg"))

	assignments, err := ShardImageFolder(source, "dataset/train", filepath.Join(root, "shards"), []string{"worker-a", "worker-b"})
	if err != nil {
		t.Fatalf("shard image folder: %v", err)
	}
	if assignments[0].Count != 2 || assignments[1].Count != 2 {
		t.Fatalf("expected equal counts of 2 and 2, got %#v", assignments)
	}
	if countImages(t, assignments[0].Path)+countImages(t, assignments[1].Path) != 4 {
		t.Fatalf("expected one dropped remainder image")
	}
}

func TestShardImageFolderBalancesUnevenClassesWithEqualCounts(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "dataset", "train")
	for index := 1; index <= 5; index++ {
		writeImage(t, filepath.Join(source, "class-a", fmt.Sprintf("%d.jpg", index)))
	}
	for index := 1; index <= 4; index++ {
		writeImage(t, filepath.Join(source, "class-b", fmt.Sprintf("%d.jpg", index)))
	}

	assignments, err := ShardImageFolder(source, "dataset/train", filepath.Join(root, "shards"), []string{"worker-a", "worker-b"})
	if err != nil {
		t.Fatalf("shard image folder: %v", err)
	}
	if assignments[0].Count != 4 || assignments[1].Count != 4 {
		t.Fatalf("expected equal counts of 4 and 4, got %#v", assignments)
	}
}

func countImages(t *testing.T, root string) int {
	t.Helper()
	count := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("count images: %v", err)
	}
	return count
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
