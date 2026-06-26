package aggregator

import (
	"sync"

	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
)

type Service struct {
	currentRound *RoundState

	mutex sync.Mutex
	cond  *sync.Cond
}

func New() *Service {

	service := &Service{
		currentRound: &RoundState{
			Round: 1,
			Gradients: make(
				map[string]*gradient.GradientSubmission,
			),
		},
	}

	service.cond = sync.NewCond(&service.mutex)

	return service
}

func (s *Service) StoreGradient(
	request *gradient.GradientSubmission,
) {

	s.currentRound.Gradients[request.WorkerId] = request
}

func (s *Service) ReceivedWorkers() int {

	return len(
		s.currentRound.Gradients,
	)
}

func (s *Service) BarrierReached(
	expectedWorkers int,
) bool {

	return s.ReceivedWorkers() == expectedWorkers
}

func (s *Service) ResetRound() {

	s.resetRoundLocked()
}

func (s *Service) resetRoundLocked() {

	s.currentRound = &RoundState{
		Round: s.currentRound.Round + 1,
		Gradients: make(
			map[string]*gradient.GradientSubmission,
		),
	}
}

func (s *Service) CurrentRound() int {
	return s.currentRound.Round
}
