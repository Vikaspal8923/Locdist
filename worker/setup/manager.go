package setup

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

type pythonCommand struct {
	Name string
	Args []string
}

var runtimeRequirements = []string{
	"grpcio",
	"protobuf",
	"numpy",
}

const cudaTorchIndexURL = "https://download.pytorch.org/whl/cu121"
const venvMarkerFile = ".ldgcc-venv-path"
const cacheReadyFile = "READY"

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
	requirements := filepath.Join(directory, "requirements.txt")
	filteredRequirements := ""
	hasUserRequirements := false
	torchRequirements := []string{"torch"}
	if _, err := os.Stat(requirements); err == nil {
		filtered, hasUser, requestedTorch, err := filterUserRequirements(requirements)
		if err != nil {
			return failed(err, logPath)
		}
		filteredRequirements = filtered
		hasUserRequirements = hasUser
		torchRequirements = mergeTorchRequirements(torchRequirements, requestedTorch)
	} else if !os.IsNotExist(err) {
		return failed(err, logPath)
	}
	fingerprint, err := dependencyFingerprint(directory, filteredRequirements, hasUserRequirements, torchRequirements)
	if err != nil {
		return failed(err, logPath)
	}
	cacheEnvPath := filepath.Join(dependencyCacheRoot(directory), fingerprint, "venv")
	if cacheReady(cacheEnvPath) {
		log.Printf("reusing cached LDGCC Python environment for job %q", jobID)
		if err := writeVenvMarker(directory, cacheEnvPath); err != nil {
			return failed(err, logPath)
		}
		return Result{Status: gradient.JobSetupStatus_JOB_SETUP_STATUS_READY, LogPath: logPath}
	}
	if err := os.RemoveAll(cacheEnvPath); err != nil {
		return failed(err, logPath)
	}
	if err := os.Remove(filepath.Join(directory, venvMarkerFile)); err != nil && !os.IsNotExist(err) {
		return failed(err, logPath)
	}
	log.Printf("creating cached Python environment for job %q", jobID)
	if err := m.createVenv(ctx, directory, logPath, cacheEnvPath); err != nil {
		return failed(err, logPath)
	}
	if err := os.MkdirAll(cacheEnvPath, 0o700); err != nil {
		return failed(err, logPath)
	}
	log.Printf("cached Python environment ready for job %q", jobID)
	venvPython := venvPythonPath(cacheEnvPath)
	log.Printf("installing LDGCC CUDA PyTorch runtime for job %q", jobID)
	cudaTorchInstallArgs := append([]string{"-m", "pip", "install"}, torchRequirements...)
	cudaTorchInstallArgs = append(cudaTorchInstallArgs, "--index-url", cudaTorchIndexURL)
	if err := m.runner.Run(ctx, directory, logPath, venvPython, cudaTorchInstallArgs...); err != nil {
		_ = os.RemoveAll(filepath.Dir(cacheEnvPath))
		return failed(fmt.Errorf("install LDGCC CUDA PyTorch runtime: %w", err), logPath)
	}
	log.Printf("installing LDGCC Python runtime dependencies for job %q", jobID)
	runtimeInstallArgs := append([]string{"-m", "pip", "install"}, runtimeRequirements...)
	if err := m.runner.Run(ctx, directory, logPath, venvPython, runtimeInstallArgs...); err != nil {
		_ = os.RemoveAll(filepath.Dir(cacheEnvPath))
		return failed(fmt.Errorf("install LDGCC runtime dependencies: %w", err), logPath)
	}
	if hasUserRequirements {
		if err := m.runner.Run(ctx, directory, logPath, venvPython, "-m", "pip", "install", "-r", filteredRequirements); err != nil {
			_ = os.RemoveAll(filepath.Dir(cacheEnvPath))
			return failed(fmt.Errorf("install requirements: %w", err), logPath)
		}
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(cacheEnvPath), cacheReadyFile), []byte("ready\n"), 0o600); err != nil {
		return failed(err, logPath)
	}
	if err := writeVenvMarker(directory, cacheEnvPath); err != nil {
		return failed(err, logPath)
	}
	return Result{Status: gradient.JobSetupStatus_JOB_SETUP_STATUS_READY, LogPath: logPath}
}

func (m *Manager) createVenv(ctx context.Context, directory, logPath, venvPath string) error {
	var failures []string
	for _, candidate := range pythonCandidates() {
		args := append(append([]string{}, candidate.Args...), "-m", "venv", venvPath)
		if err := m.runner.Run(ctx, directory, logPath, candidate.Name, args...); err == nil {
			return nil
		} else {
			failures = append(failures, fmt.Sprintf("%s %s: %v", candidate.Name, strings.Join(candidate.Args, " "), err))
			_ = os.RemoveAll(venvPath)
		}
	}
	return fmt.Errorf(
		"create Python environment: LDGCC CUDA PyTorch requires Python 3.11 or 3.12. Install Python 3.12 on this Worker and ensure it is available via the Windows py launcher. Attempts: %s",
		strings.Join(failures, "; "),
	)
}

func dependencyCacheRoot(workspacePath string) string {
	workspacesRoot := filepath.Dir(workspacePath)
	workerRoot := filepath.Dir(workspacesRoot)
	return filepath.Join(workerRoot, "env-cache")
}

func dependencyFingerprint(directory, filteredRequirements string, hasUserRequirements bool, torchRequirements []string) (string, error) {
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "goos=%s\narch=%s\ncuda_index=%s\n", runtime.GOOS, runtime.GOARCH, cudaTorchIndexURL)
	_, _ = fmt.Fprintf(hash, "torch=%s\n", strings.Join(torchRequirements, "\n"))
	_, _ = fmt.Fprintf(hash, "runtime=%s\n", strings.Join(runtimeRequirements, "\n"))
	if hasUserRequirements {
		data, err := os.ReadFile(filepath.Join(directory, filteredRequirements))
		if err != nil {
			return "", err
		}
		_, _ = hash.Write(data)
	}
	return hex.EncodeToString(hash.Sum(nil))[:32], nil
}

func cacheReady(venvPath string) bool {
	if _, err := os.Stat(filepath.Join(filepath.Dir(venvPath), cacheReadyFile)); err != nil {
		return false
	}
	info, err := os.Stat(venvPythonPath(venvPath))
	return err == nil && info.Mode().IsRegular()
}

func writeVenvMarker(directory, venvPath string) error {
	if err := os.RemoveAll(filepath.Join(directory, ".venv")); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, venvMarkerFile), []byte(venvPath+"\n"), 0o600)
}

func pythonCandidates() []pythonCommand {
	if runtime.GOOS == "windows" {
		return []pythonCommand{
			{Name: "py", Args: []string{"-3.12"}},
			{Name: "py", Args: []string{"-3.11"}},
			{Name: "python", Args: nil},
			{Name: "python3", Args: nil},
		}
	}
	return []pythonCommand{
		{Name: "python3.12", Args: nil},
		{Name: "python3.11", Args: nil},
		{Name: "python3", Args: nil},
		{Name: "python", Args: nil},
	}
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

func filterUserRequirements(requirementsPath string) (string, bool, []string, error) {
	file, err := os.Open(requirementsPath)
	if err != nil {
		return "", false, nil, err
	}
	defer file.Close()

	owned := map[string]struct{}{
		"torch":       {},
		"torchvision": {},
		"grpcio":      {},
		"protobuf":    {},
		"numpy":       {},
		"torchaudio":  {},
	}
	torchFamily := map[string]struct{}{
		"torch":       {},
		"torchvision": {},
		"torchaudio":  {},
	}
	var kept []string
	var requestedTorch []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if packageName, ok := requirementPackageName(line); ok {
			if _, exists := owned[packageName]; exists {
				if _, isTorchFamily := torchFamily[packageName]; isTorchFamily {
					requestedTorch = append(requestedTorch, packageName)
				}
				continue
			}
		}
		if strings.TrimSpace(line) != "" && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			kept = append(kept, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", false, nil, err
	}
	if len(kept) == 0 {
		return "", false, requestedTorch, nil
	}
	filteredPath := filepath.Join(filepath.Dir(requirementsPath), ".ldgcc-user-requirements.txt")
	if err := os.WriteFile(filteredPath, []byte(strings.Join(kept, "\n")+"\n"), 0o600); err != nil {
		return "", false, nil, err
	}
	return filepath.Base(filteredPath), true, requestedTorch, nil
}

func mergeTorchRequirements(base []string, requested []string) []string {
	seen := make(map[string]struct{}, len(base)+len(requested))
	var result []string
	for _, requirement := range append(base, requested...) {
		if _, ok := seen[requirement]; ok {
			continue
		}
		seen[requirement] = struct{}{}
		result = append(result, requirement)
	}
	return result
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
