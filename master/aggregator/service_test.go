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

func submission(workerID string, chunk *gradient.GradientChunk) *gradient.GradientSubmission {
	return &gradient.GradientSubmission{RuntimeVersion: 1, JobId: "job-1", WorkerId: workerID, Chunks: []*gradient.GradientChunk{chunk}}
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
