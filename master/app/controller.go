package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Vikaspal8923/Locdist/master/discovery"
	"github.com/Vikaspal8923/Locdist/master/jobs"
	"github.com/Vikaspal8923/Locdist/master/orchestrator"
	"github.com/Vikaspal8923/Locdist/master/pairing"
	"github.com/Vikaspal8923/Locdist/master/workers"
)

type Backend struct {
	Jobs        *jobs.Manager
	Workers     *workers.Manager
	Preparer    *orchestrator.Preparer
	Setup       *orchestrator.SetupCoordinator
	Training    *orchestrator.TrainingCoordinator
	Lifecycle   *orchestrator.LifecycleCoordinator
	ResultsRoot string
}

type Worker struct {
	ID            string `json:"id"`
	Instance      string `json:"instance"`
	Address       string `json:"address"`
	PairingStatus string `json:"pairing_status"`
	RequestStatus string `json:"request_status,omitempty"`
	Error         string `json:"error,omitempty"`
}

type Controller struct {
	mu         sync.RWMutex
	discovered *discovery.Registry
	pairing    *pairing.Service
	requests   map[string]Worker
	backend    *Backend
	events     *EventHub
	runCancel  context.CancelFunc
	runDone    chan struct{}
	operation  string
}

func NewController(
	discovered *discovery.Registry,
	pairingService *pairing.Service,
) *Controller {
	return &Controller{
		discovered: discovered,
		pairing:    pairingService,
		requests:   make(map[string]Worker),
		events:     NewEventHub(),
	}
}

func (c *Controller) AttachBackend(backend Backend) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.backend = &backend
}
func (c *Controller) Events() *EventHub { return c.events }

func (c *Controller) Workers() []Worker {
	discoveredWorkers := c.discovered.Workers()
	workers := make([]Worker, 0, len(discoveredWorkers))

	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, item := range discoveredWorkers {
		worker := Worker{
			ID:            item.ID,
			Instance:      item.Instance,
			Address:       discovery.Address(item),
			PairingStatus: item.PairingStatus,
		}
		if request, ok := c.requests[item.ID]; ok {
			if request.RequestStatus != "PAIRED" {
				worker.RequestStatus = request.RequestStatus
				worker.Error = request.Error
			}
		}
		workers = append(workers, worker)
	}
	return workers
}

func (c *Controller) Pair(id string) error {
	if id == "" {
		return fmt.Errorf("Worker id is required")
	}

	c.mu.Lock()
	if request, ok := c.requests[id]; ok &&
		request.RequestStatus == "PENDING" {
		c.mu.Unlock()
		return fmt.Errorf("pairing request is already pending")
	}
	c.requests[id] = Worker{RequestStatus: "PENDING"}
	c.mu.Unlock()

	go func() {
		_, err := c.pairing.Pair(context.Background(), id)

		c.mu.Lock()
		defer c.mu.Unlock()
		if err != nil {
			c.requests[id] = Worker{
				RequestStatus: "FAILED",
				Error:         err.Error(),
			}
			c.events.Publish("worker.pairing_failed", map[string]string{"id": id, "error": err.Error()})
			return
		}
		c.requests[id] = Worker{
			RequestStatus: "PAIRED",
		}
		c.events.Publish("worker.connected", map[string]string{"id": id})
	}()
	c.events.Publish("worker.pairing_pending", map[string]string{"id": id})

	return nil
}

func (c *Controller) backendValue() (*Backend, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.backend == nil {
		return nil, fmt.Errorf("job backend is not configured")
	}
	return c.backend, nil
}

func (c *Controller) Prepare(ctx context.Context, projectRoot string) error {
	if err := c.beginOperation("prepare"); err != nil {
		return err
	}
	defer c.endOperation()
	backend, err := c.backendValue()
	if err != nil {
		return err
	}
	job, _, err := backend.Preparer.PrepareAndDistribute(ctx, projectRoot)
	if err != nil {
		c.events.Publish("job.prepare_failed", map[string]string{"error": err.Error()})
		return err
	}
	c.events.Publish("job.prepared", map[string]string{"job_id": job.JobID})
	return nil
}

func (c *Controller) Setup(ctx context.Context, retryFailed bool) error {
	if err := c.beginOperation("setup"); err != nil {
		return err
	}
	defer c.endOperation()
	backend, err := c.backendValue()
	if err != nil {
		return err
	}
	c.events.Publish("job.setup_started", nil)
	if retryFailed {
		err = backend.Setup.RetryFailed(ctx)
	} else {
		err = backend.Setup.SetupAll(ctx)
	}
	if err != nil {
		c.events.Publish("job.setup_failed", map[string]string{"error": err.Error()})
		return err
	}
	c.events.Publish("job.ready", nil)
	return nil
}

func (c *Controller) Start(ctx context.Context) error {
	if err := c.beginOperation("start"); err != nil {
		return err
	}
	defer c.endOperation()
	backend, err := c.backendValue()
	if err != nil {
		return err
	}
	if err := backend.Training.Start(ctx); err != nil {
		c.events.Publish("job.start_failed", map[string]string{"error": err.Error()})
		return err
	}
	monitorContext, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	c.mu.Lock()
	if c.runCancel != nil {
		c.runCancel()
	}
	c.runCancel = cancel
	c.runDone = done
	c.mu.Unlock()
	c.events.Publish("job.started", nil)
	go func() {
		defer close(done)
		summary, monitorErr := backend.Lifecycle.Monitor(monitorContext, 0)
		if monitorErr != nil {
			c.events.Publish("job.lifecycle_error", map[string]string{"error": monitorErr.Error()})
		}
		if summary != nil {
			if _, err := c.ResultPath(summary.JobID); err == nil {
				c.events.Publish("results.collected", map[string]string{"job_id": summary.JobID})
			}
			eventType := string(summary.Status)
			if summary.Status == jobs.StatusFinished {
				eventType = "completed"
			}
			c.events.Publish("job."+eventType, summary)
		}
		c.mu.Lock()
		if c.runDone == done {
			c.runCancel = nil
			c.runDone = nil
		}
		c.mu.Unlock()
	}()
	return nil
}

func (c *Controller) beginOperation(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.operation != "" {
		return fmt.Errorf("operation %q is already running", c.operation)
	}
	c.operation = name
	return nil
}

func (c *Controller) endOperation() { c.mu.Lock(); c.operation = ""; c.mu.Unlock() }

func (c *Controller) WatchWorkers(ctx context.Context) {
	knownDiscovery := make(map[string]bool)
	knownAvailability := make(map[string]workers.Availability)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		currentDiscovery := make(map[string]bool)
		for _, worker := range c.Workers() {
			currentDiscovery[worker.Instance] = true
			if !knownDiscovery[worker.Instance] {
				knownDiscovery[worker.Instance] = true
				c.events.Publish("worker.discovered", worker)
			}
		}
		for instance := range knownDiscovery {
			if !currentDiscovery[instance] {
				delete(knownDiscovery, instance)
				c.events.Publish("worker.lost", map[string]string{"instance": instance})
			}
		}
		if backend, err := c.backendValue(); err == nil {
			for _, worker := range backend.Workers.States() {
				if previous, exists := knownAvailability[worker.WorkerID]; !exists || previous != worker.Availability {
					knownAvailability[worker.WorkerID] = worker.Availability
					c.events.Publish("worker."+strings.ToLower(string(worker.Availability)), worker)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *Controller) Shutdown(ctx context.Context) {
	c.mu.Lock()
	cancel, done := c.runCancel, c.runDone
	c.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
		}
	}
}

func (c *Controller) Stop() error {
	c.mu.Lock()
	cancel := c.runCancel
	c.mu.Unlock()
	if cancel == nil {
		return fmt.Errorf("no monitored training job is running")
	}
	cancel()
	c.events.Publish("job.stop_requested", nil)
	return nil
}

type State struct {
	Master      map[string]any  `json:"master"`
	Discovered  []Worker        `json:"discovered_workers"`
	Workers     []workers.State `json:"workers"`
	Job         *jobs.JobState  `json:"job"`
	LastSummary *jobs.Summary   `json:"last_summary"`
}

func (c *Controller) State() State {
	state := State{Master: map[string]any{"status": "ready", "version": Version}, Discovered: c.Workers()}
	backend, err := c.backendValue()
	if err != nil {
		return state
	}
	state.Workers = backend.Workers.States()
	if job, err := backend.Jobs.CurrentJob(); err == nil {
		state.Job = job
	}
	if summary, ok := backend.Jobs.LastSummary(); ok {
		state.LastSummary = summary
	}
	return state
}

func (c *Controller) ResultPath(jobID string) (string, error) {
	backend, err := c.backendValue()
	if err != nil {
		return "", err
	}
	if jobID == "" || filepath.Base(jobID) != jobID {
		return "", fmt.Errorf("invalid job_id")
	}
	path := filepath.Join(backend.ResultsRoot, jobID)
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return "", fmt.Errorf("results were not found")
	}
	return path, nil
}

const Version = "1.0.0"
