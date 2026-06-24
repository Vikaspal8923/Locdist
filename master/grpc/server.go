package grpc

import (
	"fmt"
	"net"

	"github.com/Vikaspal8923/Locdist/master/aggregator"
	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
	"github.com/Vikaspal8923/Locdist/master/internal/config"

	grpcserver "google.golang.org/grpc"
)

type Server struct {
	config     config.Config
	grpcServer *grpcserver.Server
	listener   net.Listener
}

func NewServer(
	cfg config.Config,
	aggregatorService *aggregator.Service,
) (*Server, error) {

	listener, err := net.Listen(
		"tcp",
		fmt.Sprintf(":%s", cfg.Port),
	)
	if err != nil {
		return nil, err
	}

	grpcSrv := grpcserver.NewServer()

	masterServer := NewMasterServer(
		aggregatorService,
	)

	gradient.RegisterWorkerBridgeServer(
		grpcSrv,
		masterServer,
	)

	return &Server{
		config:     cfg,
		grpcServer: grpcSrv,
		listener:   listener,
	}, nil
}

func (s *Server) Start() error {
	return s.grpcServer.Serve(
		s.listener,
	)
}

func (s *Server) Stop() {
	s.grpcServer.GracefulStop()
}
