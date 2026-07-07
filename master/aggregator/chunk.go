package aggregator

import (
	"fmt"

	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
	"google.golang.org/protobuf/proto"
)

func (s *Service) AggregateChunk(
	request *gradient.GradientChunkSubmission,
	expectedWorkers int,
) (*gradient.AggregatedGradientChunkResponse, error) {
	rounds, err := s.registerChunkSubmissions([]*gradient.GradientChunkSubmission{request}, expectedWorkers)
	if err != nil {
		return nil, err
	}
	orderedResponses, err := s.awaitChunkRounds(rounds)
	if err != nil {
		return nil, err
	}

	return orderedResponses[0], nil
}

func (s *Service) AggregateChunkBatch(
	request *gradient.GradientSubmission,
	expectedWorkers int,
) (*gradient.AggregatedGradientResponse, error) {
	if len(request.GetGroups()) > 0 {
		rounds, err := s.registerGroupSubmissions(request, expectedWorkers)
		if err != nil {
			return nil, err
		}
		orderedResponses, err := s.awaitGroupRounds(rounds)
		if err != nil {
			return nil, err
		}

		groups := make([]*gradient.GradientChunkGroup, 0, len(orderedResponses))
		chunks := make([]*gradient.GradientChunk, 0)
		var aggregationRound uint64
		for _, response := range orderedResponses {
			if aggregationRound == 0 {
				aggregationRound = response.GetAggregationRound()
			}
			groups = append(groups, response.GetGroup())
			chunks = append(chunks, response.GetGroup().GetChunks()...)
		}

		return &gradient.AggregatedGradientResponse{
			RuntimeVersion:       request.GetRuntimeVersion(),
			JobId:                request.GetJobId(),
			ParticipatingWorkers: uint32(expectedWorkers),
			AggregationRound:     aggregationRound,
			Chunks:               chunks,
			Groups:               groups,
		}, nil
	}
	if request.GetRuntimeVersion() == 0 {
		return nil, fmt.Errorf("invalid runtime version")
	}
	if request.GetJobId() == "" {
		return nil, fmt.Errorf("missing job id")
	}
	if request.GetWorkerId() == "" {
		return nil, fmt.Errorf("missing worker id")
	}
	if len(request.GetChunks()) == 0 {
		return nil, fmt.Errorf("missing chunks")
	}

	submissions := make([]*gradient.GradientChunkSubmission, 0, len(request.GetChunks()))
	for _, chunk := range request.GetChunks() {
		submissions = append(submissions, &gradient.GradientChunkSubmission{
			RuntimeVersion: request.GetRuntimeVersion(),
			JobId:          request.GetJobId(),
			WorkerId:       request.GetWorkerId(),
			Chunk:          chunk,
		})
	}

	rounds, err := s.registerChunkSubmissions(submissions, expectedWorkers)
	if err != nil {
		return nil, err
	}
	orderedResponses, err := s.awaitChunkRounds(rounds)
	if err != nil {
		return nil, err
	}

	chunks := make([]*gradient.GradientChunk, 0, len(orderedResponses))
	var aggregationRound uint64
	for _, response := range orderedResponses {
		if aggregationRound == 0 {
			aggregationRound = response.GetAggregationRound()
		}
		chunks = append(chunks, response.GetChunk())
	}

	return &gradient.AggregatedGradientResponse{
		RuntimeVersion:       request.GetRuntimeVersion(),
		JobId:                request.GetJobId(),
		ParticipatingWorkers: uint32(expectedWorkers),
		AggregationRound:     aggregationRound,
		Chunks:               chunks,
	}, nil
}

func (s *Service) StreamChunkBatch(
	request *gradient.GradientSubmission,
	expectedWorkers int,
	emit func(*gradient.AggregatedGradientChunkResponse) error,
) error {
	if len(request.GetGroups()) > 0 {
		rounds, err := s.registerGroupSubmissions(request, expectedWorkers)
		if err != nil {
			return err
		}
		return s.streamGroupRounds(rounds, emit)
	}
	if request.GetRuntimeVersion() == 0 {
		return fmt.Errorf("invalid runtime version")
	}
	if request.GetJobId() == "" {
		return fmt.Errorf("missing job id")
	}
	if request.GetWorkerId() == "" {
		return fmt.Errorf("missing worker id")
	}
	if len(request.GetChunks()) == 0 {
		return fmt.Errorf("missing chunks")
	}

	submissions := make([]*gradient.GradientChunkSubmission, 0, len(request.GetChunks()))
	for _, chunk := range request.GetChunks() {
		submissions = append(submissions, &gradient.GradientChunkSubmission{
			RuntimeVersion: request.GetRuntimeVersion(),
			JobId:          request.GetJobId(),
			WorkerId:       request.GetWorkerId(),
			Chunk:          chunk,
		})
	}

	rounds, err := s.registerChunkSubmissions(submissions, expectedWorkers)
	if err != nil {
		return err
	}
	return s.streamChunkRounds(rounds, emit)
}

func averageChunkRoundLocked(
	runtimeVersion uint32,
	jobID string,
	expectedWorkers int,
	round *ChunkRoundState,
) (*gradient.AggregatedGradientChunkResponse, error) {
	referenceRequest, ok := firstChunkSubmission(round.Submissions)
	if !ok {
		return nil, fmt.Errorf("missing chunk submissions")
	}
	referenceChunk := referenceRequest.GetChunk()
	chunks := make([]*gradient.GradientChunk, 0, expectedWorkers)
	for _, submission := range round.Submissions {
		chunk := submission.GetChunk()
		if !sameMetadata(referenceChunk.GetMetadata(), chunk.GetMetadata()) {
			return nil, fmt.Errorf("gradient metadata mismatch")
		}
		chunks = append(chunks, chunk)
	}
	averagedChunk, err := averageChunks(chunks)
	if err != nil {
		return nil, err
	}
	return &gradient.AggregatedGradientChunkResponse{
		RuntimeVersion:       runtimeVersion,
		JobId:                jobID,
		ParticipatingWorkers: uint32(expectedWorkers),
		AggregationRound:     uint64(round.Round),
		Chunk:                averagedChunk,
	}, nil
}

func chunkRoundKey(
	request *gradient.GradientChunkSubmission,
) string {
	metadata := request.GetChunk().GetMetadata()
	return fmt.Sprintf(
		"%s:%d:%d:%s",
		request.GetJobId(),
		request.GetChunk().GetSyncRound(),
		metadata.GetLayerOrder(),
		metadata.GetName(),
	)
}

func chunkLayerKey(
	request *gradient.GradientChunkSubmission,
) string {
	metadata := request.GetChunk().GetMetadata()
	return fmt.Sprintf(
		"%s:%d:%s",
		request.GetJobId(),
		metadata.GetLayerOrder(),
		metadata.GetName(),
	)
}

func firstChunkSubmission(
	submissions map[string]*gradient.GradientChunkSubmission,
) (*gradient.GradientChunkSubmission, bool) {
	for _, submission := range submissions {
		return submission, true
	}
	return nil, false
}

func (s *Service) registerChunkSubmissions(
	requests []*gradient.GradientChunkSubmission,
	expectedWorkers int,
) ([]*ChunkRoundState, error) {
	if expectedWorkers <= 0 {
		return nil, fmt.Errorf("expected workers must be greater than zero")
	}

	s.mutex.Lock()

	rounds := make([]*ChunkRoundState, 0, len(requests))
	seenKeys := make(map[string]struct{}, len(requests))

	for _, request := range requests {
		if err := validateChunkRequest(request, expectedWorkers); err != nil {
			s.mutex.Unlock()
			return nil, err
		}
		key := chunkRoundKey(request)
		if _, exists := seenKeys[key]; exists {
			s.mutex.Unlock()
			return nil, fmt.Errorf("duplicate chunk in batch for %s", key)
		}
		seenKeys[key] = struct{}{}
		layerKey := chunkLayerKey(request)
		if latestRound, ok := s.completedChunks[layerKey]; ok && request.GetChunk().GetSyncRound() <= latestRound {
			s.mutex.Unlock()
			return nil, fmt.Errorf(
				"stale chunk round %d for %s (latest completed %d)",
				request.GetChunk().GetSyncRound(),
				layerKey,
				latestRound,
			)
		}
		round, ok := s.chunkRounds[key]
		if !ok {
			round = &ChunkRoundState{
				Key:         key,
				LayerKey:    layerKey,
				Round:       int(request.GetChunk().GetSyncRound()),
				Submissions: make(map[string]*gradient.GradientChunkSubmission),
			}
			s.chunkRounds[key] = round
		}
		if existing, exists := round.Submissions[request.GetWorkerId()]; exists {
			s.mutex.Unlock()
			if proto.Equal(existing, request) {
				return nil, fmt.Errorf("duplicate chunk submission from worker %q", request.GetWorkerId())
			}
			return nil, fmt.Errorf("conflicting duplicate chunk submission from worker %q", request.GetWorkerId())
		}
		round.WaitingReceivers++
		round.Submissions[request.GetWorkerId()] = request
		rounds = append(rounds, round)
	}

	for _, round := range rounds {
		if len(round.Submissions) == expectedWorkers && round.Response == nil && round.Err == nil {
			response, err := averageChunkRoundLocked(
				requests[0].GetRuntimeVersion(),
				requests[0].GetJobId(),
				expectedWorkers,
				round,
			)
			round.Response = response
			round.Err = err
		}
	}
	s.cond.Broadcast()
	s.mutex.Unlock()
	return rounds, nil
}

func (s *Service) awaitChunkRounds(
	rounds []*ChunkRoundState,
) ([]*gradient.AggregatedGradientChunkResponse, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	for _, round := range rounds {
		for round.Response == nil && round.Err == nil {
			s.cond.Wait()
		}
	}

	ordered := make([]*gradient.AggregatedGradientChunkResponse, 0, len(rounds))
	for _, round := range rounds {
		if round.Err != nil {
			for _, pendingRound := range rounds {
				pendingRound.WaitingReceivers--
				if pendingRound.WaitingReceivers == 0 && pendingRound.Response != nil {
					s.completedChunks[pendingRound.LayerKey] = uint64(pendingRound.Round)
					delete(s.chunkRounds, pendingRound.Key)
				}
			}
			return nil, round.Err
		}
		ordered = append(ordered, proto.Clone(round.Response).(*gradient.AggregatedGradientChunkResponse))
	}

	for _, round := range rounds {
		round.WaitingReceivers--
		if round.WaitingReceivers == 0 {
			s.completedChunks[round.LayerKey] = uint64(round.Round)
			delete(s.chunkRounds, round.Key)
		}
	}

	return ordered, nil
}

func (s *Service) streamChunkRounds(
	rounds []*ChunkRoundState,
	emit func(*gradient.AggregatedGradientChunkResponse) error,
) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	remaining := make(map[*ChunkRoundState]struct{}, len(rounds))
	for _, round := range rounds {
		remaining[round] = struct{}{}
	}

	for len(remaining) > 0 {
		progressed := false
		for round := range remaining {
			if round.Err != nil {
				s.releasePendingChunkRoundsLocked(remaining)
				return round.Err
			}
			if round.Response == nil {
				continue
			}

			response := proto.Clone(round.Response).(*gradient.AggregatedGradientChunkResponse)
			round.WaitingReceivers--
			if round.WaitingReceivers == 0 {
				s.completedChunks[round.LayerKey] = uint64(round.Round)
				delete(s.chunkRounds, round.Key)
			}
			delete(remaining, round)
			progressed = true

			s.mutex.Unlock()
			err := emit(response)
			s.mutex.Lock()
			if err != nil {
				s.releasePendingChunkRoundsLocked(remaining)
				return err
			}
		}

		if len(remaining) == 0 {
			return nil
		}
		if !progressed {
			s.cond.Wait()
		}
	}

	return nil
}

func (s *Service) releasePendingChunkRoundsLocked(
	rounds map[*ChunkRoundState]struct{},
) {
	for round := range rounds {
		round.WaitingReceivers--
		if round.WaitingReceivers == 0 && round.Response != nil {
			s.completedChunks[round.LayerKey] = uint64(round.Round)
			delete(s.chunkRounds, round.Key)
		}
	}
}

func groupRoundKey(
	request *gradient.GradientSubmission,
	group *gradient.GradientChunkGroup,
) string {
	return fmt.Sprintf(
		"%s:%d:%d",
		request.GetJobId(),
		group.GetSyncRound(),
		group.GetGroupId(),
	)
}

func groupIdentityKey(
	request *gradient.GradientSubmission,
	group *gradient.GradientChunkGroup,
) string {
	return fmt.Sprintf(
		"%s:%d",
		request.GetJobId(),
		group.GetGroupId(),
	)
}

func (s *Service) registerGroupSubmissions(
	request *gradient.GradientSubmission,
	expectedWorkers int,
) ([]*GroupRoundState, error) {
	if expectedWorkers <= 0 {
		return nil, fmt.Errorf("expected workers must be greater than zero")
	}
	if request.GetRuntimeVersion() == 0 {
		return nil, fmt.Errorf("invalid runtime version")
	}
	if request.GetJobId() == "" {
		return nil, fmt.Errorf("missing job id")
	}
	if request.GetWorkerId() == "" {
		return nil, fmt.Errorf("missing worker id")
	}
	if len(request.GetGroups()) == 0 {
		return nil, fmt.Errorf("missing groups")
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	rounds := make([]*GroupRoundState, 0, len(request.GetGroups()))
	seenKeys := make(map[string]struct{}, len(request.GetGroups()))

	for _, group := range request.GetGroups() {
		if group.GetGroupId() == 0 && len(group.GetChunks()) == 0 {
			return nil, fmt.Errorf("invalid empty group")
		}
		if group.GetSyncRound() == 0 {
			return nil, fmt.Errorf("missing group sync round")
		}
		if len(group.GetChunks()) == 0 {
			return nil, fmt.Errorf("missing group chunks")
		}
		key := groupRoundKey(request, group)
		if _, exists := seenKeys[key]; exists {
			return nil, fmt.Errorf("duplicate group in batch for %s", key)
		}
		seenKeys[key] = struct{}{}
		groupKey := groupIdentityKey(request, group)
		if latestRound, ok := s.completedGroups[groupKey]; ok && group.GetSyncRound() <= latestRound {
			return nil, fmt.Errorf(
				"stale group round %d for %s (latest completed %d)",
				group.GetSyncRound(),
				groupKey,
				latestRound,
			)
		}
		round, ok := s.groupRounds[key]
		if !ok {
			round = &GroupRoundState{
				Key:         key,
				GroupKey:    groupKey,
				Round:       int(group.GetSyncRound()),
				Submissions: make(map[string]*gradient.GradientChunkGroup),
			}
			s.groupRounds[key] = round
		}
		if existing, exists := round.Submissions[request.GetWorkerId()]; exists {
			if proto.Equal(existing, group) {
				return nil, fmt.Errorf("duplicate group submission from worker %q", request.GetWorkerId())
			}
			return nil, fmt.Errorf("conflicting duplicate group submission from worker %q", request.GetWorkerId())
		}
		round.WaitingReceivers++
		round.Submissions[request.GetWorkerId()] = group
		rounds = append(rounds, round)
	}

	for _, round := range rounds {
		if len(round.Submissions) == expectedWorkers && round.Response == nil && round.Err == nil {
			response, err := averageGroupRoundLocked(
				request.GetRuntimeVersion(),
				request.GetJobId(),
				expectedWorkers,
				round,
			)
			round.Response = response
			round.Err = err
		}
	}
	s.cond.Broadcast()
	return rounds, nil
}

func averageGroupRoundLocked(
	runtimeVersion uint32,
	jobID string,
	expectedWorkers int,
	round *GroupRoundState,
) (*gradient.AggregatedGradientChunkResponse, error) {
	var reference *gradient.GradientChunkGroup
	for _, submission := range round.Submissions {
		reference = submission
		break
	}
	if reference == nil {
		return nil, fmt.Errorf("missing group submissions")
	}
	referenceChunks := reference.GetChunks()
	if len(referenceChunks) == 0 {
		return nil, fmt.Errorf("missing group chunks")
	}

	aggregatedChunks := make([]*gradient.GradientChunk, 0, len(referenceChunks))
	for memberIndex, referenceChunk := range referenceChunks {
		memberChunks := make([]*gradient.GradientChunk, 0, expectedWorkers)
		for _, submission := range round.Submissions {
			chunks := submission.GetChunks()
			if len(chunks) != len(referenceChunks) {
				return nil, fmt.Errorf("group member count mismatch")
			}
			candidate := chunks[memberIndex]
			if !sameMetadata(referenceChunk.GetMetadata(), candidate.GetMetadata()) {
				return nil, fmt.Errorf("group member metadata mismatch")
			}
			memberChunks = append(memberChunks, candidate)
		}
		averagedChunk, err := averageChunks(memberChunks)
		if err != nil {
			return nil, err
		}
		aggregatedChunks = append(aggregatedChunks, averagedChunk)
	}

	return &gradient.AggregatedGradientChunkResponse{
		RuntimeVersion:       runtimeVersion,
		JobId:                jobID,
		ParticipatingWorkers: uint32(expectedWorkers),
		AggregationRound:     uint64(round.Round),
		Group: &gradient.GradientChunkGroup{
			GroupId:   reference.GetGroupId(),
			SyncRound: uint64(round.Round),
			Chunks:    aggregatedChunks,
			ByteSize:  reference.GetByteSize(),
		},
	}, nil
}

func (s *Service) awaitGroupRounds(
	rounds []*GroupRoundState,
) ([]*gradient.AggregatedGradientChunkResponse, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	for _, round := range rounds {
		for round.Response == nil && round.Err == nil {
			s.cond.Wait()
		}
	}

	ordered := make([]*gradient.AggregatedGradientChunkResponse, 0, len(rounds))
	for _, round := range rounds {
		if round.Err != nil {
			for _, pendingRound := range rounds {
				pendingRound.WaitingReceivers--
				if pendingRound.WaitingReceivers == 0 && pendingRound.Response != nil {
					s.completedGroups[pendingRound.GroupKey] = uint64(pendingRound.Round)
					delete(s.groupRounds, pendingRound.Key)
				}
			}
			return nil, round.Err
		}
		ordered = append(ordered, proto.Clone(round.Response).(*gradient.AggregatedGradientChunkResponse))
	}

	for _, round := range rounds {
		round.WaitingReceivers--
		if round.WaitingReceivers == 0 {
			s.completedGroups[round.GroupKey] = uint64(round.Round)
			delete(s.groupRounds, round.Key)
		}
	}

	return ordered, nil
}

func (s *Service) streamGroupRounds(
	rounds []*GroupRoundState,
	emit func(*gradient.AggregatedGradientChunkResponse) error,
) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	remaining := make(map[*GroupRoundState]struct{}, len(rounds))
	for _, round := range rounds {
		remaining[round] = struct{}{}
	}

	for len(remaining) > 0 {
		progressed := false
		for round := range remaining {
			if round.Err != nil {
				s.releasePendingGroupRoundsLocked(remaining)
				return round.Err
			}
			if round.Response == nil {
				continue
			}

			response := proto.Clone(round.Response).(*gradient.AggregatedGradientChunkResponse)
			round.WaitingReceivers--
			if round.WaitingReceivers == 0 {
				s.completedGroups[round.GroupKey] = uint64(round.Round)
				delete(s.groupRounds, round.Key)
			}
			delete(remaining, round)
			progressed = true

			s.mutex.Unlock()
			err := emit(response)
			s.mutex.Lock()
			if err != nil {
				s.releasePendingGroupRoundsLocked(remaining)
				return err
			}
		}

		if len(remaining) == 0 {
			return nil
		}
		if !progressed {
			s.cond.Wait()
		}
	}

	return nil
}

func (s *Service) releasePendingGroupRoundsLocked(
	rounds map[*GroupRoundState]struct{},
) {
	for round := range rounds {
		round.WaitingReceivers--
		if round.WaitingReceivers == 0 && round.Response != nil {
			s.completedGroups[round.GroupKey] = uint64(round.Round)
			delete(s.groupRounds, round.Key)
		}
	}
}

func validateChunkRequest(
	request *gradient.GradientChunkSubmission,
	expectedWorkers int,
) error {
	if request.GetRuntimeVersion() == 0 {
		return fmt.Errorf("invalid runtime version")
	}
	if request.GetJobId() == "" {
		return fmt.Errorf("missing job id")
	}
	if request.GetWorkerId() == "" {
		return fmt.Errorf("missing worker id")
	}
	if request.GetChunk() == nil {
		return fmt.Errorf("missing chunk")
	}
	if request.GetChunk().GetMetadata() == nil {
		return fmt.Errorf("missing chunk metadata")
	}
	if request.GetChunk().GetSyncRound() == 0 {
		return fmt.Errorf("missing chunk sync round")
	}
	if request.GetChunk().GetMetadata().GetName() == "" {
		return fmt.Errorf("missing chunk parameter name")
	}
	if expectedWorkers <= 0 {
		return fmt.Errorf("expected workers must be greater than zero")
	}
	return nil
}
