package grpc

import (
	"context"
	"fmt"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	"github.com/Vikaspal8923/Locdist/worker/pairing"
	"github.com/Vikaspal8923/Locdist/worker/runtimebridge"
)

type WorkerBridgeServer struct {
	gradient.UnimplementedWorkerBridgeServer

	runtimeBridge *runtimebridge.Service
	pairing       *pairing.Manager
}

func NewWorkerBridgeServer(
	runtimeBridge *runtimebridge.Service,
	pairingManager ...*pairing.Manager,
) *WorkerBridgeServer {
	var manager *pairing.Manager
	if len(pairingManager) > 0 {
		manager = pairingManager[0]
	}
	return &WorkerBridgeServer{
		runtimeBridge: runtimeBridge,
		pairing:       manager,
	}
}

func (s *WorkerBridgeServer) SynchronizeGradients(
	ctx context.Context,
	request *gradient.GradientSubmission,
) (*gradient.AggregatedGradientResponse, error) {

	response, err := s.runtimeBridge.Synchronize(request)
	if err != nil {
		return nil, err
	}

	return response, nil
}

func (s *WorkerBridgeServer) PairWorker(
	ctx context.Context,
	request *gradient.PairWorkerRequest,
) (*gradient.PairWorkerResponse, error) {
	if s.pairing == nil {
		return nil, fmt.Errorf("pairing is not available")
	}
	return s.pairing.Request(ctx, request)
}
