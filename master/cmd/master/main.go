package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Vikaspal8923/Locdist/master/aggregator"
	"github.com/Vikaspal8923/Locdist/master/coordinator"
	"github.com/Vikaspal8923/Locdist/master/discovery"
	"github.com/Vikaspal8923/Locdist/master/grpc"
	"github.com/Vikaspal8923/Locdist/master/internal/config"
	"github.com/Vikaspal8923/Locdist/master/jobs"
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

	workerManager := workers.New()

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
	server.Stop()

	log.Println(
		"master service stopped",
	)
}
