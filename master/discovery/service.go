package discovery

import (
	"context"
	"log"
	"time"
)

type Service struct {
	browser      Browser
	registry     *Registry
	scanInterval time.Duration
	expiry       time.Duration
}

func NewService(
	browser Browser,
	registry *Registry,
	scanInterval time.Duration,
	expiry time.Duration,
) *Service {
	return &Service{
		browser:      browser,
		registry:     registry,
		scanInterval: scanInterval,
		expiry:       expiry,
	}
}

func (s *Service) Run(ctx context.Context) {
	s.scan(ctx)

	ticker := time.NewTicker(s.scanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.scan(ctx)
		}
	}
}

func (s *Service) scan(ctx context.Context) {
	workers, err := s.browser.Scan(ctx)
	if err != nil && ctx.Err() == nil {
		log.Printf("worker discovery scan failed: %v", err)
	}

	for _, worker := range workers {
		if s.registry.Upsert(worker) {
			log.Printf(
				"discovered Worker %q at %s (%s)",
				worker.Instance,
				Address(worker),
				worker.PairingStatus,
			)
		}
	}

	for _, worker := range s.registry.Prune(
		time.Now().Add(-s.expiry),
	) {
		log.Printf("Worker %q is no longer discoverable", worker.Instance)
	}
}
