package results

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/Vikaspal8923/Locdist/worker/workspace"
)

func TestManifestCollectsDeclaredOutputsAndLogs(t *testing.T) {
	workspaceManager, root := resultWorkspace(t, `{"outputs":["model/model.pt","reports"]}`)
	writeResult(t, root, "model/model.pt", "model")
	writeResult(t, root, "reports/metrics.json", `{"loss":1}`)
	writeResult(t, root, "logs/training.log", "trained")
	writeResult(t, root, "private.txt", "not declared")
	manager := New(workspaceManager)
	manifest, missing, collectionErrors, err := manager.Manifest("job-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 || len(collectionErrors) != 0 || len(manifest) != 3 {
		t.Fatalf("manifest=%v missing=%v errors=%v", manifest, missing, collectionErrors)
	}
	expected := sha256.Sum256([]byte("model"))
	foundModel := false
	for _, file := range manifest {
		if file.GetPath() == "model/model.pt" {
			foundModel = true
			if file.GetSha256() != hex.EncodeToString(expected[:]) {
				t.Fatal("incorrect checksum")
			}
		}
		if file.GetPath() == "private.txt" {
			t.Fatal("undeclared file was exposed")
		}
	}
	if !foundModel {
		t.Fatal("declared model is missing")
	}
	if _, err := manager.Open("job-1", "private.txt"); err == nil {
		t.Fatal("undeclared file download was allowed")
	}
}

func TestManifestReportsMissingOutput(t *testing.T) {
	workspaceManager, _ := resultWorkspace(t, `{"outputs":["missing.pt"]}`)
	_, missing, _, err := New(workspaceManager).Manifest("job-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0] != "missing.pt" {
		t.Fatalf("missing=%v", missing)
	}
}

func TestManifestRejectsSymlinkInsideOutputDirectory(t *testing.T) {
	workspaceManager, root := resultWorkspace(t, `{"outputs":["reports"]}`)
	writeResult(t, root, "outside.txt", "secret")
	if err := os.MkdirAll(filepath.Join(root, "reports"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "outside.txt"), filepath.Join(root, "reports", "link")); err != nil {
		t.Fatal(err)
	}
	manifest, _, collectionErrors, err := New(workspaceManager).Manifest("job-1")
	if err != nil || len(collectionErrors) == 0 {
		t.Fatalf("manifest=%v errors=%v err=%v", manifest, collectionErrors, err)
	}
}

func resultWorkspace(t *testing.T, config string) (*workspace.Manager, string) {
	t.Helper()
	manager := workspace.New(filepath.Join(t.TempDir(), "workspaces"))
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range map[string]string{"train.py": "x", "dataset/train.jsonl": "x", "job_config.json": config} {
		entry, _ := writer.Create(name)
		_, _ = entry.Write([]byte(content))
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	root, err := manager.Prepare("job-1", "train.py", "dataset/train.jsonl", buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return manager, root
}

func writeResult(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
