package app

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	"github.com/Vikaspal8923/Locdist/worker/internal/config"
	"github.com/Vikaspal8923/Locdist/worker/pairing"
	"github.com/Vikaspal8923/Locdist/worker/service"
)

type Lifecycle interface {
	Start() error
	Stop() error
	AcceptPairing() error
	RejectPairing() error
	ResetPairing() error
	State() (running bool, connection service.ConnectionState)
	Config() config.Config
	UpdateConfig(config.Config) error
	PendingPairing() (*gradient.PairWorkerRequest, bool)
	PairingRecord() (*pairing.Record, bool)
}

type PairingRequest struct {
	MasterID   string `json:"master_id"`
	MasterName string `json:"master_name"`
}

type State struct {
	Running        bool                    `json:"running"`
	Connection     service.ConnectionState `json:"connection"`
	Status         string                  `json:"status"`
	Config         PublicConfig            `json:"config"`
	PairedMaster   string                  `json:"paired_master,omitempty"`
	PairedMasterID string                  `json:"paired_master_id,omitempty"`
	PendingPairing *PairingRequest         `json:"pending_pairing,omitempty"`
	Error          string                  `json:"error,omitempty"`
	JobLogs        *JobLogs                `json:"job_logs,omitempty"`
}

type JobLogs struct {
	JobID     string `json:"job_id"`
	Setup     string `json:"setup"`
	Training  string `json:"training"`
	SetupPath string `json:"setup_path,omitempty"`
	TrainPath string `json:"training_path,omitempty"`
	Truncated bool   `json:"truncated"`
}

type Controller struct {
	mu         sync.Mutex
	lifecycle  Lifecycle
	configPath string
	lastError  string
}

type PublicConfig struct {
	WorkerName    string `json:"worker_name"`
	Host          string `json:"host"`
	GRPCPort      string `json:"grpc_port"`
	AppPort       string `json:"app_port"`
	WorkspaceRoot string `json:"workspace_root"`
	PairingPath   string `json:"pairing_path"`
}

type ConfigUpdate struct {
	WorkerName    string `json:"worker_name"`
	Host          string `json:"host"`
	GRPCPort      string `json:"grpc_port"`
	WorkspaceRoot string `json:"workspace_root"`
}

func NewController(lifecycle Lifecycle, configPath ...string) *Controller {
	path := "worker_config.json"
	if len(configPath) > 0 && configPath[0] != "" {
		path = configPath[0]
	}
	return &Controller{lifecycle: lifecycle, configPath: path}
}

func (c *Controller) Start() error {
	return c.run(c.lifecycle.Start)
}

func (c *Controller) Stop() error {
	return c.run(c.lifecycle.Stop)
}

func (c *Controller) AcceptPairing() error {
	return c.run(c.lifecycle.AcceptPairing)
}

func (c *Controller) RejectPairing() error {
	return c.run(c.lifecycle.RejectPairing)
}

func (c *Controller) ResetPairing() error {
	return c.run(c.lifecycle.ResetPairing)
}

func (c *Controller) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()

	running, connection := c.lifecycle.State()
	state := State{
		Running:    running,
		Connection: connection,
		Status:     statusLabel(running, connection),
		Config:     publicConfig(c.lifecycle.Config()),
		Error:      c.lastError,
	}
	state.JobLogs = loadJobLogs(state.Config.WorkspaceRoot)
	if record, ok := c.lifecycle.PairingRecord(); ok {
		state.PairedMaster = record.MasterName
		state.PairedMasterID = record.MasterID
	}
	if request, ok := c.lifecycle.PendingPairing(); ok {
		state.PendingPairing = &PairingRequest{
			MasterID:   request.GetMasterId(),
			MasterName: request.GetMasterName(),
		}
	}
	return state
}

func loadJobLogs(workspaceRoot string) *JobLogs {
	entries, err := os.ReadDir(workspaceRoot)
	if err != nil {
		return nil
	}
	type jobEntry struct {
		name    string
		modTime int64
	}
	jobs := make([]jobEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		jobs = append(jobs, jobEntry{name: entry.Name(), modTime: info.ModTime().UnixNano()})
	}
	if len(jobs) == 0 {
		return nil
	}
	sort.Slice(jobs, func(left, right int) bool {
		return jobs[left].modTime > jobs[right].modTime
	})
	jobID := jobs[0].name
	setupPath := filepath.Join(workspaceRoot, jobID, "logs", "setup.log")
	trainingPath := filepath.Join(workspaceRoot, jobID, "logs", "training.log")
	setup, setupTruncated := readLogTail(setupPath, 32<<10)
	training, trainingTruncated := readLogTail(trainingPath, 32<<10)
	if setup == "" && training == "" {
		return &JobLogs{JobID: jobID}
	}
	return &JobLogs{
		JobID:     jobID,
		Setup:     setup,
		Training:  training,
		SetupPath: setupPath,
		TrainPath: trainingPath,
		Truncated: setupTruncated || trainingTruncated,
	}
}

func readLogTail(path string, maxBytes int64) (string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		return "", false
	}
	offset := int64(0)
	truncated := false
	if info.Size() > maxBytes {
		offset = info.Size() - maxBytes
		truncated = true
	}
	data := make([]byte, info.Size()-offset)
	if _, err := file.ReadAt(data, offset); err != nil && len(data) == 0 {
		return "", false
	}
	text := string(data)
	if truncated {
		for len(text) > 0 && !utf8.ValidString(text) {
			text = text[1:]
		}
		text = "[showing last 32 KB]\n" + strings.TrimLeft(text, "\r\n")
	}
	return text, truncated
}

func (c *Controller) UpdateConfig(update ConfigUpdate) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	running, _ := c.lifecycle.State()
	if running {
		c.lastError = "stop Worker before changing settings"
		return errors.New(c.lastError)
	}

	next := c.lifecycle.Config()
	if update.WorkerName != "" {
		next.WorkerName = update.WorkerName
	}
	if update.Host != "" {
		next.Host = update.Host
	}
	if update.GRPCPort != "" {
		next.Port = update.GRPCPort
	}
	if update.WorkspaceRoot != "" {
		next.WorkspaceRoot = update.WorkspaceRoot
	}
	if err := c.lifecycle.UpdateConfig(next); err != nil {
		c.lastError = err.Error()
		return err
	}
	if err := config.Save(c.configPath, next); err != nil {
		c.lastError = err.Error()
		return err
	}
	c.lastError = ""
	return nil
}

func (c *Controller) run(action func() error) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := action(); err != nil {
		c.lastError = err.Error()
		return err
	}
	c.lastError = ""
	return nil
}

func publicConfig(cfg config.Config) PublicConfig {
	return PublicConfig{
		WorkerName:    cfg.WorkerName,
		Host:          cfg.Host,
		GRPCPort:      cfg.Port,
		AppPort:       cfg.AppPort,
		WorkspaceRoot: cfg.WorkspaceRoot,
		PairingPath:   cfg.PairingPath,
	}
}

func statusLabel(running bool, connection service.ConnectionState) string {
	if !running {
		return "stopped"
	}
	switch connection {
	case service.ConnectionPairingPending:
		return "pairing pending"
	case service.ConnectionPairedOnline:
		return "connected"
	case service.ConnectionPairedOffline:
		return "paired offline"
	default:
		return "discoverable"
	}
}
