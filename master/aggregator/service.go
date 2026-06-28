package aggregator

import (
	"fmt"
	"sync"

	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
)

type Service struct {
	currentRound    *RoundState
	resetJobPending bool

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
	nextRound := s.currentRound.Round + 1
	if s.resetJobPending {
		nextRound = 1
		s.resetJobPending = false
	}

	s.currentRound = &RoundState{
		Round: nextRound,
		Gradients: make(
			map[string]*gradient.GradientSubmission,
		),
	}
}

func (s *Service) AbortJob(reason string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.currentRound.WaitingReceivers == 0 {
		s.currentRound = &RoundState{Round: 1, Gradients: make(map[string]*gradient.GradientSubmission)}
		s.resetJobPending = false
		return
	}
	s.resetJobPending = true
	s.currentRound.Err = fmt.Errorf("job aborted: %s", reason)
	s.cond.Broadcast()
}

func (s *Service) CurrentRound() int {
	return s.currentRound.Round
}
