package grpc

import (
	"fmt"
	"net"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	"github.com/Vikaspal8923/Locdist/worker/internal/config"
	"github.com/Vikaspal8923/Locdist/worker/runtimebridge"
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
) (*Server, error) {

	listener, err := net.Listen(
		"tcp",
		fmt.Sprintf(":%s", cfg.Port),
	)
	if err != nil {
		return nil, err
	}

	grpcSrv := grpcserver.NewServer()

	workerBridgeServer := NewWorkerBridgeServer(
		runtimeBridge,
	)

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
