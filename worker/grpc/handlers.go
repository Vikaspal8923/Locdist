package grpc

import (
	"context"
	"fmt"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	"github.com/Vikaspal8923/Locdist/worker/pairing"
	"github.com/Vikaspal8923/Locdist/worker/runtimebridge"
	workersetup "github.com/Vikaspal8923/Locdist/worker/setup"
	"github.com/Vikaspal8923/Locdist/worker/training"
	"github.com/Vikaspal8923/Locdist/worker/workspace"
)

type WorkerBridgeServer struct {
	gradient.UnimplementedWorkerBridgeServer

	runtimeBridge *runtimebridge.Service
	pairing       *pairing.Manager
	workspace     *workspace.Manager
	setup         *workersetup.Manager
	training      *training.Manager
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

func (s *WorkerBridgeServer) SetWorkspaceManager(manager *workspace.Manager) {
	s.workspace = manager
}

func (s *WorkerBridgeServer) SetSetupManager(manager *workersetup.Manager) {
	s.setup = manager
}

func (s *WorkerBridgeServer) SetTrainingManager(manager *training.Manager) { s.training = manager }

func (s *WorkerBridgeServer) ArmJob(ctx context.Context, request *gradient.JobCommandRequest) (*gradient.JobCommandResponse, error) {
	if err := s.authenticateCommand(request); err != nil {
		return nil, err
	}
	return commandResponse(request, s.training.Arm(request.GetJobId())), nil
}

func (s *WorkerBridgeServer) ReleaseJob(ctx context.Context, request *gradient.JobCommandRequest) (*gradient.JobCommandResponse, error) {
	if err := s.authenticateCommand(request); err != nil {
		return nil, err
	}
	return commandResponse(request, s.training.Release(request.GetJobId(), request.GetWorkerId())), nil
}

func (s *WorkerBridgeServer) StopJob(ctx context.Context, request *gradient.JobCommandRequest) (*gradient.JobCommandResponse, error) {
	if err := s.authenticateCommand(request); err != nil {
		return nil, err
	}
	return commandResponse(request, s.training.Stop(request.GetJobId())), nil
}

func (s *WorkerBridgeServer) GetJobStatus(ctx context.Context, request *gradient.JobCommandRequest) (*gradient.JobCommandResponse, error) {
	if err := s.authenticateCommand(request); err != nil {
		return nil, err
	}
	return commandResponse(request, s.training.Status(request.GetJobId())), nil
}

func (s *WorkerBridgeServer) CleanupJob(ctx context.Context, request *gradient.JobCommandRequest) (*gradient.JobCommandResponse, error) {
	if err := s.authenticateCommand(request); err != nil {
		return nil, err
	}
	result := s.training.Cleanup(request.GetJobId())
	if s.setup != nil {
		s.setup.Forget(request.GetJobId())
	}
	return commandResponse(request, result), nil
}

func (s *WorkerBridgeServer) authenticateCommand(request *gradient.JobCommandRequest) error {
	if s.pairing == nil || s.training == nil {
		return fmt.Errorf("training lifecycle is not available")
	}
	record, ok := s.pairing.Record()
	if !ok || record.WorkerID != request.GetWorkerId() || record.MasterID != request.GetMasterId() || record.PairingToken != request.GetPairingToken() {
		return fmt.Errorf("Master pairing credentials are invalid")
	}
	return nil
}

func commandResponse(request *gradient.JobCommandRequest, result training.Result) *gradient.JobCommandResponse {
	return &gradient.JobCommandResponse{JobId: request.GetJobId(), WorkerId: request.GetWorkerId(), Status: result.Status, ErrorMessage: result.ErrorMessage, LogPath: result.LogPath, ExitCode: int32(result.ExitCode), LogTail: result.LogTail}
}

func (s *WorkerBridgeServer) SetupJob(ctx context.Context, request *gradient.SetupJobRequest) (*gradient.SetupJobResponse, error) {
	if s.pairing == nil || s.setup == nil {
		return nil, fmt.Errorf("job setup is not available")
	}
	record, ok := s.pairing.Record()
	if !ok || record.WorkerID != request.GetWorkerId() || record.MasterID != request.GetMasterId() || record.PairingToken != request.GetPairingToken() {
		return nil, fmt.Errorf("Master pairing credentials are invalid")
	}
	result := s.setup.Setup(ctx, request.GetJobId(), request.GetRetry())
	return &gradient.SetupJobResponse{
		JobId: request.GetJobId(), WorkerId: request.GetWorkerId(), Status: result.Status,
		ErrorMessage: result.ErrorMessage, LogPath: result.LogPath,
	}, nil
}

func (s *WorkerBridgeServer) PrepareWorkspace(ctx context.Context, request *gradient.PrepareWorkspaceRequest) (*gradient.PrepareWorkspaceResponse, error) {
	if s.pairing == nil || s.workspace == nil {
		return nil, fmt.Errorf("workspace preparation is not available")
	}
	record, ok := s.pairing.Record()
	if !ok || record.WorkerID != request.GetWorkerId() || record.MasterID != request.GetMasterId() || record.PairingToken != request.GetPairingToken() {
		return nil, fmt.Errorf("Master pairing credentials are invalid")
	}
	path, err := s.workspace.Prepare(request.GetJobId(), request.GetEntrypoint(), request.GetDatasetPath(), request.GetWorkspaceZip())
	if err != nil {
		return nil, err
	}
	return &gradient.PrepareWorkspaceResponse{JobId: request.GetJobId(), Prepared: true, WorkspacePath: path}, nil
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
