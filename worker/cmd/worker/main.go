package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Vikaspal8923/Locdist/worker/discovery"
	"github.com/Vikaspal8923/Locdist/worker/internal/config"
	"github.com/Vikaspal8923/Locdist/worker/pairing"
	"github.com/Vikaspal8923/Locdist/worker/service"
)

func main() {
	configPath := flag.String("config", "worker_config.json", "Worker configuration file")
	dataDirectory := flag.String("data-dir", "", "persistent LDGCC Worker data directory")
	flag.Parse()

	if _, err := os.Stat(*configPath); os.IsNotExist(err) {
		if err := config.Save(*configPath, config.Default()); err != nil {
			log.Fatalf("create default worker config: %v", err)
		}
	} else if err != nil {
		log.Fatalf("read worker config: %v", err)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("failed to load worker config: %v", err)
	}
	dataRoot := *dataDirectory
	if dataRoot == "" {
		dataRoot = filepath.Dir(*configPath)
		if dataRoot == "." || dataRoot == "" {
			dataRoot = "."
		}
	}
	if err := os.MkdirAll(dataRoot, 0o700); err != nil {
		log.Fatalf("create Worker data directory: %v", err)
	}
	if !filepath.IsAbs(cfg.PairingPath) {
		cfg.PairingPath = filepath.Join(dataRoot, cfg.PairingPath)
	}
	if !filepath.IsAbs(cfg.WorkspaceRoot) {
		cfg.WorkspaceRoot = filepath.Join(dataRoot, cfg.WorkspaceRoot)
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
