package discovery

import (
	"testing"
	"time"
)

func TestRegistryUpsertAndPrune(t *testing.T) {
	registry := NewRegistry()
	now := time.Now()

	if !registry.Upsert(
		Worker{
			Instance: "Vikas-Laptop",
			Address:  "192.168.1.20",
			GRPCPort: 50051,
			LastSeen: now.Add(-time.Minute),
		},
	) {
		t.Fatal("expected first sighting to be new")
	}

	if registry.Upsert(
		Worker{
			Instance: "Vikas-Laptop",
			Address:  "192.168.1.21",
			GRPCPort: 50051,
			LastSeen: now,
		},
	) {
		t.Fatal("expected repeated sighting to update existing Worker")
	}

	workers := registry.Workers()
	if len(workers) != 1 {
		t.Fatalf("expected one Worker, got %d", len(workers))
	}
	if workers[0].Address != "192.168.1.21" {
		t.Fatalf("expected refreshed address, got %q", workers[0].Address)
	}

	if removed := registry.Prune(now.Add(-time.Second)); len(removed) != 0 {
		t.Fatal("recent Worker was pruned")
	}
	if removed := registry.Prune(now.Add(time.Second)); len(removed) != 1 {
		t.Fatalf("expected one pruned Worker, got %d", len(removed))
	}
}
