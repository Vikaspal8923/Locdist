package discovery

import (
	"context"
	"testing"
	"time"
)

type fakeBrowser struct {
	workers []Worker
}

func (f fakeBrowser) Scan(ctx context.Context) ([]Worker, error) {
	return f.workers, nil
}

func TestServiceStoresDiscoveryResults(t *testing.T) {
	registry := NewRegistry()
	service := NewService(
		fakeBrowser{
			workers: []Worker{
				{
					Instance:      "Vikas-Laptop",
					Address:       "192.168.1.20",
					GRPCPort:      50051,
					PairingStatus: "unpaired",
					LastSeen:      time.Now(),
				},
			},
		},
		registry,
		time.Second,
		time.Minute,
	)

	service.scan(context.Background())

	workers := registry.Workers()
	if len(workers) != 1 {
		t.Fatalf("expected one discovered Worker, got %d", len(workers))
	}
	if workers[0].PairingStatus != "unpaired" {
		t.Fatalf(
			"expected unpaired Worker, got %q",
			workers[0].PairingStatus,
		)
	}
}
