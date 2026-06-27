package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Vikaspal8923/Locdist/worker/app"
	"github.com/Vikaspal8923/Locdist/worker/discovery"
	"github.com/Vikaspal8923/Locdist/worker/internal/config"
	"github.com/Vikaspal8923/Locdist/worker/pairing"
	"github.com/Vikaspal8923/Locdist/worker/service"
)

func main() {
	cfg, err := config.Load("worker_config.json")
	if err != nil {
		log.Fatalf("failed to load worker config: %v", err)
	}

	pairingManager, err := pairing.NewManager(
		pairing.NewFileStore(cfg.PairingPath),
	)
	if err != nil {
		log.Fatalf("failed to load Worker pairing: %v", err)
	}

	agent := service.New(
		cfg,
		discovery.NewAdvertiser(),
		pairingManager,
	)
	controller := app.NewController(agent)

	server, err := app.NewServer(cfg.AppPort, controller)
	if err != nil {
		log.Fatalf("failed to create Worker App: %v", err)
	}

	go func() {
		log.Printf("LDGCC Worker App available at %s", server.Address())
		if err := server.Start(); err != nil {
			log.Fatalf("Worker App stopped: %v", err)
		}
	}()

	shutdownSignal := make(chan os.Signal, 1)
	signal.Notify(
		shutdownSignal,
		os.Interrupt,
		syscall.SIGTERM,
	)
	<-shutdownSignal

	_ = controller.Stop()
	if err := server.Stop(); err != nil {
		log.Printf("failed to stop Worker App cleanly: %v", err)
	}
}
