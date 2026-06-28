package app

import (
	"errors"
	"sync"

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
