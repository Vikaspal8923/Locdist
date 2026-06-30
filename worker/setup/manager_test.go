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
	failCUDA    bool
	failPy312   bool
	failPy311   bool
}

func (f *fakeRunner) Run(_ context.Context, _, _ string, name string, args ...string) error {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	if strings.Contains(call, "nvidia-smi") && f.failCUDA {
		return fmt.Errorf("nvidia-smi not found")
	}
	if (strings.Contains(call, "py -3.12") || strings.Contains(call, "python3.12")) && f.failPy312 {
		return fmt.Errorf("Python 3.12 not found")
	}
	if (strings.Contains(call, "py -3.11") || strings.Contains(call, "python3.11")) && f.failPy311 {
		return fmt.Errorf("Python 3.11 not found")
	}
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
	if len(runner.calls) != 4 {
		t.Fatalf("unexpected commands: %v", runner.calls)
	}
	if !strings.Contains(runner.calls[0], "nvidia-smi -L") {
		t.Fatalf("CUDA detection missing: %v", runner.calls)
	}
	if !strings.Contains(runner.calls[1], "py -3.12 -m venv .venv") && !strings.Contains(runner.calls[1], "python3.12 -m venv .venv") {
		t.Fatalf("venv command missing: %v", runner.calls)
	}
	if !strings.Contains(runner.calls[2], "pip install torch --index-url https://download.pytorch.org/whl/cu121") {
		t.Fatalf("CUDA torch install missing: %v", runner.calls)
	}
	if !strings.Contains(runner.calls[3], "pip install grpcio protobuf numpy") {
		t.Fatalf("runtime dependency install missing: %v", runner.calls)
	}
}

func TestSetupInstallsRuntimeDependenciesBeforeUserRequirements(t *testing.T) {
	workspaceManager := preparedWorkspace(t, true)
	runner := &fakeRunner{}
	manager := NewWithRunner(workspaceManager, runner)
	result := manager.Setup(context.Background(), "job-1", false)
	if result.Status != gradient.JobSetupStatus_JOB_SETUP_STATUS_READY {
		t.Fatalf("status = %s, error = %s", result.Status, result.ErrorMessage)
	}
	if len(runner.calls) != 5 {
		t.Fatalf("unexpected commands: %v", runner.calls)
	}
	if !strings.Contains(runner.calls[2], "pip install torch --index-url https://download.pytorch.org/whl/cu121") {
		t.Fatalf("CUDA torch install missing: %v", runner.calls)
	}
	if !strings.Contains(runner.calls[3], "pip install grpcio protobuf numpy") {
		t.Fatalf("runtime dependency install missing: %v", runner.calls)
	}
	if !strings.Contains(runner.calls[4], "pip install -r .ldgcc-user-requirements.txt") {
		t.Fatalf("user requirements install missing: %v", runner.calls)
	}
}

func TestSetupFiltersLDGCCOwnedPackagesFromUserRequirements(t *testing.T) {
	workspaceManager := preparedWorkspaceWithRequirements(t, "torch\nnumpy>=1.26\ngrpcio\nprotobuf\npandas==2.0\n")
	runner := &fakeRunner{}
	manager := NewWithRunner(workspaceManager, runner)
	result := manager.Setup(context.Background(), "job-1", false)
	if result.Status != gradient.JobSetupStatus_JOB_SETUP_STATUS_READY {
		t.Fatalf("status = %s, error = %s", result.Status, result.ErrorMessage)
	}
	path, _ := workspaceManager.Path("job-1")
	filtered, err := os.ReadFile(filepath.Join(path, ".ldgcc-user-requirements.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(filtered) != "pandas==2.0\n" {
		t.Fatalf("filtered requirements = %q", string(filtered))
	}
}

func TestSetupSkipsUserInstallWhenOnlyLDGCCOwnedPackagesExist(t *testing.T) {
	workspaceManager := preparedWorkspaceWithRequirements(t, "torch\ngrpcio\nprotobuf\nnumpy\n")
	runner := &fakeRunner{}
	manager := NewWithRunner(workspaceManager, runner)
	result := manager.Setup(context.Background(), "job-1", false)
	if result.Status != gradient.JobSetupStatus_JOB_SETUP_STATUS_READY {
		t.Fatalf("status = %s, error = %s", result.Status, result.ErrorMessage)
	}
	if len(runner.calls) != 4 {
		t.Fatalf("unexpected commands: %v", runner.calls)
	}
}

func TestSetupFailsWhenCUDADetectionFails(t *testing.T) {
	workspaceManager := preparedWorkspace(t, false)
	runner := &fakeRunner{failCUDA: true}
	manager := NewWithRunner(workspaceManager, runner)
	result := manager.Setup(context.Background(), "job-1", false)
	if result.Status != gradient.JobSetupStatus_JOB_SETUP_STATUS_FAILED {
		t.Fatalf("status = %s", result.Status)
	}
	if !strings.Contains(result.ErrorMessage, "requires an NVIDIA CUDA Worker") {
		t.Fatalf("unexpected error: %s", result.ErrorMessage)
	}
}

func TestSetupFallsBackToPython311WhenPython312IsMissing(t *testing.T) {
	workspaceManager := preparedWorkspace(t, false)
	runner := &fakeRunner{failPy312: true}
	manager := NewWithRunner(workspaceManager, runner)
	result := manager.Setup(context.Background(), "job-1", false)
	if result.Status != gradient.JobSetupStatus_JOB_SETUP_STATUS_READY {
		t.Fatalf("status = %s, error = %s", result.Status, result.ErrorMessage)
	}
	joined := strings.Join(runner.calls, "\n")
	if !strings.Contains(joined, "py -3.11 -m venv .venv") && !strings.Contains(joined, "python3.11 -m venv .venv") {
		t.Fatalf("Python 3.11 fallback missing: %v", runner.calls)
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
	requirementsText := ""
	if requirements {
		requirementsText = "example==1.0\n"
	}
	return preparedWorkspaceWithRequirements(t, requirementsText)
}

func preparedWorkspaceWithRequirements(t *testing.T, requirementsText string) *workspace.Manager {
	t.Helper()
	manager := workspace.New(filepath.Join(t.TempDir(), "workspaces"))
	files := map[string]string{"train.py": "print('ok')", "dataset/train.jsonl": "one\n", "job_config.json": "{}"}
	if requirementsText != "" {
		files["requirements.txt"] = requirementsText
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
