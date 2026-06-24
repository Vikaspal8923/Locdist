package tests

import (
	"testing"

	"github.com/Vikaspal8923/Locdist/master/aggregator"
	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
)

func TestAggregateSuccess(t *testing.T) {

	service := aggregator.New()

	request := &gradient.GradientSubmission{
		RuntimeVersion: 1,
		JobId:          "job-123",
		WorkerId:       "worker-a",
		Chunks: []*gradient.GradientChunk{
			{},
		},
	}

	response, err := service.Aggregate(
		request,
	)
	if err != nil {
		t.Fatalf(
			"expected no error, got %v",
			err,
		)
	}

	if response.RuntimeVersion != 1 {
		t.Fatalf(
			"unexpected runtime version",
		)
	}

	if response.JobId != "job-123" {
		t.Fatalf(
			"unexpected job id",
		)
	}

	if response.ParticipatingWorkers != 1 {
		t.Fatalf(
			"unexpected participating workers",
		)
	}

	if response.AggregationRound != 1 {
		t.Fatalf(
			"unexpected aggregation round",
		)
	}

	if len(response.Chunks) != 1 {
		t.Fatalf(
			"unexpected chunk count",
		)
	}
}

func TestAggregateInvalidRuntimeVersion(
	t *testing.T,
) {

	service := aggregator.New()

	request := &gradient.GradientSubmission{
		RuntimeVersion: 0,
		JobId:          "job-123",
		WorkerId:       "worker-a",
		Chunks: []*gradient.GradientChunk{
			{},
		},
	}

	_, err := service.Aggregate(
		request,
	)

	if err == nil {
		t.Fatalf(
			"expected error",
		)
	}
}

func TestAggregateMissingJobID(
	t *testing.T,
) {

	service := aggregator.New()

	request := &gradient.GradientSubmission{
		RuntimeVersion: 1,
		WorkerId:       "worker-a",
		Chunks: []*gradient.GradientChunk{
			{},
		},
	}

	_, err := service.Aggregate(
		request,
	)

	if err == nil {
		t.Fatalf(
			"expected error",
		)
	}
}

func TestAggregateMissingWorkerID(
	t *testing.T,
) {

	service := aggregator.New()

	request := &gradient.GradientSubmission{
		RuntimeVersion: 1,
		JobId:          "job-123",
		Chunks: []*gradient.GradientChunk{
			{},
		},
	}

	_, err := service.Aggregate(
		request,
	)

	if err == nil {
		t.Fatalf(
			"expected error",
		)
	}
}

func TestAggregateMissingChunks(
	t *testing.T,
) {

	service := aggregator.New()

	request := &gradient.GradientSubmission{
		RuntimeVersion: 1,
		JobId:          "job-123",
		WorkerId:       "worker-a",
	}

	_, err := service.Aggregate(
		request,
	)

	if err == nil {
		t.Fatalf(
			"expected error",
		)
	}
}
