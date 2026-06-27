package app

import (
	"sync"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
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
	PairedMaster   string                  `json:"paired_master,omitempty"`
	PendingPairing *PairingRequest         `json:"pending_pairing,omitempty"`
	Error          string                  `json:"error,omitempty"`
}

type Controller struct {
	mu        sync.Mutex
	lifecycle Lifecycle
	lastError string
}

func NewController(lifecycle Lifecycle) *Controller {
	return &Controller{lifecycle: lifecycle}
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
		Error:      c.lastError,
	}
	if record, ok := c.lifecycle.PairingRecord(); ok {
		state.PairedMaster = record.MasterName
	}
	if request, ok := c.lifecycle.PendingPairing(); ok {
		state.PendingPairing = &PairingRequest{
			MasterID:   request.GetMasterId(),
			MasterName: request.GetMasterName(),
		}
	}
	return state
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
