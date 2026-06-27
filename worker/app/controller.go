package app

import "sync"

type Lifecycle interface {
	Start() error
	Stop() error
	State() (running bool, paired bool)
}

type State struct {
	Running bool   `json:"running"`
	Paired  bool   `json:"paired"`
	Error   string `json:"error,omitempty"`
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
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.lifecycle.Start(); err != nil {
		c.lastError = err.Error()
		return err
	}

	c.lastError = ""
	return nil
}

func (c *Controller) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.lifecycle.Stop(); err != nil {
		c.lastError = err.Error()
		return err
	}

	c.lastError = ""
	return nil
}

func (c *Controller) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()

	running, paired := c.lifecycle.State()
	return State{
		Running: running,
		Paired:  paired,
		Error:   c.lastError,
	}
}
