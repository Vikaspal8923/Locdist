package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Vikaspal8923/Locdist/worker/grpc"
	"github.com/Vikaspal8923/Locdist/worker/internal/config"
	"github.com/Vikaspal8923/Locdist/worker/masterclient"
	"github.com/Vikaspal8923/Locdist/worker/runtimebridge"
)

func main() {

	cfg, err := config.Load(
		"worker_config.json",
	)
	if err != nil {
		log.Fatalf(
			"failed to load worker config: %v",
			err,
		)
	}

	masterClient, err := masterclient.New(
		cfg,
	)
	if err != nil {
		log.Fatalf(
			"failed to create master client: %v",
			err,
		)
	}
	defer masterClient.Close()

	runtimeBridge := runtimebridge.New(
		masterClient,
	)

	server, err := grpc.NewServer(
		cfg,
		runtimeBridge,
	)
	if err != nil {
		log.Fatalf(
			"failed to create worker server: %v",
			err,
		)
	}

	go func() {
		log.Printf(
			"worker service listening on port %s",
			cfg.Port,
		)

		if err := server.Start(); err != nil {
			log.Fatalf(
				"worker server stopped: %v",
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
		"worker service stopped",
	)
}
