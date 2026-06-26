package aggregator

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"

	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
	internalerrors "github.com/Vikaspal8923/Locdist/master/internal/errors"

	"google.golang.org/protobuf/proto"
)

func (s *Service) Aggregate(
	request *gradient.GradientSubmission,
	expectedWorkers int,
) (*gradient.AggregatedGradientResponse, error) {

	if request.RuntimeVersion == 0 {
		return nil, internalerrors.ErrInvalidRuntimeVersion
	}

	if request.JobId == "" {
		return nil, internalerrors.ErrMissingJobID
	}

	if request.WorkerId == "" {
		return nil, internalerrors.ErrMissingWorkerID
	}

	if len(request.Chunks) == 0 {
		return nil, internalerrors.ErrMissingChunks
	}

	if expectedWorkers <= 0 {
		return nil, fmt.Errorf(
			"expected workers must be greater than zero",
		)
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	for s.currentRound.Response != nil ||
		s.currentRound.Err != nil {

		s.cond.Wait()
	}

	round := s.currentRound
	round.WaitingReceivers++

	s.StoreGradient(request)

	if s.BarrierReached(expectedWorkers) &&
		round.Response == nil &&
		round.Err == nil {

		round.Response, round.Err = s.averageRoundLocked(
			request.RuntimeVersion,
			request.JobId,
			expectedWorkers,
		)

		s.cond.Broadcast()
	}

	for round.Response == nil && round.Err == nil {
		s.cond.Wait()
	}

	response := round.Response
	err := round.Err

	round.WaitingReceivers--

	if round.WaitingReceivers == 0 {
		s.resetRoundLocked()
		s.cond.Broadcast()
	}

	if err != nil {
		return nil, err
	}

	return proto.Clone(response).(*gradient.AggregatedGradientResponse), nil
}

func (s *Service) averageRoundLocked(
	runtimeVersion uint32,
	jobID string,
	expectedWorkers int,
) (*gradient.AggregatedGradientResponse, error) {

	workerIDs := make(
		[]string,
		0,
		len(s.currentRound.Gradients),
	)

	for workerID := range s.currentRound.Gradients {
		workerIDs = append(workerIDs, workerID)
	}

	sort.Strings(workerIDs)

	reference := s.currentRound.Gradients[workerIDs[0]]

	averagedChunks := make(
		[]*gradient.GradientChunk,
		len(reference.Chunks),
	)

	for chunkIndex, referenceChunk := range reference.Chunks {

		chunks := make(
			[]*gradient.GradientChunk,
			0,
			expectedWorkers,
		)

		for _, workerID := range workerIDs {
			submission := s.currentRound.Gradients[workerID]

			if len(submission.Chunks) !=
				len(reference.Chunks) {

				return nil, fmt.Errorf(
					"gradient chunk count mismatch",
				)
			}

			chunk := submission.Chunks[chunkIndex]

			if !sameMetadata(
				referenceChunk.Metadata,
				chunk.Metadata,
			) {
				return nil, fmt.Errorf(
					"gradient metadata mismatch",
				)
			}

			if referenceChunk.HasGrad != chunk.HasGrad {
				return nil, fmt.Errorf(
					"gradient presence mismatch",
				)
			}

			chunks = append(chunks, chunk)
		}

		averagedChunk, err := averageChunks(
			chunks,
		)
		if err != nil {
			return nil, err
		}

		averagedChunks[chunkIndex] = averagedChunk
	}

	return &gradient.AggregatedGradientResponse{
		RuntimeVersion:       runtimeVersion,
		JobId:                jobID,
		ParticipatingWorkers: uint32(expectedWorkers),
		AggregationRound:     uint64(s.currentRound.Round),
		Chunks:               averagedChunks,
	}, nil
}

func sameMetadata(
	left *gradient.ParameterMetadata,
	right *gradient.ParameterMetadata,
) bool {

	if left == nil || right == nil {
		return left == right
	}

	if left.Name != right.Name ||
		left.Numel != right.Numel ||
		left.Dtype != right.Dtype ||
		len(left.Shape) != len(right.Shape) {

		return false
	}

	for index := range left.Shape {
		if left.Shape[index] != right.Shape[index] {
			return false
		}
	}

	return true
}

func averageChunks(
	chunks []*gradient.GradientChunk,
) (*gradient.GradientChunk, error) {

	reference := chunks[0]

	result := proto.Clone(
		reference,
	).(*gradient.GradientChunk)

	if !reference.HasGrad {
		result.Data = nil
		result.ByteSize = 0
		return result, nil
	}

	averagedData, err := averageData(
		reference.Metadata.Dtype,
		reference.Metadata.Numel,
		chunks,
	)
	if err != nil {
		return nil, err
	}

	result.Data = averagedData
	result.ByteSize = uint64(len(averagedData))

	return result, nil
}

func averageData(
	dtype string,
	numel int64,
	chunks []*gradient.GradientChunk,
) ([]byte, error) {

	switch dtype {
	case "torch.float16":
		return averageFloat16(numel, chunks)
	case "torch.float32":
		return averageFloat32(numel, chunks)
	case "torch.float64":
		return averageFloat64(numel, chunks)
	case "torch.bfloat16":
		return averageBFloat16(numel, chunks)
	default:
		return nil, fmt.Errorf(
			"unsupported gradient dtype %q",
			dtype,
		)
	}
}

func averageFloat32(
	numel int64,
	chunks []*gradient.GradientChunk,
) ([]byte, error) {

	if err := validateChunkData(numel, 4, chunks); err != nil {
		return nil, err
	}

	result := make([]byte, int(numel)*4)

	for index := 0; index < int(numel); index++ {
		var sum float64

		offset := index * 4

		for _, chunk := range chunks {
			bits := binary.LittleEndian.Uint32(
				chunk.Data[offset : offset+4],
			)
			sum += float64(math.Float32frombits(bits))
		}

		average := float32(
			sum / float64(len(chunks)),
		)

		binary.LittleEndian.PutUint32(
			result[offset:offset+4],
			math.Float32bits(average),
		)
	}

	return result, nil
}

func averageFloat64(
	numel int64,
	chunks []*gradient.GradientChunk,
) ([]byte, error) {

	if err := validateChunkData(numel, 8, chunks); err != nil {
		return nil, err
	}

	result := make([]byte, int(numel)*8)

	for index := 0; index < int(numel); index++ {
		var sum float64

		offset := index * 8

		for _, chunk := range chunks {
			bits := binary.LittleEndian.Uint64(
				chunk.Data[offset : offset+8],
			)
			sum += math.Float64frombits(bits)
		}

		average := sum / float64(len(chunks))

		binary.LittleEndian.PutUint64(
			result[offset:offset+8],
			math.Float64bits(average),
		)
	}

	return result, nil
}

func averageFloat16(
	numel int64,
	chunks []*gradient.GradientChunk,
) ([]byte, error) {

	if err := validateChunkData(numel, 2, chunks); err != nil {
		return nil, err
	}

	result := make([]byte, int(numel)*2)

	for index := 0; index < int(numel); index++ {
		var sum float64

		offset := index * 2

		for _, chunk := range chunks {
			bits := binary.LittleEndian.Uint16(
				chunk.Data[offset : offset+2],
			)
			sum += float64(float16ToFloat32(bits))
		}

		average := float32(
			sum / float64(len(chunks)),
		)

		binary.LittleEndian.PutUint16(
			result[offset:offset+2],
			float32ToFloat16(average),
		)
	}

	return result, nil
}

func averageBFloat16(
	numel int64,
	chunks []*gradient.GradientChunk,
) ([]byte, error) {

	if err := validateChunkData(numel, 2, chunks); err != nil {
		return nil, err
	}

	result := make([]byte, int(numel)*2)

	for index := 0; index < int(numel); index++ {
		var sum float64

		offset := index * 2

		for _, chunk := range chunks {
			bits := binary.LittleEndian.Uint16(
				chunk.Data[offset : offset+2],
			)
			sum += float64(bfloat16ToFloat32(bits))
		}

		average := float32(
			sum / float64(len(chunks)),
		)

		binary.LittleEndian.PutUint16(
			result[offset:offset+2],
			float32ToBFloat16(average),
		)
	}

	return result, nil
}

func validateChunkData(
	numel int64,
	elementSize int,
	chunks []*gradient.GradientChunk,
) error {

	expectedSize := int(numel) * elementSize

	for _, chunk := range chunks {
		if len(chunk.Data) != expectedSize {
			return fmt.Errorf(
				"gradient data size mismatch",
			)
		}

		if int(chunk.ByteSize) != expectedSize {
			return fmt.Errorf(
				"gradient byte_size mismatch",
			)
		}
	}

	return nil
}

func float16ToFloat32(bits uint16) float32 {

	sign := uint32(bits&0x8000) << 16
	exponent := int((bits >> 10) & 0x1f)
	fraction := uint32(bits & 0x03ff)

	if exponent == 0 {
		if fraction == 0 {
			return math.Float32frombits(sign)
		}

		for fraction&0x0400 == 0 {
			fraction <<= 1
			exponent--
		}

		fraction &= 0x03ff

		return math.Float32frombits(
			sign |
				uint32(exponent+113)<<23 |
				fraction<<13,
		)
	}

	if exponent == 31 {
		return math.Float32frombits(
			sign | 0x7f800000 | fraction<<13,
		)
	}

	return math.Float32frombits(
		sign | uint32(exponent+112)<<23 | fraction<<13,
	)
}

func float32ToFloat16(value float32) uint16 {

	bits := math.Float32bits(value)
	sign := uint16((bits >> 16) & 0x8000)
	exponent := int((bits >> 23) & 0xff)
	fraction := bits & 0x7fffff

	if exponent == 255 {
		if fraction == 0 {
			return sign | 0x7c00
		}
		return sign | 0x7e00
	}

	exponent16 := exponent - 127 + 15

	if exponent16 >= 31 {
		return sign | 0x7c00
	}

	if exponent16 <= 0 {
		if exponent16 < -10 {
			return sign
		}

		fraction |= 0x800000
		shift := uint(14 - exponent16)
		rounded := (fraction + (1 << (shift - 1))) >> shift
		return sign | uint16(rounded)
	}

	rounded := fraction + 0x1000
	if rounded&0x800000 != 0 {
		rounded = 0
		exponent16++
		if exponent16 >= 31 {
			return sign | 0x7c00
		}
	}

	return sign |
		uint16(exponent16<<10) |
		uint16(rounded>>13)
}

func bfloat16ToFloat32(bits uint16) float32 {
	return math.Float32frombits(uint32(bits) << 16)
}

func float32ToBFloat16(value float32) uint16 {

	bits := math.Float32bits(value)
	roundingBias := uint32(0x7fff) + ((bits >> 16) & 1)

	return uint16((bits + roundingBias) >> 16)
}
