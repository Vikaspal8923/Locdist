package service

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Vikaspal8923/Locdist/worker/discovery"
	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	workergrpc "github.com/Vikaspal8923/Locdist/worker/grpc"
	"github.com/Vikaspal8923/Locdist/worker/internal/config"
	"github.com/Vikaspal8923/Locdist/worker/masterclient"
	"github.com/Vikaspal8923/Locdist/worker/pairing"
	"github.com/Vikaspal8923/Locdist/worker/runtimebridge"
	"github.com/Vikaspal8923/Locdist/worker/status"
)

type ConnectionState string

const (
	ConnectionUnpaired       ConnectionState = "UNPAIRED"
	ConnectionPairingPending ConnectionState = "PAIRING_PENDING"
	ConnectionPairedOnline   ConnectionState = "PAIRED_ONLINE"
	ConnectionPairedOffline  ConnectionState = "PAIRED_OFFLINE"
)

type Agent struct {
	mu            sync.Mutex
	config        config.Config
	advertiser    discovery.Advertiser
	pairing       *pairing.Manager
	server        *workergrpc.Server
	runtimeBridge *runtimebridge.Service
	client        *masterclient.Client
	running       bool
	connection    ConnectionState
	heartbeatStop context.CancelFunc
}

func New(
	cfg config.Config,
	advertiser discovery.Advertiser,
	pairingManager ...*pairing.Manager,
) *Agent {
	var manager *pairing.Manager
	if len(pairingManager) > 0 {
		manager = pairingManager[0]
	}
	return &Agent{
		config:     cfg,
		advertiser: advertiser,
		pairing:    manager,
		connection: ConnectionUnpaired,
	}
}

func (a *Agent) Start() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.running {
		return fmt.Errorf("worker is already running")
	}
	if a.pairing == nil {
		return fmt.Errorf("pairing manager is required")
	}

	a.runtimeBridge = runtimebridge.New(
		runtimebridge.UnavailableSynchronizer{},
	)
	server, err := workergrpc.NewServer(
		a.config,
		a.runtimeBridge,
		a.pairing,
	)
	if err != nil {
		return fmt.Errorf("create worker server: %w", err)
	}

	go func() {
		if err := server.Start(); err != nil {
			log.Printf("worker server stopped: %v", err)
		}
	}()

	a.server = server
	a.running = true
	a.connection = ConnectionUnpaired

	if record, ok := a.pairing.Record(); ok {
		a.connection = ConnectionPairedOffline
		if err := a.connect(record); err != nil {
			log.Printf("saved Master is offline: %v", err)
		}
	}

	if err := a.startAdvertisement(); err != nil {
		a.stopLocked()
		return err
	}
	a.startHeartbeat()

	log.Printf(
		"worker %q is discoverable on the LAN (%s)",
		a.config.WorkerName,
		a.pairingStatus(),
	)
	return nil
}

func (a *Agent) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stopLocked()
}

func (a *Agent) AcceptPairing() error {
	if err := a.pairing.Accept(); err != nil {
		return err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	record, ok := a.pairing.Record()
	if !ok {
		return fmt.Errorf("accepted pairing was not stored")
	}
	a.connection = ConnectionPairedOffline
	if err := a.connect(record); err != nil {
		return fmt.Errorf("pairing saved, but Master connection failed: %w", err)
	}
	return a.refreshAdvertisement()
}

func (a *Agent) RejectPairing() error {
	return a.pairing.Reject()
}

func (a *Agent) ResetPairing() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.connection == ConnectionPairingPending {
		return fmt.Errorf("cannot reset while pairing is pending")
	}

	if record, paired := a.pairing.Record(); paired && a.client != nil {
		_, err := a.client.Unpair(
			&gradient.UnpairWorkerRequest{
				WorkerId:     record.WorkerID,
				MasterId:     record.MasterID,
				PairingToken: record.PairingToken,
			},
		)
		if err != nil {
			log.Printf(
				"Master could not acknowledge pairing reset: %v",
				err,
			)
		}
	}

	a.closeClient()
	if a.runtimeBridge != nil {
		a.runtimeBridge.SetSynchronizer(
			runtimebridge.UnavailableSynchronizer{},
		)
	}
	if err := a.pairing.Reset(); err != nil {
		return err
	}
	a.connection = ConnectionUnpaired

	if a.running {
		return a.refreshAdvertisement()
	}
	return nil
}

func (a *Agent) State() (bool, ConnectionState) {
	a.mu.Lock()
	defer a.mu.Unlock()

	connection := a.connection
	if _, pending := a.pairing.Pending(); pending {
		connection = ConnectionPairingPending
	}
	return a.running, connection
}

func (a *Agent) PendingPairing() (*gradient.PairWorkerRequest, bool) {
	return a.pairing.Pending()
}

func (a *Agent) PairingRecord() (*pairing.Record, bool) {
	return a.pairing.Record()
}

func (a *Agent) connect(record *pairing.Record) error {
	cfg := a.config
	cfg.MasterHost = record.MasterHost
	cfg.MasterPort = record.MasterPort

	client, err := masterclient.New(cfg)
	if err != nil {
		return fmt.Errorf("create Master client: %w", err)
	}
	if err := registerPairedWorker(a.config, record, client); err != nil {
		client.Close()
		return err
	}

	a.closeClient()
	a.client = client
	a.runtimeBridge.SetSynchronizer(client)
	a.connection = ConnectionPairedOnline
	return nil
}

func (a *Agent) startAdvertisement() error {
	return a.advertiser.Start(
		discovery.Metadata{
			Name:            a.config.WorkerName,
			Host:            a.config.Host,
			Port:            a.config.Port,
			ProtocolVersion: 1,
			PairingStatus:   a.pairingStatus(),
		},
	)
}

func (a *Agent) refreshAdvertisement() error {
	if err := a.advertiser.Stop(); err != nil {
		return err
	}
	return a.startAdvertisement()
}

func (a *Agent) pairingStatus() string {
	if _, paired := a.pairing.Record(); paired {
		return "paired"
	}
	return "unpaired"
}

func (a *Agent) stopLocked() error {
	if !a.running {
		return nil
	}

	if a.heartbeatStop != nil {
		a.heartbeatStop()
		a.heartbeatStop = nil
	}
	if record, paired := a.pairing.Record(); paired && a.client != nil {
		_, _ = a.client.GoingOffline(
			&gradient.WorkerOfflineRequest{
				WorkerId:     record.WorkerID,
				MasterId:     record.MasterID,
				PairingToken: record.PairingToken,
			},
		)
	}

	discoveryErr := a.advertiser.Stop()
	a.server.Stop()
	a.closeClient()

	a.server = nil
	a.runtimeBridge = nil
	a.running = false
	if _, paired := a.pairing.Record(); paired {
		a.connection = ConnectionPairedOffline
	} else {
		a.connection = ConnectionUnpaired
	}
	return discoveryErr
}

func (a *Agent) startHeartbeat() {
	ctx, cancel := context.WithCancel(context.Background())
	a.heartbeatStop = cancel

	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.sendHeartbeat()
			}
		}
	}()
}

func (a *Agent) sendHeartbeat() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.running {
		return
	}
	record, paired := a.pairing.Record()
	if !paired {
		return
	}

	if a.client == nil {
		if err := a.connect(record); err != nil {
			a.connection = ConnectionPairedOffline
			return
		}
	}

	_, err := a.client.Heartbeat(
		&gradient.WorkerHeartbeat{
			WorkerId:     record.WorkerID,
			MasterId:     record.MasterID,
			PairingToken: record.PairingToken,
			Status: gradient.
				WorkerStatus_WORKER_STATUS_IDLE,
		},
	)
	if err != nil {
		a.closeClient()
		a.runtimeBridge.SetSynchronizer(
			runtimebridge.UnavailableSynchronizer{},
		)
		a.connection = ConnectionPairedOffline
		return
	}
	a.connection = ConnectionPairedOnline
}

func (a *Agent) closeClient() {
	if a.client != nil {
		a.client.Close()
		a.client = nil
	}
}

func registerPairedWorker(
	cfg config.Config,
	record *pairing.Record,
	client *masterclient.Client,
) error {
	registration, err := client.Register(
		&gradient.RegisterWorkerRequest{
			WorkerId:     record.WorkerID,
			Host:         cfg.Host,
			GrpcPort:     cfg.Port,
			MasterId:     record.MasterID,
			PairingToken: record.PairingToken,
		},
	)
	if err != nil {
		return fmt.Errorf("register Worker with Master: %w", err)
	}
	if !registration.GetRegistered() {
		return fmt.Errorf("Master rejected Worker registration")
	}

	statusManager := status.New(record.WorkerID, client)
	if err := statusManager.Set(
		gradient.WorkerStatus_WORKER_STATUS_IDLE,
		"",
	); err != nil {
		return fmt.Errorf("report initial Worker status: %w", err)
	}

	log.Printf(
		"worker %s registered with Master and is IDLE",
		record.WorkerID,
	)
	return nil
}
