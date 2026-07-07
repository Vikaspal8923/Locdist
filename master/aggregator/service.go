package aggregator

import (
	"fmt"
	"sync"

	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
)

type Service struct {
	currentRound    *RoundState
	chunkRounds     map[string]*ChunkRoundState
	completedChunks map[string]uint64
	groupRounds     map[string]*GroupRoundState
	completedGroups map[string]uint64
	resetJobPending bool

	mutex sync.Mutex
	cond  *sync.Cond

	metricsPath string
}

func New() *Service {

	service := &Service{
		currentRound: &RoundState{
			Round: 1,
			Gradients: make(
				map[string]*gradient.GradientSubmission,
			),
		},
		chunkRounds:     make(map[string]*ChunkRoundState),
		completedChunks: make(map[string]uint64),
		groupRounds:     make(map[string]*GroupRoundState),
		completedGroups: make(map[string]uint64),
	}

	service.cond = sync.NewCond(&service.mutex)

	return service
}

func (s *Service) SetMetricsPath(path string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.metricsPath = path
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
	s.chunkRounds = make(map[string]*ChunkRoundState)
	s.completedChunks = make(map[string]uint64)
	s.groupRounds = make(map[string]*GroupRoundState)
	s.completedGroups = make(map[string]uint64)
}

func (s *Service) AbortJob(reason string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	chunkWaiters := false
	for _, round := range s.chunkRounds {
		if round.WaitingReceivers > 0 {
			chunkWaiters = true
			break
		}
	}
	groupWaiters := false
	for _, round := range s.groupRounds {
		if round.WaitingReceivers > 0 {
			groupWaiters = true
			break
		}
	}
	if s.currentRound.WaitingReceivers == 0 && !chunkWaiters && !groupWaiters {
		s.currentRound = &RoundState{Round: 1, Gradients: make(map[string]*gradient.GradientSubmission)}
		s.chunkRounds = make(map[string]*ChunkRoundState)
		s.completedChunks = make(map[string]uint64)
		s.groupRounds = make(map[string]*GroupRoundState)
		s.completedGroups = make(map[string]uint64)
		s.resetJobPending = false
		return
	}
	s.resetJobPending = true
	s.currentRound.Err = fmt.Errorf("job aborted: %s", reason)
	for _, round := range s.chunkRounds {
		round.Err = fmt.Errorf("job aborted: %s", reason)
	}
	for _, round := range s.groupRounds {
		round.Err = fmt.Errorf("job aborted: %s", reason)
	}
	s.cond.Broadcast()
}

func (s *Service) CurrentRound() int {
	return s.currentRound.Round
}
