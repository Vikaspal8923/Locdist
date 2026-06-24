package runtimebridge

import gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"

type Synchronizer interface {
	Synchronize(
		request *gradient.GradientSubmission,
	) (*gradient.AggregatedGradientResponse, error)
}
