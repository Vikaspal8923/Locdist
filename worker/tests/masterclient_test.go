package tests

import (
	"net"
	"testing"

	"github.com/Vikaspal8923/Locdist/master/aggregator"
	mastergrpc "github.com/Vikaspal8923/Locdist/master/grpc"

	mastergradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"

	workerconfig "github.com/Vikaspal8923/Locdist/worker/internal/config"
	"github.com/Vikaspal8923/Locdist/worker/masterclient"

	grpcserver "google.golang.org/grpc"
)

func TestMasterClient(
	t *testing.T,
) {

	listener, err := net.Listen(
		"tcp",
		"127.0.0.1:0",
	)
	if err != nil {
		t.Fatalf(
			"failed to create listener: %v",
			err,
		)
	}

	server := grpcserver.NewServer()

	aggregatorService := aggregator.New()

	mastergradient.RegisterWorkerBridgeServer(
		server,
		mastergrpc.NewMasterServer(
			aggregatorService,
		),
	)

	go server.Serve(listener)

	defer server.Stop()

	cfg := workerconfig.Config{
		MasterHost: "127.0.0.1",
		MasterPort: listener.Addr().(*net.TCPAddr).PortString(),
	}

	client, err := masterclient.New(
		cfg,
	)
	if err != nil {
		t.Fatalf(
			"failed to create client: %v",
			err,
		)
	}

	defer client.Close()

	request := &mastergradient.GradientSubmission{
		RuntimeVersion: 1,
		JobId:          "job-123",
		WorkerId:       "worker-a",
		Chunks: []*mastergradient.GradientChunk{
			{},
		},
	}

	response, err := client.Synchronize(
		request,
	)

	if err != nil {
		t.Fatalf(
			"unexpected error: %v",
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
			"unexpected worker count",
		)
	}
}