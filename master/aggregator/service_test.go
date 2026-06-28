package aggregator

import (
	"testing"
	"time"

	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
)

func TestAbortJobReleasesBarrierAndResetsRound(t *testing.T) {
	service := New()
	done := make(chan error, 1)
	go func() {
		_, err := service.Aggregate(&gradient.GradientSubmission{RuntimeVersion: 1, JobId: "job-1", WorkerId: "worker-1", Chunks: []*gradient.GradientChunk{{HasGrad: false}}}, 2)
		done <- err
	}()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		service.mutex.Lock()
		waiting := service.currentRound.WaitingReceivers
		service.mutex.Unlock()
		if waiting == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	service.AbortJob("worker disconnected")
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("blocked aggregation returned no abort error")
		}
	case <-time.After(time.Second):
		t.Fatal("aggregation barrier was not released")
	}
	if service.CurrentRound() != 1 {
		t.Fatalf("round = %d", service.CurrentRound())
	}
}
