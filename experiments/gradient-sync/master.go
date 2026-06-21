package main

import (
	"fmt"
	"sync"
)

// Master coordinates gradient synchronization across all workers.
// It holds the barrier and performs aggregation only once every
// registered worker has submitted its gradient for the current step.
type Master struct {
	mu          sync.Mutex
	cond        *sync.Cond
	totalWorkers int

	// received[workerID] = gradient submitted this step
	received map[string][]float64

	// aggregated result broadcast back to all waiting workers
	aggregated []float64
}

// NewMaster creates a Master expecting exactly n workers.
func NewMaster(n int) *Master {
	m := &Master{
		totalWorkers: n,
		received:     make(map[string][]float64),
	}
	m.cond = sync.NewCond(&m.mu)
	return m
}

// SubmitGradient is called by a worker to hand in its gradient and
// block until all other workers have also submitted (barrier).
// It returns the aggregated gradient once every worker is ready.
func (m *Master) SubmitGradient(workerID string, gradient []float64) []float64 {
	m.mu.Lock()

	// Store this worker's gradient.
	m.received[workerID] = gradient
	fmt.Printf("[Master] Received gradient from %s (%d/%d submitted)\n",
		workerID, len(m.received), m.totalWorkers)

	// If we are the last worker to arrive, aggregate and wake everyone.
	if len(m.received) == m.totalWorkers {
		fmt.Println("[Master] All workers submitted — starting aggregation")
		m.aggregated = aggregate(m.received)
		fmt.Printf("[Master] Aggregated gradient: %v\n", m.aggregated)
		m.cond.Broadcast() // release every waiting worker
	} else {
		// Not the last — wait at the barrier.
		fmt.Printf("[Master] %s is waiting at barrier…\n", workerID)
		for len(m.received) < m.totalWorkers {
			m.cond.Wait()
		}
	}

	result := make([]float64, len(m.aggregated))
	copy(result, m.aggregated)

	m.mu.Unlock()
	return result
}

// ResetStep clears state so the Master can be reused for the next
// training step. Call this after all workers have received their result.
func (m *Master) ResetStep() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.received = make(map[string][]float64)
	m.aggregated = nil
}

// aggregate computes element-wise mean of all submitted gradients.
func aggregate(gradients map[string][]float64) []float64 {
	var length int
	for _, g := range gradients {
		length = len(g)
		break
	}

	sum := make([]float64, length)
	for _, g := range gradients {
		for i, v := range g {
			sum[i] += v
		}
	}

	n := float64(len(gradients))
	result := make([]float64, length)
	for i, s := range sum {
		result[i] = s / n
	}
	return result
}