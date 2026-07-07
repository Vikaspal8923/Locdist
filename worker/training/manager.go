package training

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	"github.com/Vikaspal8923/Locdist/worker/pairing"
	"github.com/Vikaspal8923/Locdist/worker/workspace"
)

type Readiness interface{ IsReady(jobID string) bool }

type jobConfig struct {
	Entrypoint    string          `json:"entrypoint"`
	Communication json.RawMessage `json:"communication,omitempty"`
	Training      json.RawMessage `json:"training,omitempty"`
}

type process struct {
	status        gradient.JobRunStatus
	entrypoint    string
	command       *exec.Cmd
	done          chan struct{}
	communication string
	training      string
	errorMessage  string
	logPath       string
	exitCode      int
}

type Result struct {
	Status       gradient.JobRunStatus
	ErrorMessage string
	LogPath      string
	ExitCode     int
	LogTail      []byte
}

type Manager struct {
	mu          sync.Mutex
	workspace   *workspace.Manager
	readiness   Readiness
	workerPort  string
	pairing     *pairing.Manager
	processes   map[string]*process
	stopTimeout time.Duration
}

func New(
	workspaceManager *workspace.Manager,
	readiness Readiness,
	workerPort string,
	pairingManager *pairing.Manager,
) *Manager {
	return &Manager{
		workspace:   workspaceManager,
		readiness:   readiness,
		workerPort:  workerPort,
		pairing:     pairingManager,
		processes:   make(map[string]*process),
		stopTimeout: 5 * time.Second,
	}
}

func (m *Manager) Arm(jobID string) Result {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.readiness.IsReady(jobID) {
		return failed("job setup is not ready", "")
	}
	if current := m.processes[jobID]; current != nil && (current.status == gradient.JobRunStatus_JOB_RUN_STATUS_ARMED || current.status == gradient.JobRunStatus_JOB_RUN_STATUS_RUNNING) {
		return failed("job is already armed or running", current.logPath)
	}
	directory, err := m.workspace.Path(jobID)
	if err != nil {
		return failed(err.Error(), "")
	}
	data, err := os.ReadFile(filepath.Join(directory, "job_config.json"))
	if err != nil {
		return failed("read job_config.json: "+err.Error(), "")
	}
	var config jobConfig
	if err := json.Unmarshal(data, &config); err != nil || config.Entrypoint == "" {
		return failed("job_config.json has no valid entrypoint", "")
	}
	python, err := pythonPath(directory)
	if err != nil {
		return failed(err.Error(), "")
	}
	for _, required := range []string{python, filepath.Join(directory, filepath.FromSlash(config.Entrypoint))} {
		info, err := os.Stat(required)
		if err != nil || !info.Mode().IsRegular() {
			return failed(fmt.Sprintf("required training file %q is missing", required), "")
		}
	}
	logPath := filepath.Join(directory, "logs", "training.log")
	m.processes[jobID] = &process{status: gradient.JobRunStatus_JOB_RUN_STATUS_ARMED, entrypoint: config.Entrypoint, logPath: logPath, exitCode: -1}
	if len(config.Communication) > 0 {
		m.processes[jobID].communication = string(config.Communication)
	}
	if len(config.Training) > 0 {
		m.processes[jobID].training = string(config.Training)
	}
	return Result{Status: gradient.JobRunStatus_JOB_RUN_STATUS_ARMED, LogPath: logPath}
}

func (m *Manager) Release(jobID, workerID string) Result {
	m.mu.Lock()
	current := m.processes[jobID]
	if current == nil || current.status != gradient.JobRunStatus_JOB_RUN_STATUS_ARMED {
		m.mu.Unlock()
		return failed("job is not armed", "")
	}
	directory, err := m.workspace.Path(jobID)
	if err != nil {
		m.mu.Unlock()
		return failed(err.Error(), current.logPath)
	}
	log, err := os.OpenFile(current.logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		m.mu.Unlock()
		return failed(err.Error(), current.logPath)
	}
	absDirectory, err := filepath.Abs(directory)
	if err != nil {
		log.Close()
		m.mu.Unlock()
		return failed("resolve workspace path: "+err.Error(), current.logPath)
	}
	python, err := pythonPath(absDirectory)
	if err != nil {
		log.Close()
		m.mu.Unlock()
		return failed(err.Error(), current.logPath)
	}
	command := exec.Command(python, current.entrypoint)
	command.Dir = absDirectory
	command.Env = append(os.Environ(), "LDGCC_JOB_ID="+jobID, "LDGCC_WORKER_ID="+workerID, "LDGCC_WORKER_HOST=127.0.0.1", "LDGCC_WORKER_PORT="+m.workerPort)
	if m.pairing != nil {
		if record, ok := m.pairing.Record(); ok {
			command.Env = append(
				command.Env,
				"LDGCC_MASTER_HOST="+record.MasterHost,
				"LDGCC_MASTER_PORT="+record.MasterPort,
				"LDGCC_SYNC_TARGET=master",
			)
		}
	}
	if current.communication != "" {
		command.Env = append(command.Env, "LDGCC_COMMUNICATION="+current.communication)
	}
	if current.training != "" {
		command.Env = append(command.Env, "LDGCC_TRAINING="+current.training)
	}
	command.Stdout, command.Stderr = log, log
	if err := command.Start(); err != nil {
		log.Close()
		m.mu.Unlock()
		return failed("start training: "+err.Error(), current.logPath)
	}
	current.command = command
	current.done = make(chan struct{})
	current.status = gradient.JobRunStatus_JOB_RUN_STATUS_RUNNING
	m.mu.Unlock()
	go m.wait(jobID, current, log)
	return Result{Status: gradient.JobRunStatus_JOB_RUN_STATUS_RUNNING, LogPath: current.logPath}
}

func venvPythonPath(venvPath string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(venvPath, "Scripts", "python.exe")
	}
	return filepath.Join(venvPath, "bin", "python")
}

func pythonPath(workspacePath string) (string, error) {
	marker := filepath.Join(workspacePath, ".ldgcc-venv-path")
	if data, err := os.ReadFile(marker); err == nil {
		venvPath := strings.TrimSpace(string(data))
		if venvPath == "" {
			return "", fmt.Errorf("cached Python environment marker is empty")
		}
		if !filepath.IsAbs(venvPath) {
			return "", fmt.Errorf("cached Python environment path is not absolute")
		}
		return venvPythonPath(venvPath), nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read cached Python environment marker: %w", err)
	}
	return venvPythonPath(filepath.Join(workspacePath, ".venv")), nil
}

func (m *Manager) wait(jobID string, current *process, log *os.File) {
	err := current.command.Wait()
	log.Close()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.processes[jobID] != current {
		return
	}
	if current.status != gradient.JobRunStatus_JOB_RUN_STATUS_CANCELLED {
		if err == nil {
			current.status = gradient.JobRunStatus_JOB_RUN_STATUS_COMPLETED
			current.exitCode = 0
		} else {
			current.status = gradient.JobRunStatus_JOB_RUN_STATUS_FAILED
			current.errorMessage = err.Error()
			current.exitCode = current.command.ProcessState.ExitCode()
		}
	}
	close(current.done)
}

func (m *Manager) Stop(jobID string) Result {
	m.mu.Lock()
	current := m.processes[jobID]
	if current == nil {
		m.mu.Unlock()
		return failed("job is not armed or running", "")
	}
	if current.status == gradient.JobRunStatus_JOB_RUN_STATUS_ARMED {
		current.status = gradient.JobRunStatus_JOB_RUN_STATUS_CANCELLED
		m.mu.Unlock()
		return Result{Status: current.status, LogPath: current.logPath}
	}
	if current.status != gradient.JobRunStatus_JOB_RUN_STATUS_RUNNING {
		result := resultOf(current)
		m.mu.Unlock()
		return result
	}
	current.status = gradient.JobRunStatus_JOB_RUN_STATUS_CANCELLED
	command, done := current.command, current.done
	_ = command.Process.Signal(os.Interrupt)
	m.mu.Unlock()
	select {
	case <-done:
	case <-time.After(m.stopTimeout):
		_ = command.Process.Kill()
		<-done
	}
	return Result{Status: gradient.JobRunStatus_JOB_RUN_STATUS_CANCELLED, LogPath: current.logPath}
}

func (m *Manager) Status(jobID string) Result {
	m.mu.Lock()
	defer m.mu.Unlock()
	if current := m.processes[jobID]; current != nil {
		return resultOf(current)
	}
	return Result{Status: gradient.JobRunStatus_JOB_RUN_STATUS_UNKNOWN}
}

func (m *Manager) Cleanup(jobID string) Result {
	status := m.Status(jobID)
	if status.Status == gradient.JobRunStatus_JOB_RUN_STATUS_ARMED || status.Status == gradient.JobRunStatus_JOB_RUN_STATUS_RUNNING {
		m.Stop(jobID)
		status = m.Status(jobID)
	}
	status.LogTail = readLogTail(status.LogPath)
	if err := m.workspace.Remove(jobID); err != nil {
		return failed("remove workspace: "+err.Error(), status.LogPath)
	}
	m.mu.Lock()
	delete(m.processes, jobID)
	m.mu.Unlock()
	return status
}

func resultOf(value *process) Result {
	return Result{Status: value.status, ErrorMessage: value.errorMessage, LogPath: value.logPath, ExitCode: value.exitCode, LogTail: readLogTail(value.logPath)}
}
func failed(message, logPath string) Result {
	return Result{Status: gradient.JobRunStatus_JOB_RUN_STATUS_FAILED, ErrorMessage: message, LogPath: logPath, ExitCode: -1}
}

const maxLogTailBytes = 1 << 20

func readLogTail(path string) []byte {
	if path == "" {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil
	}
	start := info.Size() - maxLogTailBytes
	if start < 0 {
		start = 0
	}
	if _, err := file.Seek(start, 0); err != nil {
		return nil
	}
	if start == 0 {
		data, _ := os.ReadFile(path)
		return data
	}
	data, _ := io.ReadAll(file)
	return data
}
