package tests

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/Vikaspal8923/Locdist/master/aggregator"
	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
)

func TestAggregateSingleWorker(t *testing.T) {

	service := aggregator.New()

	response, err := service.Aggregate(
		gradientSubmission("worker-a", []float32{1, 3}),
		1,
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	assertResponse(
		t,
		response,
		1,
		1,
		[]float32{1, 3},
	)
}

func TestAggregateBlocksUntilAllWorkersSubmit(t *testing.T) {

	service := aggregator.New()
	firstResponse := make(
		chan *gradient.AggregatedGradientResponse,
		1,
	)
	firstErr := make(chan error, 1)

	go func() {
		response, err := service.Aggregate(
			gradientSubmission(
				"worker-a",
				[]float32{1, 3},
			),
			2,
		)
		firstResponse <- response
		firstErr <- err
	}()

	select {
	case <-firstResponse:
		t.Fatalf("worker returned before barrier was satisfied")
	case <-time.After(50 * time.Millisecond):
	}

	secondResponse, err := service.Aggregate(
		gradientSubmission(
			"worker-b",
			[]float32{3, 5},
		),
		2,
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if err := <-firstErr; err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	response := <-firstResponse

	assertResponse(
		t,
		response,
		2,
		1,
		[]float32{2, 4},
	)
	assertResponse(
		t,
		secondResponse,
		2,
		1,
		[]float32{2, 4},
	)

	if !bytes.Equal(
		response.Chunks[0].Data,
		secondResponse.Chunks[0].Data,
	) {
		t.Fatalf("workers received different aggregated gradients")
	}
}

func TestAggregateDuplicateWorkerReplacesSubmission(t *testing.T) {

	service := aggregator.New()
	responses := make(
		chan *gradient.AggregatedGradientResponse,
		3,
	)
	errs := make(chan error, 3)

	submit := func(workerID string, values []float32) {
		response, err := service.Aggregate(
			gradientSubmission(workerID, values),
			2,
		)
		responses <- response
		errs <- err
	}

	go submit("worker-a", []float32{1, 1})
	time.Sleep(20 * time.Millisecond)

	go submit("worker-a", []float32{5, 5})
	time.Sleep(20 * time.Millisecond)

	go submit("worker-b", []float32{3, 7})

	for index := 0; index < 3; index++ {
		select {
		case err := <-errs:
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for duplicate submission")
		}
	}

	for index := 0; index < 3; index++ {
		response := <-responses
		assertResponse(
			t,
			response,
			2,
			1,
			[]float32{4, 6},
		)
	}
}

func TestAggregateAdvancesRounds(t *testing.T) {

	service := aggregator.New()

	first, err := service.Aggregate(
		gradientSubmission("worker-a", []float32{1}),
		1,
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	second, err := service.Aggregate(
		gradientSubmission("worker-a", []float32{2}),
		1,
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	assertResponse(t, first, 1, 1, []float32{1})
	assertResponse(t, second, 1, 2, []float32{2})
}

func TestAggregateInvalidRuntimeVersion(t *testing.T) {

	service := aggregator.New()

	request := gradientSubmission("worker-a", []float32{1})
	request.RuntimeVersion = 0

	_, err := service.Aggregate(request, 1)

	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestAggregateMissingJobID(t *testing.T) {

	service := aggregator.New()

	request := gradientSubmission("worker-a", []float32{1})
	request.JobId = ""

	_, err := service.Aggregate(request, 1)

	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestAggregateMissingWorkerID(t *testing.T) {

	service := aggregator.New()

	request := gradientSubmission("worker-a", []float32{1})
	request.WorkerId = ""

	_, err := service.Aggregate(request, 1)

	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestAggregateMissingChunks(t *testing.T) {

	service := aggregator.New()

	request := gradientSubmission("worker-a", []float32{1})
	request.Chunks = nil

	_, err := service.Aggregate(request, 1)

	if err == nil {
		t.Fatalf("expected error")
	}
}

func gradientSubmission(
	workerID string,
	values []float32,
) *gradient.GradientSubmission {

	return &gradient.GradientSubmission{
		RuntimeVersion: 1,
		JobId:          "job-123",
		WorkerId:       workerID,
		Chunks: []*gradient.GradientChunk{
			float32Chunk(values),
		},
	}
}

func float32Chunk(
	values []float32,
) *gradient.GradientChunk {

	data := make([]byte, len(values)*4)

	for index, value := range values {
		binary.LittleEndian.PutUint32(
			data[index*4:index*4+4],
			math.Float32bits(value),
		)
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

func assertResponse(
	t *testing.T,
	response *gradient.AggregatedGradientResponse,
	participatingWorkers uint32,
	aggregationRound uint64,
	values []float32,
) {

	t.Helper()

	if response.RuntimeVersion != 1 {
		t.Fatalf("unexpected runtime version")
	}

	if response.JobId != "job-123" {
		t.Fatalf("unexpected job id")
	}

	if response.ParticipatingWorkers != participatingWorkers {
		t.Fatalf("unexpected participating workers")
	}

	if response.AggregationRound != aggregationRound {
		t.Fatalf("unexpected aggregation round")
	}

	if len(response.Chunks) != 1 {
		t.Fatalf("unexpected chunk count")
	}

	actual := float32Values(response.Chunks[0].Data)

	for index, expected := range values {
		if actual[index] != expected {
			t.Fatalf(
				"unexpected averaged value at %d: got %v want %v",
				index,
				actual[index],
				expected,
			)
		}
	}
}

func float32Values(data []byte) []float32 {

	values := make([]float32, len(data)/4)

	for index := range values {
		bits := binary.LittleEndian.Uint32(
			data[index*4 : index*4+4],
		)
		values[index] = math.Float32frombits(bits)
	}

	return values
}
