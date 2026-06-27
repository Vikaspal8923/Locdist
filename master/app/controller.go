package app

import (
	"context"
	"fmt"
	"sync"

	"github.com/Vikaspal8923/Locdist/master/discovery"
	"github.com/Vikaspal8923/Locdist/master/pairing"
)

type Worker struct {
	Instance      string `json:"instance"`
	Address       string `json:"address"`
	PairingStatus string `json:"pairing_status"`
	RequestStatus string `json:"request_status,omitempty"`
	Error         string `json:"error,omitempty"`
}

type Controller struct {
	mu         sync.RWMutex
	discovered *discovery.Registry
	pairing    *pairing.Service
	requests   map[string]Worker
}

func NewController(
	discovered *discovery.Registry,
	pairingService *pairing.Service,
) *Controller {
	return &Controller{
		discovered: discovered,
		pairing:    pairingService,
		requests:   make(map[string]Worker),
	}
}

func (c *Controller) Workers() []Worker {
	discoveredWorkers := c.discovered.Workers()
	workers := make([]Worker, 0, len(discoveredWorkers))

	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, item := range discoveredWorkers {
		worker := Worker{
			Instance:      item.Instance,
			Address:       discovery.Address(item),
			PairingStatus: item.PairingStatus,
		}
		if request, ok := c.requests[item.Instance]; ok {
			if request.RequestStatus != "PAIRED" {
				worker.RequestStatus = request.RequestStatus
				worker.Error = request.Error
			}
		}
		workers = append(workers, worker)
	}
	return workers
}

func (c *Controller) Pair(instance string) error {
	if instance == "" {
		return fmt.Errorf("Worker instance is required")
	}

	c.mu.Lock()
	if request, ok := c.requests[instance]; ok &&
		request.RequestStatus == "PENDING" {
		c.mu.Unlock()
		return fmt.Errorf("pairing request is already pending")
	}
	c.requests[instance] = Worker{RequestStatus: "PENDING"}
	c.mu.Unlock()

	go func() {
		_, err := c.pairing.Pair(context.Background(), instance)

		c.mu.Lock()
		defer c.mu.Unlock()
		if err != nil {
			c.requests[instance] = Worker{
				RequestStatus: "FAILED",
				Error:         err.Error(),
			}
			return
		}
		c.requests[instance] = Worker{
			RequestStatus: "PAIRED",
		}
	}()

	return nil
}
