package training

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	"github.com/Vikaspal8923/Locdist/worker/workspace"
)

type readiness bool

func (r readiness) IsReady(string) bool { return bool(r) }

func TestArmRequiresReadySetup(t *testing.T) {
	manager := New(preparedTrainingWorkspace(t, "#!/bin/sh\nexit 0\n", `{"entrypoint":"train.py"}`), readiness(false), "50051", nil)
	if result := manager.Arm("job-1"); result.Status != gradient.JobRunStatus_JOB_RUN_STATUS_FAILED {
		t.Fatalf("status = %s", result.Status)
	}
}

func TestReleaseInjectsRuntimeEnvironmentAndCompletes(t *testing.T) {
	workspaceManager := preparedTrainingWorkspace(
		t,
		"#!/bin/sh\necho \"$LDGCC_JOB_ID|$LDGCC_WORKER_ID|$LDGCC_WORKER_HOST|$LDGCC_WORKER_PORT|$LDGCC_TRAINING\"\n",
		`{"entrypoint":"train.py","training":{"gradient_accumulation_steps":10}}`,
	)
	manager := New(workspaceManager, readiness(true), "51000", nil)
	if result := manager.Arm("job-1"); result.Status != gradient.JobRunStatus_JOB_RUN_STATUS_ARMED {
		t.Fatalf("arm = %+v", result)
	}
	if result := manager.Arm("job-1"); result.Status != gradient.JobRunStatus_JOB_RUN_STATUS_FAILED {
		t.Fatal("duplicate arm was accepted")
	}
	if result := manager.Release("job-1", "worker-1"); result.Status != gradient.JobRunStatus_JOB_RUN_STATUS_RUNNING {
		t.Fatalf("release = %+v", result)
	}
	waitForStatus(t, manager, gradient.JobRunStatus_JOB_RUN_STATUS_COMPLETED)
	if result := manager.Status("job-1"); result.ExitCode != 0 {
		t.Fatalf("exit code = %d", result.ExitCode)
	}
	path, _ := workspaceManager.Path("job-1")
	log, err := os.ReadFile(filepath.Join(path, "logs", "training.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "job-1|worker-1|127.0.0.1|51000|{\"gradient_accumulation_steps\":10}") {
		t.Fatalf("environment was not injected: %q", log)
	}
	cleanup := manager.Cleanup("job-1")
	if !strings.Contains(string(cleanup.LogTail), "job-1|worker-1") {
		t.Fatalf("cleanup lost log tail: %q", cleanup.LogTail)
	}
	if _, err := workspaceManager.Path("job-1"); err == nil {
		t.Fatal("workspace was not removed")
	}
}

func TestFailedProcessReportsExitCode(t *testing.T) {
	manager := New(preparedTrainingWorkspace(t, "#!/bin/sh\necho failed\nexit 7\n", `{"entrypoint":"train.py"}`), readiness(true), "50051", nil)
	if result := manager.Arm("job-1"); result.Status != gradient.JobRunStatus_JOB_RUN_STATUS_ARMED {
		t.Fatal(result.ErrorMessage)
	}
	manager.Release("job-1", "worker-1")
	waitForStatus(t, manager, gradient.JobRunStatus_JOB_RUN_STATUS_FAILED)
	if result := manager.Status("job-1"); result.ExitCode != 7 || !strings.Contains(string(result.LogTail), "failed") {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestReleaseUsesCachedVenvMarker(t *testing.T) {
	workspaceManager := preparedTrainingWorkspace(t, "#!/bin/sh\nexit 99\n", `{"entrypoint":"train.py"}`)
	path, _ := workspaceManager.Path("job-1")
	cachedRoot := filepath.Join(filepath.Dir(filepath.Dir(path)), "env-cache", "test", "venv")
	cachedPython := venvPythonPath(cachedRoot)
	if err := os.MkdirAll(filepath.Dir(cachedPython), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachedPython, []byte("#!/bin/sh\necho cached-python\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, ".ldgcc-venv-path"), []byte(cachedRoot+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := New(workspaceManager, readiness(true), "50051", nil)
	if result := manager.Arm("job-1"); result.Status != gradient.JobRunStatus_JOB_RUN_STATUS_ARMED {
		t.Fatal(result.ErrorMessage)
	}
	manager.Release("job-1", "worker-1")
	waitForStatus(t, manager, gradient.JobRunStatus_JOB_RUN_STATUS_COMPLETED)
	log, err := os.ReadFile(filepath.Join(path, "logs", "training.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "cached-python") {
		t.Fatalf("cached python was not used: %q", log)
	}
}

func TestStopCancelsRunningProcess(t *testing.T) {
	manager := New(preparedTrainingWorkspace(t, "#!/bin/sh\nsleep 30\n", `{"entrypoint":"train.py"}`), readiness(true), "50051", nil)
	if result := manager.Arm("job-1"); result.Status != gradient.JobRunStatus_JOB_RUN_STATUS_ARMED {
		t.Fatal(result.ErrorMessage)
	}
	if result := manager.Release("job-1", "worker-1"); result.Status != gradient.JobRunStatus_JOB_RUN_STATUS_RUNNING {
		t.Fatal(result.ErrorMessage)
	}
	if result := manager.Stop("job-1"); result.Status != gradient.JobRunStatus_JOB_RUN_STATUS_CANCELLED {
		t.Fatalf("stop = %+v", result)
	}
}

func preparedTrainingWorkspace(t *testing.T, pythonScript string, jobConfig string) *workspace.Manager {
	t.Helper()
	manager := workspace.New(filepath.Join(t.TempDir(), "workspaces"))
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range map[string]string{"train.py": "print('train')", "dataset/train.jsonl": "one\n", "job_config.json": jobConfig} {
		entry, _ := writer.Create(name)
		_, _ = entry.Write([]byte(content))
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	path, err := manager.Prepare("job-1", "train.py", "dataset/train.jsonl", buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	python := venvPythonPath(filepath.Join(path, ".venv"))
	if err := os.MkdirAll(filepath.Dir(python), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(python, []byte(pythonScript), 0o700); err != nil {
		t.Fatal(err)
	}
	return manager
}

func waitForStatus(t *testing.T, manager *Manager, expected gradient.JobRunStatus) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if manager.Status("job-1").Status == expected {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("status did not become %s: %+v", expected, manager.Status("job-1"))
}
