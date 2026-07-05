package aggregator

import (
	"encoding/binary"
	"math"
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

func TestAggregateSparseTopKReturnsSparseUnionAverage(t *testing.T) {
	service := New()
	chunkA := sparseChunk([]int64{0, 2}, []float32{2, 6})
	chunkB := sparseChunk([]int64{1, 2}, []float32{4, 10})

	done := make(chan *gradient.AggregatedGradientResponse, 2)
	errs := make(chan error, 2)
	go func() {
		response, err := service.Aggregate(submission("worker-a", chunkA), 2)
		if err != nil {
			errs <- err
			return
		}
		done <- response
	}()
	go func() {
		response, err := service.Aggregate(submission("worker-b", chunkB), 2)
		if err != nil {
			errs <- err
			return
		}
		done <- response
	}()

	var response *gradient.AggregatedGradientResponse
	for index := 0; index < 2; index++ {
		select {
		case err := <-errs:
			t.Fatalf("aggregate sparse: %v", err)
		case response = <-done:
		case <-time.After(time.Second):
			t.Fatal("sparse aggregation timed out")
		}
	}

	chunk := response.GetChunks()[0]
	if chunk.GetEncoding() != "topk" {
		t.Fatalf("expected sparse response, got %q", chunk.GetEncoding())
	}
	got, err := chunkIndices(chunk)
	if err != nil {
		t.Fatalf("decode response indices: %v", err)
	}
	if want := []int64{0, 1, 2}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("indices = %v, expected %v", got, want)
	}
	if len(chunk.GetIndices()) != 0 || len(chunk.GetIndicesU32()) != 12 {
		t.Fatalf("expected packed uint32 response indices, repeated=%v bytes=%d", chunk.GetIndices(), len(chunk.GetIndicesU32()))
	}
	values := decodeTestFloat32(chunk.GetData())
	expected := []float32{1, 2, 8}
	for index := range expected {
		if values[index] != expected[index] {
			t.Fatalf("value[%d] = %v, expected %v", index, values[index], expected[index])
		}
	}
}

func TestAggregateSparseTopKTreatsMissingGradientAsZero(t *testing.T) {
	service := New()
	chunkA := sparseChunk([]int64{1}, []float32{8})
	chunkB := missingChunk()

	done := make(chan *gradient.AggregatedGradientResponse, 2)
	errs := make(chan error, 2)
	go func() {
		response, err := service.Aggregate(submission("worker-a", chunkA), 2)
		if err != nil {
			errs <- err
			return
		}
		done <- response
	}()
	go func() {
		response, err := service.Aggregate(submission("worker-b", chunkB), 2)
		if err != nil {
			errs <- err
			return
		}
		done <- response
	}()

	var response *gradient.AggregatedGradientResponse
	for index := 0; index < 2; index++ {
		select {
		case err := <-errs:
			t.Fatalf("aggregate sparse with missing gradient: %v", err)
		case response = <-done:
		case <-time.After(time.Second):
			t.Fatal("sparse aggregation timed out")
		}
	}

	chunk := response.GetChunks()[0]
	indices, err := chunkIndices(chunk)
	if err != nil {
		t.Fatalf("decode response indices: %v", err)
	}
	if len(indices) != 1 || indices[0] != 1 {
		t.Fatalf("indices = %v, expected [1]", indices)
	}
	values := decodeTestFloat32(chunk.GetData())
	if len(values) != 1 || values[0] != 4 {
		t.Fatalf("values = %v, expected [4]", values)
	}
}

func TestAverageDenseTreatsMissingGradientAsZero(t *testing.T) {
	chunkA := denseFloat32Chunk([]float32{2, 6})
	chunkB := &gradient.GradientChunk{
		Metadata: &gradient.ParameterMetadata{
			Name:  "linear.weight",
			Shape: []int64{2},
			Numel: 2,
			Dtype: "torch.float32",
		},
		HasGrad: false,
	}

	chunk, err := averageChunks([]*gradient.GradientChunk{chunkA, chunkB})
	if err != nil {
		t.Fatalf("average dense with missing gradient: %v", err)
	}
	values := decodeTestFloat32(chunk.GetData())
	if values[0] != 1 || values[1] != 3 {
		t.Fatalf("values = %v, expected [1 3]", values)
	}
}

func TestAggregateChunkReturnsAveragedSingleLayer(t *testing.T) {
	service := New()
	chunkA := sparseChunk([]int64{0, 2}, []float32{2, 6})
	chunkA.Metadata.LayerOrder = 3
	chunkA.SyncRound = 5
	chunkB := sparseChunk([]int64{1, 2}, []float32{4, 10})
	chunkB.Metadata.LayerOrder = 3
	chunkB.SyncRound = 5

	done := make(chan *gradient.AggregatedGradientChunkResponse, 2)
	errs := make(chan error, 2)
	go func() {
		response, err := service.AggregateChunk(chunkSubmission("worker-a", chunkA), 2)
		if err != nil {
			errs <- err
			return
		}
		done <- response
	}()
	go func() {
		response, err := service.AggregateChunk(chunkSubmission("worker-b", chunkB), 2)
		if err != nil {
			errs <- err
			return
		}
		done <- response
	}()

	var response *gradient.AggregatedGradientChunkResponse
	for index := 0; index < 2; index++ {
		select {
		case err := <-errs:
			t.Fatalf("aggregate chunk: %v", err)
		case response = <-done:
		case <-time.After(time.Second):
			t.Fatal("chunk aggregation timed out")
		}
	}

	chunk := response.GetChunk()
	if chunk.GetMetadata().GetLayerOrder() != 3 {
		t.Fatalf("expected layer order 3, got %d", chunk.GetMetadata().GetLayerOrder())
	}
	if chunk.GetSyncRound() != 5 {
		t.Fatalf("expected sync round 5, got %d", chunk.GetSyncRound())
	}
	got, err := chunkIndices(chunk)
	if err != nil {
		t.Fatalf("decode response indices: %v", err)
	}
	if want := []int64{0, 1, 2}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("indices = %v, expected %v", got, want)
	}
}

func TestAggregateChunkRejectsDuplicateWorkerSubmission(t *testing.T) {
	service := New()
	chunk := sparseChunk([]int64{0}, []float32{2})
	chunk.Metadata.LayerOrder = 1
	chunk.SyncRound = 3

	done := make(chan error, 1)
	go func() {
		_, err := service.AggregateChunk(chunkSubmission("worker-a", chunk), 2)
		done <- err
	}()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		service.mutex.Lock()
		waiting := 0
		if round, ok := service.chunkRounds[chunkRoundKey(chunkSubmission("worker-a", chunk))]; ok {
			waiting = round.WaitingReceivers
		}
		service.mutex.Unlock()
		if waiting == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	if _, err := service.AggregateChunk(chunkSubmission("worker-a", chunk), 2); err == nil {
		t.Fatal("expected duplicate submission to fail")
	}

	service.AbortJob("duplicate chunk test cleanup")
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("blocked duplicate test aggregation was not released")
	}
}

func TestAggregateChunkRejectsStaleCompletedRound(t *testing.T) {
	service := New()
	chunkA := sparseChunk([]int64{0}, []float32{2})
	chunkA.Metadata.LayerOrder = 2
	chunkA.SyncRound = 7
	chunkB := sparseChunk([]int64{0}, []float32{4})
	chunkB.Metadata.LayerOrder = 2
	chunkB.SyncRound = 7

	done := make(chan error, 2)
	for workerID, chunk := range map[string]*gradient.GradientChunk{
		"worker-a": chunkA,
		"worker-b": chunkB,
	} {
		go func(workerID string, chunk *gradient.GradientChunk) {
			_, err := service.AggregateChunk(chunkSubmission(workerID, chunk), 2)
			done <- err
		}(workerID, chunk)
	}

	for index := 0; index < 2; index++ {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("complete round before stale check: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("stale round setup timed out")
		}
	}

	if _, err := service.AggregateChunk(chunkSubmission("worker-a", chunkA), 2); err == nil {
		t.Fatal("expected stale completed chunk round to fail")
	}
}

func TestAggregateChunkBatchReturnsOrderedBatch(t *testing.T) {
	service := New()

	layer1A := sparseChunk([]int64{0}, []float32{2})
	layer1A.Metadata.LayerOrder = 1
	layer1A.Metadata.Name = "layer-1"
	layer1A.SyncRound = 8
	layer2A := sparseChunk([]int64{1}, []float32{6})
	layer2A.Metadata.LayerOrder = 2
	layer2A.Metadata.Name = "layer-2"
	layer2A.SyncRound = 8

	layer1B := sparseChunk([]int64{0}, []float32{4})
	layer1B.Metadata.LayerOrder = 1
	layer1B.Metadata.Name = "layer-1"
	layer1B.SyncRound = 8
	layer2B := sparseChunk([]int64{1}, []float32{10})
	layer2B.Metadata.LayerOrder = 2
	layer2B.Metadata.Name = "layer-2"
	layer2B.SyncRound = 8

	type batchResult struct {
		workerID string
		response *gradient.AggregatedGradientResponse
	}
	done := make(chan batchResult, 2)
	errs := make(chan error, 2)
	go func() {
		response, err := service.AggregateChunkBatch(&gradient.GradientSubmission{
			RuntimeVersion: 1,
			JobId:          "job-1",
			WorkerId:       "worker-a",
			Chunks:         []*gradient.GradientChunk{layer2A, layer1A},
		}, 2)
		if err != nil {
			errs <- err
			return
		}
		done <- batchResult{workerID: "worker-a", response: response}
	}()
	go func() {
		response, err := service.AggregateChunkBatch(&gradient.GradientSubmission{
			RuntimeVersion: 1,
			JobId:          "job-1",
			WorkerId:       "worker-b",
			Chunks:         []*gradient.GradientChunk{layer1B, layer2B},
		}, 2)
		if err != nil {
			errs <- err
			return
		}
		done <- batchResult{workerID: "worker-b", response: response}
	}()

	for index := 0; index < 2; index++ {
		select {
		case err := <-errs:
			t.Fatalf("aggregate chunk batch: %v", err)
		case result := <-done:
			response := result.response
			if len(response.GetChunks()) != 2 {
				t.Fatalf("expected 2 chunks, got %d", len(response.GetChunks()))
			}
			switch result.workerID {
			case "worker-a":
				if response.GetChunks()[0].GetMetadata().GetLayerOrder() != 2 {
					t.Fatalf("expected worker-a response chunk 0 to be layer 2, got %d", response.GetChunks()[0].GetMetadata().GetLayerOrder())
				}
				if response.GetChunks()[1].GetMetadata().GetLayerOrder() != 1 {
					t.Fatalf("expected worker-a response chunk 1 to be layer 1, got %d", response.GetChunks()[1].GetMetadata().GetLayerOrder())
				}
			case "worker-b":
				if response.GetChunks()[0].GetMetadata().GetLayerOrder() != 1 {
					t.Fatalf("expected worker-b response chunk 0 to be layer 1, got %d", response.GetChunks()[0].GetMetadata().GetLayerOrder())
				}
				if response.GetChunks()[1].GetMetadata().GetLayerOrder() != 2 {
					t.Fatalf("expected worker-b response chunk 1 to be layer 2, got %d", response.GetChunks()[1].GetMetadata().GetLayerOrder())
				}
			default:
				t.Fatalf("unexpected worker id %q", result.workerID)
			}
		case <-time.After(time.Second):
			t.Fatal("chunk batch aggregation timed out")
		}
	}
}

func TestStreamChunkBatchReturnsAllChunks(t *testing.T) {
	service := New()

	layer1A := sparseChunk([]int64{0}, []float32{2})
	layer1A.Metadata.LayerOrder = 1
	layer1A.Metadata.Name = "layer-1"
	layer1A.SyncRound = 9
	layer2A := sparseChunk([]int64{1}, []float32{6})
	layer2A.Metadata.LayerOrder = 2
	layer2A.Metadata.Name = "layer-2"
	layer2A.SyncRound = 9

	layer1B := sparseChunk([]int64{0}, []float32{4})
	layer1B.Metadata.LayerOrder = 1
	layer1B.Metadata.Name = "layer-1"
	layer1B.SyncRound = 9
	layer2B := sparseChunk([]int64{1}, []float32{10})
	layer2B.Metadata.LayerOrder = 2
	layer2B.Metadata.Name = "layer-2"
	layer2B.SyncRound = 9

	type streamResult struct {
		workerID string
		layers   []uint32
	}

	done := make(chan streamResult, 2)
	errs := make(chan error, 2)

	go func() {
		var layers []uint32
		err := service.StreamChunkBatch(&gradient.GradientSubmission{
			RuntimeVersion: 1,
			JobId:          "job-1",
			WorkerId:       "worker-a",
			Chunks:         []*gradient.GradientChunk{layer2A, layer1A},
		}, 2, func(response *gradient.AggregatedGradientChunkResponse) error {
			layers = append(layers, response.GetChunk().GetMetadata().GetLayerOrder())
			return nil
		})
		if err != nil {
			errs <- err
			return
		}
		done <- streamResult{workerID: "worker-a", layers: layers}
	}()

	go func() {
		var layers []uint32
		err := service.StreamChunkBatch(&gradient.GradientSubmission{
			RuntimeVersion: 1,
			JobId:          "job-1",
			WorkerId:       "worker-b",
			Chunks:         []*gradient.GradientChunk{layer1B, layer2B},
		}, 2, func(response *gradient.AggregatedGradientChunkResponse) error {
			layers = append(layers, response.GetChunk().GetMetadata().GetLayerOrder())
			return nil
		})
		if err != nil {
			errs <- err
			return
		}
		done <- streamResult{workerID: "worker-b", layers: layers}
	}()

	for index := 0; index < 2; index++ {
		select {
		case err := <-errs:
			t.Fatalf("stream chunk batch: %v", err)
		case result := <-done:
			if len(result.layers) != 2 {
				t.Fatalf("expected 2 streamed layers for %s, got %d", result.workerID, len(result.layers))
			}
		case <-time.After(time.Second):
			t.Fatal("stream chunk batch timed out")
		}
	}
}

func submission(workerID string, chunk *gradient.GradientChunk) *gradient.GradientSubmission {
	return &gradient.GradientSubmission{RuntimeVersion: 1, JobId: "job-1", WorkerId: workerID, Chunks: []*gradient.GradientChunk{chunk}}
}

func chunkSubmission(workerID string, chunk *gradient.GradientChunk) *gradient.GradientChunkSubmission {
	return &gradient.GradientChunkSubmission{RuntimeVersion: 1, JobId: "job-1", WorkerId: workerID, Chunk: chunk}
}

func sparseChunk(indices []int64, values []float32) *gradient.GradientChunk {
	data := make([]byte, len(values)*4)
	for index, value := range values {
		binary.LittleEndian.PutUint32(data[index*4:index*4+4], math.Float32bits(value))
	}
	return &gradient.GradientChunk{
		Metadata:  &gradient.ParameterMetadata{Name: "p", Shape: []int64{4}, Numel: 4, Dtype: "torch.float32"},
		HasGrad:   true,
		Data:      data,
		ByteSize:  uint64(len(data)),
		DataDtype: "torch.float32",
		Encoding:  "topk",
		Indices:   indices,
	}
}

func denseFloat32Chunk(values []float32) *gradient.GradientChunk {
	data := make([]byte, len(values)*4)
	for index, value := range values {
		binary.LittleEndian.PutUint32(data[index*4:index*4+4], math.Float32bits(value))
	}
	return &gradient.GradientChunk{
		Metadata: &gradient.ParameterMetadata{
			Name:  "linear.weight",
			Shape: []int64{int64(len(values))},
			Numel: int64(len(values)),
			Dtype: "torch.float32",
		},
		HasGrad:  true,
		Data:     data,
		ByteSize: uint64(len(data)),
	}
}

func missingChunk() *gradient.GradientChunk {
	return &gradient.GradientChunk{
		Metadata: &gradient.ParameterMetadata{Name: "p", Shape: []int64{4}, Numel: 4, Dtype: "torch.float32"},
		HasGrad:  false,
	}
}

func decodeTestFloat32(data []byte) []float32 {
	values := make([]float32, len(data)/4)
	for index := range values {
		values[index] = math.Float32frombits(binary.LittleEndian.Uint32(data[index*4 : index*4+4]))
	}
	return values
}
