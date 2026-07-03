package aggregator

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"time"

	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
	internalerrors "github.com/Vikaspal8923/Locdist/master/internal/errors"
	"github.com/Vikaspal8923/Locdist/master/metrics"

	"google.golang.org/protobuf/proto"
)

const maxUint32Index = int64(1<<32 - 1)

func (s *Service) Aggregate(
	request *gradient.GradientSubmission,
	expectedWorkers int,
) (*gradient.AggregatedGradientResponse, error) {
	totalStart := time.Now()

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

	lockAcquired := time.Now()
	for s.currentRound.Response != nil ||
		s.currentRound.Err != nil {

		s.cond.Wait()
	}
	previousRoundWaitDone := time.Now()

	round := s.currentRound
	roundNumber := round.Round
	round.WaitingReceivers++

	s.StoreGradient(request)
	storedAt := time.Now()

	aggregateDuration := time.Duration(0)
	aggregatedByWorker := false
	if s.BarrierReached(expectedWorkers) &&
		round.Response == nil &&
		round.Err == nil {

		aggregateStart := time.Now()
		round.Response, round.Err = s.averageRoundLocked(
			request.RuntimeVersion,
			request.JobId,
			expectedWorkers,
		)
		aggregateDuration = time.Since(aggregateStart)
		aggregatedByWorker = true

		s.cond.Broadcast()
	}

	for round.Response == nil && round.Err == nil {
		s.cond.Wait()
	}
	barrierReleased := time.Now()

	response := round.Response
	err := round.Err

	round.WaitingReceivers--

	if round.WaitingReceivers == 0 {
		s.resetRoundLocked()
		s.cond.Broadcast()
	}

	if err != nil {
		s.mutex.Unlock()
		return nil, err
	}

	cloned := proto.Clone(response).(*gradient.AggregatedGradientResponse)
	done := time.Now()
	metricsPath := s.metricsPath
	event := map[string]any{
		"component":                       "master",
		"job_id":                          request.GetJobId(),
		"worker_id":                       request.GetWorkerId(),
		"round":                           roundNumber,
		"expected_workers":                expectedWorkers,
		"received_workers_at_arrival":     len(round.Gradients),
		"aggregated_by_this_worker":       aggregatedByWorker,
		"total_ms":                        ms(done.Sub(totalStart)),
		"lock_wait_ms":                    ms(lockAcquired.Sub(totalStart)),
		"previous_round_wait_ms":          ms(previousRoundWaitDone.Sub(lockAcquired)),
		"store_submission_ms":             ms(storedAt.Sub(previousRoundWaitDone)),
		"barrier_wait_ms":                 ms(barrierReleased.Sub(storedAt) - aggregateDuration),
		"aggregate_ms":                    ms(aggregateDuration),
		"response_clone_ms":               ms(done.Sub(barrierReleased)),
		"bytes_from_worker":               metrics.ProtoBytes(request),
		"bytes_to_worker":                 metrics.ProtoBytes(cloned),
		"aggregation_round_from_response": cloned.GetAggregationRound(),
	}
	s.mutex.Unlock()

	metrics.AppendJSONL(metricsPath, event)

	return cloned, nil
}

func ms(duration time.Duration) float64 {
	return float64(duration.Microseconds()) / 1000.0
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

	reference := firstGradientChunk(chunks)
	if reference == nil {
		reference = chunks[0]
	}

	result := proto.Clone(
		reference,
	).(*gradient.GradientChunk)

	if !reference.HasGrad {
		result.Data = nil
		result.ByteSize = 0
		result.Encoding = "dense"
		result.Indices = nil
		return result, nil
	}

	if usesCompression(chunks) {
		averagedData, indices, err := averageSparseResponse(
			reference.Metadata.Dtype,
			reference.Metadata.Numel,
			chunks,
		)
		if err != nil {
			return nil, err
		}
		result.Data = averagedData
		result.ByteSize = uint64(len(averagedData))
		result.DataDtype = responseDataDtype(reference)
		result.Encoding = "topk"
		packedIndices, err := packUint32Indices(indices)
		if err != nil {
			return nil, err
		}
		result.Indices = nil
		result.IndicesU32 = packedIndices
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
	if reference.GetDataDtype() != "" {
		result.DataDtype = reference.GetDataDtype()
	}
	result.Encoding = "dense"
	result.Indices = nil

	return result, nil
}

func responseDataDtype(chunk *gradient.GradientChunk) string {
	if chunk.GetDataDtype() != "" {
		return chunk.GetDataDtype()
	}
	return chunk.GetMetadata().GetDtype()
}

func firstGradientChunk(chunks []*gradient.GradientChunk) *gradient.GradientChunk {
	for _, chunk := range chunks {
		if chunk.GetHasGrad() {
			return chunk
		}
	}
	return nil
}

func averageData(
	dtype string,
	numel int64,
	chunks []*gradient.GradientChunk,
) ([]byte, error) {
	if usesCompression(chunks) || hasMissingGradient(chunks) || chunks[0].GetDataDtype() != "" {
		return averageCompressedData(dtype, numel, chunks)
	}

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

func usesCompression(chunks []*gradient.GradientChunk) bool {
	for _, chunk := range chunks {
		if chunk.GetEncoding() != "" && chunk.GetEncoding() != "dense" {
			return true
		}
	}
	return false
}

func hasMissingGradient(chunks []*gradient.GradientChunk) bool {
	for _, chunk := range chunks {
		if !chunk.GetHasGrad() {
			return true
		}
	}
	return false
}

func averageCompressedData(
	metadataDtype string,
	numel int64,
	chunks []*gradient.GradientChunk,
) ([]byte, error) {
	outputDtype := chunks[0].GetDataDtype()
	if outputDtype == "" {
		outputDtype = metadataDtype
	}
	sums := make([]float32, int(numel))
	for _, chunk := range chunks {
		values, err := decodeChunkFloat32(metadataDtype, numel, chunk)
		if err != nil {
			return nil, err
		}
		for index, value := range values {
			sums[index] += value
		}
	}
	for index := range sums {
		sums[index] /= float32(len(chunks))
	}
	return encodeFloat32Values(outputDtype, sums)
}

func averageSparseResponse(
	metadataDtype string,
	numel int64,
	chunks []*gradient.GradientChunk,
) ([]byte, []int64, error) {
	reference := firstGradientChunk(chunks)
	if reference == nil {
		return nil, nil, nil
	}
	outputDtype := responseDataDtype(reference)
	sums := make(map[int64]float32)
	for _, chunk := range chunks {
		if !chunk.GetHasGrad() {
			continue
		}
		if encoding := chunk.GetEncoding(); encoding != "topk" {
			return nil, nil, fmt.Errorf("sparse response requires topk chunks, got %q", encoding)
		}
		dataDtype := chunk.GetDataDtype()
		if dataDtype == "" {
			dataDtype = metadataDtype
		}
		elementSize, err := dtypeSize(dataDtype)
		if err != nil {
			return nil, nil, err
		}
		indices, err := chunkIndices(chunk)
		if err != nil {
			return nil, nil, err
		}
		if len(chunk.GetData()) != len(indices)*elementSize || int(chunk.GetByteSize()) != len(indices)*elementSize {
			return nil, nil, fmt.Errorf("sparse gradient data size mismatch")
		}
		seen := make(map[int64]struct{}, len(indices))
		for position, rawIndex := range indices {
			if rawIndex < 0 || rawIndex >= numel {
				return nil, nil, fmt.Errorf("sparse gradient index out of bounds")
			}
			if _, exists := seen[rawIndex]; exists {
				return nil, nil, fmt.Errorf("duplicate sparse gradient index")
			}
			seen[rawIndex] = struct{}{}
			value, err := decodeFloatAt(dataDtype, chunk.GetData(), position)
			if err != nil {
				return nil, nil, err
			}
			sums[rawIndex] += value
		}
	}
	indices := make([]int64, 0, len(sums))
	for index := range sums {
		indices = append(indices, index)
	}
	sort.Slice(indices, func(left, right int) bool { return indices[left] < indices[right] })
	values := make([]float32, len(indices))
	for position, index := range indices {
		values[position] = sums[index] / float32(len(chunks))
	}
	data, err := encodeFloat32Values(outputDtype, values)
	if err != nil {
		return nil, nil, err
	}
	return data, indices, nil
}

func decodeChunkFloat32(
	metadataDtype string,
	numel int64,
	chunk *gradient.GradientChunk,
) ([]float32, error) {
	if !chunk.GetHasGrad() {
		return make([]float32, int(numel)), nil
	}
	dataDtype := chunk.GetDataDtype()
	if dataDtype == "" {
		dataDtype = metadataDtype
	}
	encoding := chunk.GetEncoding()
	if encoding == "" {
		encoding = "dense"
	}
	switch encoding {
	case "dense":
		return decodeDenseFloat32(dataDtype, numel, chunk)
	case "topk":
		return decodeSparseFloat32(dataDtype, numel, chunk)
	default:
		return nil, fmt.Errorf("unsupported gradient encoding %q", encoding)
	}
}

func decodeDenseFloat32(
	dataDtype string,
	numel int64,
	chunk *gradient.GradientChunk,
) ([]float32, error) {
	elementSize, err := dtypeSize(dataDtype)
	if err != nil {
		return nil, err
	}
	if len(chunk.GetData()) != int(numel)*elementSize || int(chunk.GetByteSize()) != int(numel)*elementSize {
		return nil, fmt.Errorf("gradient data size mismatch")
	}
	values := make([]float32, int(numel))
	for index := 0; index < int(numel); index++ {
		value, err := decodeFloatAt(dataDtype, chunk.GetData(), index)
		if err != nil {
			return nil, err
		}
		values[index] = value
	}
	return values, nil
}

func decodeSparseFloat32(
	dataDtype string,
	numel int64,
	chunk *gradient.GradientChunk,
) ([]float32, error) {
	elementSize, err := dtypeSize(dataDtype)
	if err != nil {
		return nil, err
	}
	indices, err := chunkIndices(chunk)
	if err != nil {
		return nil, err
	}
	if len(chunk.GetData()) != len(indices)*elementSize || int(chunk.GetByteSize()) != len(indices)*elementSize {
		return nil, fmt.Errorf("sparse gradient data size mismatch")
	}
	values := make([]float32, int(numel))
	seen := make(map[int64]struct{}, len(indices))
	for position, rawIndex := range indices {
		if rawIndex < 0 || rawIndex >= numel {
			return nil, fmt.Errorf("sparse gradient index out of bounds")
		}
		if _, exists := seen[rawIndex]; exists {
			return nil, fmt.Errorf("duplicate sparse gradient index")
		}
		seen[rawIndex] = struct{}{}
		value, err := decodeFloatAt(dataDtype, chunk.GetData(), position)
		if err != nil {
			return nil, err
		}
		values[int(rawIndex)] = value
	}
	return values, nil
}

func chunkIndices(chunk *gradient.GradientChunk) ([]int64, error) {
	if len(chunk.GetIndicesU32()) > 0 {
		return unpackUint32Indices(chunk.GetIndicesU32())
	}
	return chunk.GetIndices(), nil
}

func unpackUint32Indices(data []byte) ([]int64, error) {
	if len(data)%4 != 0 {
		return nil, fmt.Errorf("packed uint32 indices byte length must be divisible by 4")
	}
	indices := make([]int64, len(data)/4)
	for index := range indices {
		offset := index * 4
		indices[index] = int64(binary.LittleEndian.Uint32(data[offset : offset+4]))
	}
	return indices, nil
}

func packUint32Indices(indices []int64) ([]byte, error) {
	if len(indices) == 0 {
		return nil, nil
	}
	data := make([]byte, len(indices)*4)
	for index, value := range indices {
		if value < 0 || value > maxUint32Index {
			return nil, fmt.Errorf("packed uint32 indices support values up to %d", maxUint32Index)
		}
		binary.LittleEndian.PutUint32(data[index*4:index*4+4], uint32(value))
	}
	return data, nil
}

func dtypeSize(dtype string) (int, error) {
	switch dtype {
	case "torch.float16", "torch.bfloat16":
		return 2, nil
	case "torch.float32":
		return 4, nil
	case "torch.float64":
		return 8, nil
	default:
		return 0, fmt.Errorf("unsupported gradient dtype %q", dtype)
	}
}

func decodeFloatAt(dtype string, data []byte, index int) (float32, error) {
	switch dtype {
	case "torch.float16":
		offset := index * 2
		return float16ToFloat32(binary.LittleEndian.Uint16(data[offset : offset+2])), nil
	case "torch.bfloat16":
		offset := index * 2
		return bfloat16ToFloat32(binary.LittleEndian.Uint16(data[offset : offset+2])), nil
	case "torch.float32":
		offset := index * 4
		return math.Float32frombits(binary.LittleEndian.Uint32(data[offset : offset+4])), nil
	case "torch.float64":
		offset := index * 8
		return float32(math.Float64frombits(binary.LittleEndian.Uint64(data[offset : offset+8]))), nil
	default:
		return 0, fmt.Errorf("unsupported gradient dtype %q", dtype)
	}
}

func encodeFloat32Values(dtype string, values []float32) ([]byte, error) {
	elementSize, err := dtypeSize(dtype)
	if err != nil {
		return nil, err
	}
	result := make([]byte, len(values)*elementSize)
	for index, value := range values {
		switch dtype {
		case "torch.float16":
			binary.LittleEndian.PutUint16(result[index*2:index*2+2], float32ToFloat16(value))
		case "torch.bfloat16":
			binary.LittleEndian.PutUint16(result[index*2:index*2+2], float32ToBFloat16(value))
		case "torch.float32":
			binary.LittleEndian.PutUint32(result[index*4:index*4+4], math.Float32bits(value))
		case "torch.float64":
			binary.LittleEndian.PutUint64(result[index*8:index*8+8], math.Float64bits(float64(value)))
		default:
			return nil, fmt.Errorf("unsupported gradient dtype %q", dtype)
		}
	}
	return result, nil
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
