package grpc

import (
	"context"

	"github.com/Vikaspal8923/Locdist/master/aggregator"
	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
)

type MasterServer struct {
	gradient.UnimplementedWorkerBridgeServer

	aggregator *aggregator.Service
}

func NewMasterServer(
	aggregatorService *aggregator.Service,
) *MasterServer {

	return &MasterServer{
		aggregator: aggregatorService,
	}
}

func (s *MasterServer) SynchronizeGradients(
	ctx context.Context,
	request *gradient.GradientSubmission,
) (*gradient.AggregatedGradientResponse, error) {

	return s.aggregator.Aggregate(
		request,
	)
}
