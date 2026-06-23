package grpc

import (
	"context"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	"github.com/Vikaspal8923/Locdist/worker/runtimebridge"
)

type WorkerBridgeServer struct {
	gradient.UnimplementedWorkerBridgeServer

	runtimeBridge *runtimebridge.Service
}

func NewWorkerBridgeServer(
	runtimeBridge *runtimebridge.Service,
) *WorkerBridgeServer {
	return &WorkerBridgeServer{
		runtimeBridge: runtimeBridge,
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
