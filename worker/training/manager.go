package training

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	"github.com/Vikaspal8923/Locdist/worker/workspace"
)

type Readiness interface{ IsReady(jobID string) bool }

type jobConfig struct {
	Entrypoint string `json:"entrypoint"`
}

type process struct {
	status       gradient.JobRunStatus
	entrypoint   string
	command      *exec.Cmd
	done         chan struct{}
	errorMessage string
	logPath      string
}

type Result struct {
	Status       gradient.JobRunStatus
	ErrorMessage string
	LogPath      string
}

type Manager struct {
	mu          sync.Mutex
	workspace   *workspace.Manager
	readiness   Readiness
	workerPort  string
	processes   map[string]*process
	stopTimeout time.Duration
}

func New(workspaceManager *workspace.Manager, readiness Readiness, workerPort string) *Manager {
	return &Manager{workspace: workspaceManager, readiness: readiness, workerPort: workerPort, processes: make(map[string]*process), stopTimeout: 5 * time.Second}
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
	for _, required := range []string{filepath.Join(directory, ".venv", "bin", "python"), filepath.Join(directory, filepath.FromSlash(config.Entrypoint))} {
		info, err := os.Stat(required)
		if err != nil || !info.Mode().IsRegular() {
			return failed(fmt.Sprintf("required training file %q is missing", required), "")
		}
	}
	logPath := filepath.Join(directory, "logs", "training.log")
	m.processes[jobID] = &process{status: gradient.JobRunStatus_JOB_RUN_STATUS_ARMED, entrypoint: config.Entrypoint, logPath: logPath}
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
	python := filepath.Join(directory, ".venv", "bin", "python")
	command := exec.Command(python, current.entrypoint)
	command.Dir = directory
	command.Env = append(os.Environ(), "LDGCC_JOB_ID="+jobID, "LDGCC_WORKER_ID="+workerID, "LDGCC_WORKER_HOST=127.0.0.1", "LDGCC_WORKER_PORT="+m.workerPort)
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
		} else {
			current.status = gradient.JobRunStatus_JOB_RUN_STATUS_FAILED
			current.errorMessage = err.Error()
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

func resultOf(value *process) Result {
	return Result{Status: value.status, ErrorMessage: value.errorMessage, LogPath: value.logPath}
}
func failed(message, logPath string) Result {
	return Result{Status: gradient.JobRunStatus_JOB_RUN_STATUS_FAILED, ErrorMessage: message, LogPath: logPath}
}
