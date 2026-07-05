package aggregator

import (
	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
)

type RoundState struct {
	Round int

	Gradients map[string]*gradient.GradientSubmission

	Response *gradient.AggregatedGradientResponse

	Err error

	WaitingReceivers int
}

type ChunkRoundState struct {
	Key string

	LayerKey string

	Round int

	Submissions map[string]*gradient.GradientChunkSubmission

	Response *gradient.AggregatedGradientChunkResponse

	Err error

	WaitingReceivers int
}
