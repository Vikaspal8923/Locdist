package main

import (
	"fmt"
	"sync"
)

// Worker represents a single training node.
// In V1 everything runs in-process; the Master pointer replaces the
// network call that will exist in future prototypes.
type Worker struct {
	ID       string
	Gradient []float64
	master   *Master
}

// NewWorker creates a worker with a pre-assigned fake gradient.
func NewWorker(id string, gradient []float64, m *Master) *Worker {
	return &Worker{
		ID:       id,
		Gradient: gradient,
		master:   m,
	}
}

// RunStep simulates one training step:
//  1. "Compute" gradient (already set as a field — stands in for backward pass)
//  2. Submit to Master and block at barrier
//  3. Receive aggregated gradient and print
func (w *Worker) RunStep(wg *sync.WaitGroup) {
	defer wg.Done()

	fmt.Printf("[%s] Computed local gradient: %v\n", w.ID, w.Gradient)
	fmt.Printf("[%s] Submitting to Master…\n", w.ID)

	// This call blocks until ALL workers have submitted.
	aggregated := w.master.SubmitGradient(w.ID, w.Gradient)

	fmt.Printf("[%s] Received aggregated gradient: %v\n", w.ID, aggregated)
}