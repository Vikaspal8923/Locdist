package grpc

import (
	"context"

	"github.com/Vikaspal8923/Locdist/master/coordinator"
	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
)

type MasterServer struct {
	gradient.UnimplementedWorkerBridgeServer

	coordinator *coordinator.Coordinator
}

func NewMasterServer(
	coordinatorService *coordinator.Coordinator,
) *MasterServer {

	return &MasterServer{
		coordinator: coordinatorService,
	}
}

func (s *MasterServer) SynchronizeGradients(
	ctx context.Context,
	request *gradient.GradientSubmission,
) (*gradient.AggregatedGradientResponse, error) {

	return s.coordinator.SynchronizeGradients(
		request,
	)
}

func (s *MasterServer) RegisterWorker(
	ctx context.Context,
	request *gradient.RegisterWorkerRequest,
) (*gradient.RegisterWorkerResponse, error) {

	return s.coordinator.RegisterWorker(request)
}

func (s *MasterServer) UpdateWorkerStatus(
	ctx context.Context,
	request *gradient.WorkerStatusUpdate,
) (*gradient.WorkerStatusResponse, error) {

	return s.coordinator.UpdateWorkerStatus(request)
}

func (s *MasterServer) UnpairWorker(
	ctx context.Context,
	request *gradient.UnpairWorkerRequest,
) (*gradient.UnpairWorkerResponse, error) {
	return s.coordinator.UnpairWorker(request)
}
