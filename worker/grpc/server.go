package grpc

import (
	"fmt"
	"net"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	"github.com/Vikaspal8923/Locdist/worker/internal/config"
	"github.com/Vikaspal8923/Locdist/worker/pairing"
	workerresults "github.com/Vikaspal8923/Locdist/worker/results"
	"github.com/Vikaspal8923/Locdist/worker/runtimebridge"
	workersetup "github.com/Vikaspal8923/Locdist/worker/setup"
	"github.com/Vikaspal8923/Locdist/worker/training"
	"github.com/Vikaspal8923/Locdist/worker/workspace"
	grpcserver "google.golang.org/grpc"
)

type Server struct {
	config     config.Config
	grpcServer *grpcserver.Server
	listener   net.Listener
}

func NewServer(
	cfg config.Config,
	runtimeBridge *runtimebridge.Service,
	pairingManager *pairing.Manager,
) (*Server, error) {

	listener, err := net.Listen(
		"tcp",
		fmt.Sprintf(":%s", cfg.Port),
	)
	if err != nil {
		return nil, err
	}

	grpcSrv := grpcserver.NewServer(grpcserver.MaxRecvMsgSize(workspace.MaxRPCBytes))

	workerBridgeServer := NewWorkerBridgeServer(
		runtimeBridge,
		pairingManager,
	)
	workspaceManager := workspace.New(cfg.WorkspaceRoot)
	workerBridgeServer.SetWorkspaceManager(workspaceManager)
	setupManager := workersetup.New(workspaceManager)
	workerBridgeServer.SetSetupManager(setupManager)
	workerBridgeServer.SetTrainingManager(
		training.New(
			workspaceManager,
			setupManager,
			cfg.Port,
			pairingManager,
		),
	)
	workerBridgeServer.SetResultManager(workerresults.New(workspaceManager))

	gradient.RegisterWorkerBridgeServer(
		grpcSrv,
		workerBridgeServer,
	)

	return &Server{
		config:     cfg,
		grpcServer: grpcSrv,
		listener:   listener,
	}, nil
}

func (s *Server) Start() error {
	return s.grpcServer.Serve(s.listener)
}

func (s *Server) Stop() {
	s.grpcServer.GracefulStop()
}
