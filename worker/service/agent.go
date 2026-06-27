package service

import (
	"fmt"
	"log"
	"sync"

	"github.com/Vikaspal8923/Locdist/worker/discovery"
	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	workergrpc "github.com/Vikaspal8923/Locdist/worker/grpc"
	"github.com/Vikaspal8923/Locdist/worker/internal/config"
	"github.com/Vikaspal8923/Locdist/worker/masterclient"
	"github.com/Vikaspal8923/Locdist/worker/runtimebridge"
	"github.com/Vikaspal8923/Locdist/worker/status"
)

type Agent struct {
	mu         sync.Mutex
	config     config.Config
	advertiser discovery.Advertiser
	server     *workergrpc.Server
	client     *masterclient.Client
	running    bool
	paired     bool
}

func New(cfg config.Config, advertiser discovery.Advertiser) *Agent {
	return &Agent{
		config:     cfg,
		advertiser: advertiser,
	}
}

func (a *Agent) Start() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.running {
		return fmt.Errorf("worker is already running")
	}

	synchronizer := runtimebridge.Synchronizer(
		runtimebridge.UnavailableSynchronizer{},
	)

	if a.config.WorkerID != "" {
		client, err := masterclient.New(a.config)
		if err != nil {
			return fmt.Errorf("create master client: %w", err)
		}

		if err := registerPairedWorker(a.config, client); err != nil {
			client.Close()
			return err
		}

		a.client = client
		a.paired = true
		synchronizer = client
	}

	runtimeBridge := runtimebridge.New(synchronizer)
	server, err := workergrpc.NewServer(a.config, runtimeBridge)
	if err != nil {
		a.closeClient()
		return fmt.Errorf("create worker server: %w", err)
	}

	go func() {
		if err := server.Start(); err != nil {
			log.Printf("worker server stopped: %v", err)
		}
	}()

	pairingStatus := "unpaired"
	if a.paired {
		pairingStatus = "paired"
	}

	if err := a.advertiser.Start(
		discovery.Metadata{
			Name:            a.config.WorkerName,
			Host:            a.config.Host,
			Port:            a.config.Port,
			ProtocolVersion: 1,
			PairingStatus:   pairingStatus,
		},
	); err != nil {
		server.Stop()
		a.closeClient()
		a.paired = false
		return err
	}

	a.server = server
	a.running = true

	log.Printf(
		"worker %q is discoverable on the LAN (%s)",
		a.config.WorkerName,
		pairingStatus,
	)

	return nil
}

func (a *Agent) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.running {
		return nil
	}

	discoveryErr := a.advertiser.Stop()
	a.server.Stop()
	a.closeClient()

	a.server = nil
	a.running = false
	a.paired = false

	return discoveryErr
}

func (a *Agent) State() (running bool, paired bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.running, a.paired
}

func (a *Agent) closeClient() {
	if a.client != nil {
		a.client.Close()
		a.client = nil
	}
}

func registerPairedWorker(
	cfg config.Config,
	client *masterclient.Client,
) error {
	registration, err := client.Register(
		&gradient.RegisterWorkerRequest{
			WorkerId: cfg.WorkerID,
			Host:     cfg.Host,
			GrpcPort: cfg.Port,
		},
	)
	if err != nil {
		return fmt.Errorf("register worker with master: %w", err)
	}
	if !registration.GetRegistered() {
		return fmt.Errorf("master rejected worker registration")
	}

	statusManager := status.New(cfg.WorkerID, client)
	if err := statusManager.Set(
		gradient.WorkerStatus_WORKER_STATUS_IDLE,
		"",
	); err != nil {
		return fmt.Errorf("report initial worker status: %w", err)
	}

	log.Printf(
		"worker %s registered with master and is IDLE",
		cfg.WorkerID,
	)

	return nil
}
