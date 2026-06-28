package setup

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	"github.com/Vikaspal8923/Locdist/worker/workspace"
)

type fakeRunner struct {
	calls       []string
	failInstall bool
}

func (f *fakeRunner) Run(_ context.Context, _, _ string, name string, args ...string) error {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	if strings.Contains(call, "pip install") && f.failInstall {
		f.failInstall = false
		return fmt.Errorf("dependency error")
	}
	return nil
}

func TestSetupWithoutRequirementsBecomesReady(t *testing.T) {
	workspaceManager := preparedWorkspace(t, false)
	runner := &fakeRunner{}
	manager := NewWithRunner(workspaceManager, runner)
	result := manager.Setup(context.Background(), "job-1", false)
	if result.Status != gradient.JobSetupStatus_JOB_SETUP_STATUS_READY {
		t.Fatalf("status = %s, error = %s", result.Status, result.ErrorMessage)
	}
	if len(runner.calls) != 1 || !strings.Contains(runner.calls[0], "-m venv .venv") {
		t.Fatalf("unexpected commands: %v", runner.calls)
	}
}

func TestFailedDependencySetupCanRetryWithoutDeletingProject(t *testing.T) {
	workspaceManager := preparedWorkspace(t, true)
	runner := &fakeRunner{failInstall: true}
	manager := NewWithRunner(workspaceManager, runner)
	first := manager.Setup(context.Background(), "job-1", false)
	if first.Status != gradient.JobSetupStatus_JOB_SETUP_STATUS_FAILED {
		t.Fatalf("first status = %s", first.Status)
	}
	second := manager.Setup(context.Background(), "job-1", true)
	if second.Status != gradient.JobSetupStatus_JOB_SETUP_STATUS_READY {
		t.Fatalf("retry status = %s, error = %s", second.Status, second.ErrorMessage)
	}
	path, _ := workspaceManager.Path("job-1")
	if _, err := os.Stat(filepath.Join(path, "train.py")); err != nil {
		t.Fatal("retry removed project code")
	}
}

func preparedWorkspace(t *testing.T, requirements bool) *workspace.Manager {
	t.Helper()
	manager := workspace.New(filepath.Join(t.TempDir(), "workspaces"))
	files := map[string]string{"train.py": "print('ok')", "dataset/train.jsonl": "one\n", "job_config.json": "{}"}
	if requirements {
		files["requirements.txt"] = "example==1.0\n"
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range files {
		entry, _ := writer.Create(name)
		_, _ = entry.Write([]byte(content))
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Prepare("job-1", "train.py", "dataset/train.jsonl", buffer.Bytes()); err != nil {
		t.Fatal(err)
	}
	return manager
}
