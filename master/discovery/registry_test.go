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

	if !registry.Upsert(
		Worker{
			Instance: "Vikas-Laptop",
			Address:  "192.168.1.21",
			GRPCPort: 50051,
			LastSeen: now,
		},
	) {
		t.Fatal("expected same name at different address to be a different Worker")
	}

	workers := registry.Workers()
	if len(workers) != 2 {
		t.Fatalf("expected two Workers, got %d", len(workers))
	}

	if registry.Upsert(
		Worker{
			Instance: "Vikas-Laptop",
			Address:  "192.168.1.20",
			GRPCPort: 50051,
			LastSeen: now,
		},
	) {
		t.Fatal("expected same name/address/port to update existing Worker")
	}

	if removed := registry.Prune(now.Add(-time.Second)); len(removed) != 0 {
		t.Fatal("recent Worker was pruned")
	}
	if removed := registry.Prune(now.Add(time.Second)); len(removed) != 2 {
		t.Fatalf("expected two pruned Workers, got %d", len(removed))
	}
}
