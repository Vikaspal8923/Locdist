package sharder

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShardJSONL(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "dataset", "train.jsonl")
	writeDataset(t, source, []string{
		`{"text":"a","label":1}`,
		`{"text":"b","label":0}`,
		`{"text":"c","label":1}`,
		`{"text":"d","label":0}`,
		`{"text":"e","label":1}`,
	})

	assignments, err := ShardJSONL(
		source,
		"dataset/train.jsonl",
		filepath.Join(root, "shards"),
		[]string{"worker-a", "worker-b"},
	)
	if err != nil {
		t.Fatalf("shard JSONL: %v", err)
	}

	if assignments[0].Start != 1 || assignments[0].End != 3 {
		t.Fatalf("unexpected first shard: %#v", assignments[0])
	}
	if assignments[1].Start != 4 || assignments[1].End != 5 {
		t.Fatalf("unexpected second shard: %#v", assignments[1])
	}

	firstShard, err := os.ReadFile(assignments[0].Path)
	if err != nil {
		t.Fatalf("read first shard: %v", err)
	}
	if string(firstShard) != "{\"text\":\"a\",\"label\":1}\n{\"text\":\"b\",\"label\":0}\n{\"text\":\"c\",\"label\":1}\n" {
		t.Fatalf("unexpected first shard content: %q", firstShard)
	}
}

func TestShardJSONLRejectsInvalidJSON(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "dataset", "train.jsonl")
	writeDataset(t, source, []string{`{"ok":true}`, `not-json`})

	_, err := ShardJSONL(
		source,
		"dataset/train.jsonl",
		filepath.Join(root, "shards"),
		[]string{"worker-a"},
	)
	if err == nil {
		t.Fatal("expected invalid JSONL to fail")
	}
}

func TestShardJSONLRequiresSamplePerWorker(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "dataset", "train.jsonl")
	writeDataset(t, source, []string{`{"ok":true}`})

	_, err := ShardJSONL(
		source,
		"dataset/train.jsonl",
		filepath.Join(root, "shards"),
		[]string{"worker-a", "worker-b"},
	)
	if err == nil {
		t.Fatal("expected too few samples to fail")
	}
}

func writeDataset(t *testing.T, path string, lines []string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := ""
	for _, line := range lines {
		content += line + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write dataset: %v", err)
	}
}
