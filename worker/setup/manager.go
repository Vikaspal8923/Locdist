package setup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	"github.com/Vikaspal8923/Locdist/worker/workspace"
)

type Runner interface {
	Run(ctx context.Context, directory, logPath, name string, args ...string) error
}

type CommandRunner struct{}

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
	venvPath := filepath.Join(directory, ".venv")
	if err := os.RemoveAll(venvPath); err != nil {
		return failed(err, logPath)
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		return failed(fmt.Errorf("python3 is not installed"), logPath)
	}
	if err := m.runner.Run(ctx, directory, logPath, python, "-m", "venv", ".venv"); err != nil {
		return failed(fmt.Errorf("create Python environment: %w", err), logPath)
	}
	requirements := filepath.Join(directory, "requirements.txt")
	if _, err := os.Stat(requirements); os.IsNotExist(err) {
		return Result{Status: gradient.JobSetupStatus_JOB_SETUP_STATUS_READY, LogPath: logPath}
	} else if err != nil {
		return failed(err, logPath)
	}
	venvPython := filepath.Join(venvPath, "bin", "python")
	if err := m.runner.Run(ctx, directory, logPath, venvPython, "-m", "pip", "install", "-r", "requirements.txt"); err != nil {
		return failed(fmt.Errorf("install requirements: %w", err), logPath)
	}
	return Result{Status: gradient.JobSetupStatus_JOB_SETUP_STATUS_READY, LogPath: logPath}
}

func failed(err error, logPath string) Result {
	return Result{Status: gradient.JobSetupStatus_JOB_SETUP_STATUS_FAILED, ErrorMessage: err.Error(), LogPath: logPath}
}
