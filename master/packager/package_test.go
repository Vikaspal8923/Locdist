package packager

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildReplacesDatasetAndExcludesLocalState(t *testing.T) {
	root := t.TempDir()
	write := func(relative, content string) string {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	write("train.py", "print('train')")
	write("dataset/train.jsonl", "original\n")
	write(".git/config", "secret")
	shard := write("ldgcc_jobs/job-1/shards/worker-1.jsonl", "worker shard\n")

	data, err := Build(PackageRequest{ProjectRoot: root, JobID: "job-1", WorkerID: "worker-1", Entrypoint: "train.py", DatasetPath: "dataset/train.jsonl", ShardPath: shard, Outputs: []string{"model/model.pt"}})
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, data)
	if string(files["dataset/train.jsonl"]) != "worker shard\n" {
		t.Fatalf("dataset was not replaced: %q", files["dataset/train.jsonl"])
	}
	if _, ok := files[".git/config"]; ok {
		t.Fatal(".git content was packaged")
	}
	if _, ok := files["job_config.json"]; !ok {
		t.Fatal("job_config.json is missing")
	}
	if !bytes.Contains(files["job_config.json"], []byte("model/model.pt")) {
		t.Fatal("configured outputs are missing from job config")
	}
}

func unzip(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string][]byte)
	for _, file := range reader.File {
		source, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(source)
		source.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[file.Name] = content
	}
	return files
}
