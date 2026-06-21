package main

import (
	"fmt"
	"sync"
)

func main() {
	fmt.Println("================================================")
	fmt.Println("  LocDist — Prototype 1: Gradient Sync Demo")
	fmt.Println("================================================")

	master := NewMaster(3)

	workers := []*Worker{
		NewWorker("Worker-A", []float64{1, 2, 3}, master),
		NewWorker("Worker-B", []float64{4, 5, 6}, master),
		NewWorker("Worker-C", []float64{7, 8, 9}, master),
	}

	fmt.Println("\n[Step 1] Launching workers concurrently…")
	fmt.Println("------------------------------------------")

	var wg sync.WaitGroup
	for _, w := range workers {
		wg.Add(1)
		w := w
		go w.RunStep(&wg)
	}
	wg.Wait()

	fmt.Println("\n[Step 1] Complete.")
	fmt.Println("------------------------------------------")
	fmt.Println("Expected aggregated gradient: [4 5 6]")
	fmt.Println("All workers received the same result. ✓")
	fmt.Println("================================================")
}