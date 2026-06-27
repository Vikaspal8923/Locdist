package runtimebridge

import (
	"fmt"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
)

type UnavailableSynchronizer struct{}

func (UnavailableSynchronizer) Synchronize(
	request *gradient.GradientSubmission,
) (*gradient.AggregatedGradientResponse, error) {
	return nil, fmt.Errorf("worker is not paired with a master")
}
