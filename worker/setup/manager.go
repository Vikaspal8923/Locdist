package setup

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	"github.com/Vikaspal8923/Locdist/worker/workspace"
)

type Runner interface {
	Run(ctx context.Context, directory, logPath, name string, args ...string) error
}

type CommandRunner struct{}

var runtimeRequirements = []string{
	"grpcio",
	"protobuf",
	"numpy",
}

const cudaTorchIndexURL = "https://download.pytorch.org/whl/cu121"

func (CommandRunner) Run(ctx context.Context, directory, logPath, name string, args ...string) error {
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer log.Close()
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = directory
	command.Stdout = log
	command.Stderr = log
	return command.Run()
}

type Result struct {
	Status       gradient.JobSetupStatus
	ErrorMessage string
	LogPath      string
}

type Manager struct {
	mu        sync.Mutex
	workspace *workspace.Manager
	runner    Runner
	states    map[string]Result
}

func New(workspaceManager *workspace.Manager) *Manager {
	return NewWithRunner(workspaceManager, CommandRunner{})
}

func NewWithRunner(workspaceManager *workspace.Manager, runner Runner) *Manager {
	return &Manager{workspace: workspaceManager, runner: runner, states: make(map[string]Result)}
}

func (m *Manager) Setup(ctx context.Context, jobID string, retry bool) Result {
	m.mu.Lock()
	current := m.states[jobID]
	if current.Status == gradient.JobSetupStatus_JOB_SETUP_STATUS_SETTING_UP {
		m.mu.Unlock()
		return Result{Status: gradient.JobSetupStatus_JOB_SETUP_STATUS_FAILED, ErrorMessage: "job setup is already running"}
	}
	if current.Status == gradient.JobSetupStatus_JOB_SETUP_STATUS_READY {
		m.mu.Unlock()
		return current
	}
	if current.Status == gradient.JobSetupStatus_JOB_SETUP_STATUS_FAILED && !retry {
		m.mu.Unlock()
		return current
	}
	m.states[jobID] = Result{Status: gradient.JobSetupStatus_JOB_SETUP_STATUS_SETTING_UP}
	m.mu.Unlock()

	result := m.run(ctx, jobID)
	m.mu.Lock()
	m.states[jobID] = result
	m.mu.Unlock()
	return result
}

func (m *Manager) IsReady(jobID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.states[jobID].Status == gradient.JobSetupStatus_JOB_SETUP_STATUS_READY
}

func (m *Manager) Forget(jobID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.states, jobID)
}

func (m *Manager) run(ctx context.Context, jobID string) Result {
	directory, err := m.workspace.Path(jobID)
	if err != nil {
		return failed(err, "")
	}
	logPath := filepath.Join(directory, "logs", "setup.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return failed(err, logPath)
	}
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		return failed(err, logPath)
	}
	if err := m.runner.Run(ctx, directory, logPath, "nvidia-smi", "-L"); err != nil {
		return failed(fmt.Errorf("LDGCC V1 requires an NVIDIA CUDA Worker. No CUDA GPU was detected on this Worker: %w", err), logPath)
	}
	venvPath := filepath.Join(directory, ".venv")
	if err := os.RemoveAll(venvPath); err != nil {
		return failed(err, logPath)
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		return failed(fmt.Errorf("python3 is not installed"), logPath)
	}
	log.Printf("creating private Python environment for job %q", jobID)
	if err := m.runner.Run(ctx, directory, logPath, python, "-m", "venv", ".venv"); err != nil {
		return failed(fmt.Errorf("create Python environment: %w", err), logPath)
	}
	log.Printf("private Python environment ready for job %q", jobID)
	venvPython := venvPythonPath(venvPath)
	log.Printf("installing LDGCC CUDA PyTorch runtime for job %q", jobID)
	if err := m.runner.Run(ctx, directory, logPath, venvPython, "-m", "pip", "install", "torch", "--index-url", cudaTorchIndexURL); err != nil {
		return failed(fmt.Errorf("install LDGCC CUDA PyTorch runtime: %w", err), logPath)
	}
	log.Printf("installing LDGCC Python runtime dependencies for job %q", jobID)
	runtimeInstallArgs := append([]string{"-m", "pip", "install"}, runtimeRequirements...)
	if err := m.runner.Run(ctx, directory, logPath, venvPython, runtimeInstallArgs...); err != nil {
		return failed(fmt.Errorf("install LDGCC runtime dependencies: %w", err), logPath)
	}
	requirements := filepath.Join(directory, "requirements.txt")
	if _, err := os.Stat(requirements); os.IsNotExist(err) {
		return Result{Status: gradient.JobSetupStatus_JOB_SETUP_STATUS_READY, LogPath: logPath}
	} else if err != nil {
		return failed(err, logPath)
	}
	filteredRequirements, hasUserRequirements, err := filterUserRequirements(requirements)
	if err != nil {
		return failed(err, logPath)
	}
	if !hasUserRequirements {
		return Result{Status: gradient.JobSetupStatus_JOB_SETUP_STATUS_READY, LogPath: logPath}
	}
	if err := m.runner.Run(ctx, directory, logPath, venvPython, "-m", "pip", "install", "-r", filteredRequirements); err != nil {
		return failed(fmt.Errorf("install requirements: %w", err), logPath)
	}
	return Result{Status: gradient.JobSetupStatus_JOB_SETUP_STATUS_READY, LogPath: logPath}
}

func venvPythonPath(venvPath string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(venvPath, "Scripts", "python.exe")
	}
	return filepath.Join(venvPath, "bin", "python")
}

func failed(err error, logPath string) Result {
	return Result{Status: gradient.JobSetupStatus_JOB_SETUP_STATUS_FAILED, ErrorMessage: err.Error(), LogPath: logPath}
}

func filterUserRequirements(requirementsPath string) (string, bool, error) {
	file, err := os.Open(requirementsPath)
	if err != nil {
		return "", false, err
	}
	defer file.Close()

	owned := map[string]struct{}{
		"torch":    {},
		"grpcio":   {},
		"protobuf": {},
		"numpy":    {},
	}
	var kept []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if packageName, ok := requirementPackageName(line); ok {
			if _, exists := owned[packageName]; exists {
				continue
			}
		}
		if strings.TrimSpace(line) != "" && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			kept = append(kept, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", false, err
	}
	if len(kept) == 0 {
		return "", false, nil
	}
	filteredPath := filepath.Join(filepath.Dir(requirementsPath), ".ldgcc-user-requirements.txt")
	if err := os.WriteFile(filteredPath, []byte(strings.Join(kept, "\n")+"\n"), 0o600); err != nil {
		return "", false, err
	}
	return filepath.Base(filteredPath), true, nil
}

func requirementPackageName(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "-") {
		return "", false
	}
	if hashIndex := strings.Index(trimmed, "#"); hashIndex >= 0 {
		trimmed = strings.TrimSpace(trimmed[:hashIndex])
	}
	if trimmed == "" {
		return "", false
	}
	for _, separator := range []string{"==", ">=", "<=", "~=", "!=", ">", "<", "[", ";"} {
		if index := strings.Index(trimmed, separator); index >= 0 {
			trimmed = strings.TrimSpace(trimmed[:index])
		}
	}
	return strings.ToLower(trimmed), trimmed != ""
}
