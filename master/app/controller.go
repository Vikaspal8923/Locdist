package app

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Vikaspal8923/Locdist/master/discovery"
	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
	"github.com/Vikaspal8923/Locdist/master/jobs"
	"github.com/Vikaspal8923/Locdist/master/orchestrator"
	"github.com/Vikaspal8923/Locdist/master/pairing"
	"github.com/Vikaspal8923/Locdist/master/workers"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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
	network    map[string]NetworkCheck
}

type NetworkCheck struct {
	WorkerID  string  `json:"worker_id"`
	LatencyMS int64   `json:"latency_ms,omitempty"`
	Mbps      float64 `json:"mbps,omitempty"`
	Quality   string  `json:"quality"`
	Error     string  `json:"error,omitempty"`
	CheckedAt string  `json:"checked_at"`
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
		network:    make(map[string]NetworkCheck),
	}
}

func (c *Controller) AttachBackend(backend Backend) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.backend = &backend
}

func (c *Controller) CheckWorkerNetwork(ctx context.Context) error {
	backend, err := c.backendValue()
	if err != nil {
		return err
	}
	states := backend.Workers.States()
	if len(states) == 0 {
		return fmt.Errorf("no paired Workers are available")
	}
	results := make(map[string]NetworkCheck, len(states))
	for _, worker := range states {
		result := c.checkOneWorker(ctx, backend.Workers, worker)
		results[worker.WorkerID] = result
	}
	c.mu.Lock()
	for workerID, result := range results {
		c.network[workerID] = result
	}
	c.mu.Unlock()
	c.events.Publish("workers.network_checked", results)
	return nil
}

func (c *Controller) checkOneWorker(ctx context.Context, manager *workers.Manager, worker workers.State) NetworkCheck {
	checkedAt := time.Now().Format(time.RFC3339)
	if worker.Availability != workers.AvailabilityOnline {
		return NetworkCheck{WorkerID: worker.WorkerID, Quality: "offline", Error: "Worker is not online", CheckedAt: checkedAt}
	}
	pairing, ok := manager.Pairing(worker.WorkerID)
	if !ok {
		return NetworkCheck{WorkerID: worker.WorkerID, Quality: "error", Error: "pairing credentials are missing", CheckedAt: checkedAt}
	}
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	connection, err := grpc.DialContext(dialCtx, net.JoinHostPort(worker.Host, worker.GRPCPort), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		return NetworkCheck{WorkerID: worker.WorkerID, Quality: "error", Error: err.Error(), CheckedAt: checkedAt}
	}
	defer connection.Close()
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	start := time.Now()
	_, err = gradient.NewWorkerBridgeClient(connection).Heartbeat(requestCtx, &gradient.WorkerHeartbeat{
		WorkerId:     worker.WorkerID,
		MasterId:     pairing.MasterID,
		PairingToken: pairing.Token,
		Status:       worker.Status,
		JobId:        worker.JobID,
	})
	if err != nil {
		return NetworkCheck{WorkerID: worker.WorkerID, Quality: "error", Error: err.Error(), CheckedAt: checkedAt}
	}
	latency := time.Since(start).Milliseconds()
	mbps, err := benchmarkUpload(requestCtx, gradient.NewWorkerBridgeClient(connection), worker, pairing)
	if err != nil {
		return NetworkCheck{WorkerID: worker.WorkerID, LatencyMS: latency, Quality: "error", Error: err.Error(), CheckedAt: checkedAt}
	}
	return NetworkCheck{WorkerID: worker.WorkerID, LatencyMS: latency, Mbps: mbps, Quality: networkQuality(latency, mbps), CheckedAt: checkedAt}
}

func benchmarkUpload(ctx context.Context, client gradient.WorkerBridgeClient, worker workers.State, pairing workers.Pairing) (float64, error) {
	stream, err := client.BenchmarkUpload(ctx)
	if err != nil {
		return 0, err
	}
	chunk := make([]byte, 256<<10)
	const totalBytes = 8 << 20
	for sent := 0; sent < totalBytes; sent += len(chunk) {
		size := len(chunk)
		if remaining := totalBytes - sent; remaining < size {
			size = remaining
		}
		if err := stream.Send(&gradient.BenchmarkChunk{
			WorkerId:     worker.WorkerID,
			MasterId:     pairing.MasterID,
			PairingToken: pairing.Token,
			Data:         chunk[:size],
		}); err != nil {
			return 0, err
		}
	}
	result, err := stream.CloseAndRecv()
	if err != nil {
		return 0, err
	}
	return result.GetMbps(), nil
}

func networkQuality(latencyMS int64, mbps float64) string {
	if mbps > 0 && mbps < 50 {
		return "poor"
	}
	if mbps >= 50 && mbps < 150 {
		return "fair"
	}
	switch {
	case latencyMS <= 10:
		return "excellent"
	case latencyMS <= 30:
		return "good"
	case latencyMS <= 80:
		return "fair"
	default:
		return "poor"
	}
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
			currentDiscovery[worker.ID] = true
			if !knownDiscovery[worker.ID] {
				knownDiscovery[worker.ID] = true
				c.events.Publish("worker.discovered", worker)
			}
		}
		for id := range knownDiscovery {
			if !currentDiscovery[id] {
				delete(knownDiscovery, id)
				c.events.Publish("worker.lost", map[string]string{"id": id})
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
	Master      map[string]any          `json:"master"`
	Discovered  []Worker                `json:"discovered_workers"`
	Workers     []workers.State         `json:"workers"`
	Job         *jobs.JobState          `json:"job"`
	LastSummary *jobs.Summary           `json:"last_summary"`
	Network     map[string]NetworkCheck `json:"network,omitempty"`
}

func (c *Controller) State() State {
	state := State{Master: map[string]any{"status": "ready", "version": Version}, Discovered: c.Workers()}
	backend, err := c.backendValue()
	if err != nil {
		return state
	}
	state.Workers = backend.Workers.States()
	c.mu.RLock()
	if len(c.network) > 0 {
		state.Network = make(map[string]NetworkCheck, len(c.network))
		for workerID, result := range c.network {
			state.Network[workerID] = result
		}
	}
	c.mu.RUnlock()
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
