package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Vikaspal8923/Locdist/master/aggregator"
	masterapp "github.com/Vikaspal8923/Locdist/master/app"
	"github.com/Vikaspal8923/Locdist/master/coordinator"
	"github.com/Vikaspal8923/Locdist/master/discovery"
	"github.com/Vikaspal8923/Locdist/master/grpc"
	"github.com/Vikaspal8923/Locdist/master/internal/config"
	"github.com/Vikaspal8923/Locdist/master/jobs"
	"github.com/Vikaspal8923/Locdist/master/pairing"
	"github.com/Vikaspal8923/Locdist/master/workers"
)

func main() {

	cfg, err := config.Load(
		"master_config.json",
	)
	if err != nil {
		log.Fatalf(
			"failed to load master config: %v",
			err,
		)
	}

	aggregatorService := aggregator.New()

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
	appServer, err := masterapp.NewServer(
		cfg.AppPort,
		appController,
	)
	if err != nil {
		log.Fatalf("failed to create Master App: %v", err)
	}

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

	shutdownSignal := make(
		chan os.Signal,
		1,
	)

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
	if err := appServer.Stop(); err != nil {
		log.Printf("failed to stop Master App cleanly: %v", err)
	}
	server.Stop()

	log.Println(
		"master service stopped",
	)
}
