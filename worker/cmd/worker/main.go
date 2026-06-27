package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

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
	if err := agent.Start(); err != nil {
		log.Fatalf("failed to start worker: %v", err)
	}

	shutdownSignal := make(chan os.Signal, 1)
	signal.Notify(
		shutdownSignal,
		os.Interrupt,
		syscall.SIGTERM,
	)
	<-shutdownSignal

	log.Println("shutdown signal received")
	if err := agent.Stop(); err != nil {
		log.Printf("failed to stop worker cleanly: %v", err)
	}
	log.Println("worker service stopped")
}
