package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Vikaspal8923/Locdist/master/aggregator"
	"github.com/Vikaspal8923/Locdist/master/grpc"
	"github.com/Vikaspal8923/Locdist/master/internal/config"
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

	server, err := grpc.NewServer(
		cfg,
		aggregatorService,
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

	server.Stop()

	log.Println(
		"master service stopped",
	)
}
