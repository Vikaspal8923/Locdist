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

func TestBuildReplacesImageFolderDataset(t *testing.T) {
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
	write("dataset/train/caries/original.jpg", "original")
	shardRoot := filepath.Join(root, "ldgcc_jobs", "job-1", "shards", "worker-1", "dataset", "train")
	write("ldgcc_jobs/job-1/shards/worker-1/dataset/train/caries/shard.jpg", "shard")

	data, err := Build(PackageRequest{ProjectRoot: root, JobID: "job-1", WorkerID: "worker-1", Entrypoint: "train.py", DatasetPath: "dataset/train", ShardPath: shardRoot, ShardKind: "directory"})
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, data)
	if _, ok := files["dataset/train/caries/original.jpg"]; ok {
		t.Fatal("original image dataset file was packaged")
	}
	if string(files["dataset/train/caries/shard.jpg"]) != "shard" {
		t.Fatalf("image shard was not packaged: %q", files["dataset/train/caries/shard.jpg"])
	}
}

func TestBuildInjectsLDGCCRuntime(t *testing.T) {
	root := t.TempDir()
	runtimeRoot := t.TempDir()
	writeProject := func(relative, content string) string {
		return writeFile(t, root, relative, content)
	}
	writeRuntime := func(relative, content string) {
		writeFile(t, runtimeRoot, relative, content)
	}
	writeProject("train.py", "import locdist")
	writeProject("dataset/train.jsonl", "row\n")
	writeProject("locdist/api.py", "bad user copy")
	shard := writeProject("ldgcc_jobs/job-1/shards/worker-1.jsonl", "worker shard\n")
	writeRuntime("__init__.py", "official runtime")
	writeRuntime("api.py", "official api")
	writeRuntime("generated/gradient_pb2.py", "generated")
	writeRuntime("__pycache__/api.pyc", "cache")

	data, err := Build(PackageRequest{
		ProjectRoot: root, JobID: "job-1", WorkerID: "worker-1",
		Entrypoint: "train.py", DatasetPath: "dataset/train.jsonl", ShardPath: shard,
		RuntimePath: runtimeRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, data)
	if string(files["locdist/api.py"]) != "official api" {
		t.Fatalf("LDGCC runtime was not injected: %q", files["locdist/api.py"])
	}
	if _, ok := files["locdist/__pycache__/api.pyc"]; ok {
		t.Fatal("runtime cache files were packaged")
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

func writeFile(t *testing.T, root, relative, content string) string {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
