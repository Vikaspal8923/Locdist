package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Vikaspal8923/Locdist/master/aggregator"
	masterapp "github.com/Vikaspal8923/Locdist/master/app"
	"github.com/Vikaspal8923/Locdist/master/coordinator"
	"github.com/Vikaspal8923/Locdist/master/discovery"
	"github.com/Vikaspal8923/Locdist/master/grpc"
	"github.com/Vikaspal8923/Locdist/master/internal/config"
	"github.com/Vikaspal8923/Locdist/master/jobs"
	"github.com/Vikaspal8923/Locdist/master/orchestrator"
	"github.com/Vikaspal8923/Locdist/master/pairing"
	"github.com/Vikaspal8923/Locdist/master/workers"
)

func main() {
	configPath := flag.String("config", "master_config.json", "Master configuration file")
	dataDirectory := flag.String("data-dir", "", "persistent LDGCC data directory")
	appHost := flag.String("app-host", "", "localhost application API host")
	appPort := flag.String("app-port", "", "localhost application API port; use 0 for automatic")
	sessionToken := flag.String("session-token", "", "application API bearer token")
	flag.Parse()

	if _, err := os.Stat(*configPath); os.IsNotExist(err) {
		if err := config.Save(*configPath, config.Default()); err != nil {
			log.Fatalf("create default master config: %v", err)
		}
	} else if err != nil {
		log.Fatalf("read master config: %v", err)
	}

	cfg, err := config.Load(
		*configPath,
	)
	if err != nil {
		log.Fatalf(
			"failed to load master config: %v",
			err,
		)
	}
	if *appHost != "" {
		cfg.AppHost = *appHost
	}
	if *appPort != "" {
		cfg.AppPort = *appPort
	}
	dataRoot := *dataDirectory
	if dataRoot == "" {
		dataRoot = "."
	}
	if err := os.MkdirAll(dataRoot, 0o700); err != nil {
		log.Fatalf("create data directory: %v", err)
	}
	if !filepath.IsAbs(cfg.PairingPath) {
		cfg.PairingPath = filepath.Join(dataRoot, cfg.PairingPath)
	}
	jobsRoot := filepath.Join(dataRoot, "ldgcc_jobs")
	resultsRoot := filepath.Join(dataRoot, "ldgcc_results")
	token := *sessionToken
	if token == "" {
		token, err = randomToken()
		if err != nil {
			log.Fatalf("generate session token: %v", err)
		}
	}

	aggregatorService := aggregator.New()
	aggregatorService.SetMetricsPath(filepath.Join(dataRoot, "ldgcc_master_sync_metrics.jsonl"))

	jobManager := jobs.New()

	workerManager, err := workers.NewPersistent(
		workers.NewFilePairingStore(cfg.PairingPath),
	)
	if err != nil {
		log.Fatalf("failed to load Master pairings: %v", err)
	}

	discoveredWorkers := discovery.NewRegistry()
	discoveryService := discovery.NewService(
		discovery.NewBrowser(2*time.Second),
		discoveredWorkers,
		3*time.Second,
		10*time.Second,
	)
	discoveryContext, stopDiscovery := context.WithCancel(
		context.Background(),
	)
	defer stopDiscovery()
	go discoveryService.Run(discoveryContext)

	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-discoveryContext.Done():
				return
			case now := <-ticker.C:
				workerManager.Sweep(
					now,
					6*time.Second,
					12*time.Second,
				)
			}
		}
	}()

	pairingService := pairing.New(
		cfg,
		discoveredWorkers,
		workerManager,
	)
	appController := masterapp.NewController(
		discoveredWorkers,
		pairingService,
	)
	preparer := orchestrator.NewPreparer(jobManager, workerManager, jobsRoot)
	setupCoordinator := orchestrator.NewSetupCoordinator(jobManager, workerManager)
	trainingCoordinator := orchestrator.NewTrainingCoordinator(jobManager, workerManager)
	lifecycleCoordinator := orchestrator.NewLifecycleCoordinator(jobManager, workerManager, jobsRoot, aggregatorService)
	appController.AttachBackend(masterapp.Backend{Jobs: jobManager, Workers: workerManager, Preparer: preparer, Setup: setupCoordinator, Training: trainingCoordinator, Lifecycle: lifecycleCoordinator, ResultsRoot: resultsRoot})
	go appController.WatchWorkers(discoveryContext)
	appServer, err := masterapp.NewSecureServer(
		cfg.AppHost,
		cfg.AppPort,
		token,
		appController,
	)
	if err != nil {
		log.Fatalf("failed to create Master App: %v", err)
	}

	shutdownSignal := make(chan os.Signal, 1)
	appServer.SetShutdown(func() {
		select {
		case shutdownSignal <- syscall.SIGTERM:
		default:
		}
	})
	sessionPath := filepath.Join(dataRoot, "master-session.json")
	if err := writeSession(sessionPath, appServer.Address(), token); err != nil {
		log.Fatalf("write Master session: %v", err)
	}
	defer os.Remove(sessionPath)

	coordinatorService := coordinator.New(
		aggregatorService,
		jobManager,
		workerManager,
	)

	server, err := grpc.NewServer(
		cfg,
		coordinatorService,
	)
	if err != nil {
		log.Fatalf(
			"failed to create master server: %v",
			err,
		)
	}

	go func() {

		log.Printf(
			"master service listening on port %s",
			cfg.Port,
		)

		if err := server.Start(); err != nil {
			log.Fatalf(
				"master server stopped: %v",
				err,
			)
		}
	}()

	go func() {
		log.Printf("LDGCC Master App available at %s", appServer.Address())
		if err := appServer.Start(); err != nil {
			log.Fatalf("Master App stopped: %v", err)
		}
	}()

	signal.Notify(
		shutdownSignal,
		os.Interrupt,
		syscall.SIGTERM,
	)

	<-shutdownSignal

	log.Println(
		"shutdown signal received",
	)

	stopDiscovery()
	shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	appController.Shutdown(shutdownContext)
	shutdownCancel()
	if err := appServer.Stop(); err != nil {
		log.Printf("failed to stop Master App cleanly: %v", err)
	}
	server.Stop()

	log.Println(
		"master service stopped",
	)
}

func randomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func writeSession(path, address, token string) error {
	host, port := "127.0.0.1", ""
	if parsed, err := url.Parse(address); err == nil {
		if parsedHost, parsedPort, splitErr := net.SplitHostPort(parsed.Host); splitErr == nil {
			host, port = parsedHost, parsedPort
		}
	}
	data, err := json.MarshalIndent(map[string]any{"pid": os.Getpid(), "host": host, "port": port, "address": address, "version": masterapp.Version, "session_token": token, "started_at": time.Now().UTC()}, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".master-session-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
