package orchestrator

import (
	"context"
	"fmt"
	"net"
	"time"

	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
	"github.com/Vikaspal8923/Locdist/master/jobs"
	"github.com/Vikaspal8923/Locdist/master/packager"
	"github.com/Vikaspal8923/Locdist/master/workers"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Distributor struct {
	workers *workers.Manager
	timeout time.Duration
}

type WorkspaceResult struct {
	WorkerID      string
	WorkspacePath string
}

func NewDistributor(workerManager *workers.Manager) *Distributor {
	return &Distributor{workers: workerManager, timeout: 30 * time.Second}
}

func (d *Distributor) Distribute(ctx context.Context, job *jobs.JobState) ([]WorkspaceResult, error) {
	if job == nil {
		return nil, fmt.Errorf("job is required")
	}
	shards := make(map[string]string, len(job.Shards))
	for _, shard := range job.Shards {
		shards[shard.WorkerID] = shard.Path
	}
	results := make([]WorkspaceResult, 0, len(job.Workers))
	for _, worker := range job.Workers {
		shardPath, ok := shards[worker.WorkerID]
		if !ok {
			return nil, fmt.Errorf("worker %q has no dataset shard", worker.WorkerID)
		}
		pairing, ok := d.workers.Pairing(worker.WorkerID)
		if !ok {
			return nil, fmt.Errorf("worker %q has no pairing credentials", worker.WorkerID)
		}
		archive, err := packager.Build(packager.PackageRequest{
			ProjectRoot: job.ProjectRoot, JobID: job.JobID, WorkerID: worker.WorkerID,
			Entrypoint: job.Entrypoint, DatasetPath: job.DatasetPath, ShardPath: shardPath,
			Outputs:       job.Outputs,
			Communication: job.Communication,
		})
		if err != nil {
			return nil, fmt.Errorf("package worker %q: %w", worker.WorkerID, err)
		}
		requestCtx, cancel := context.WithTimeout(ctx, d.timeout)
		address := net.JoinHostPort(worker.Host, worker.GRPCPort)
		connection, err := grpc.DialContext(requestCtx, address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(packager.MaxRPCBytes)), grpc.WithBlock())
		if err != nil {
			cancel()
			return nil, fmt.Errorf("connect to worker %q: %w", worker.WorkerID, err)
		}
		response, callErr := gradient.NewWorkerBridgeClient(connection).PrepareWorkspace(requestCtx, &gradient.PrepareWorkspaceRequest{
			JobId: job.JobID, WorkerId: worker.WorkerID, MasterId: pairing.MasterID,
			PairingToken: pairing.Token, Entrypoint: job.Entrypoint, DatasetPath: job.DatasetPath,
			WorkspaceZip: archive,
		})
		connection.Close()
		cancel()
		if callErr != nil {
			return nil, fmt.Errorf("prepare worker %q: %w", worker.WorkerID, callErr)
		}
		if !response.GetPrepared() {
			return nil, fmt.Errorf("worker %q did not prepare its workspace", worker.WorkerID)
		}
		results = append(results, WorkspaceResult{WorkerID: worker.WorkerID, WorkspacePath: response.GetWorkspacePath()})
	}
	return results, nil
}

func (p *Preparer) PrepareAndDistribute(ctx context.Context, projectRoot string) (*jobs.JobState, []WorkspaceResult, error) {
	job, err := p.Prepare(projectRoot)
	if err != nil {
		return nil, nil, err
	}
	results, err := NewDistributor(p.workerManager).Distribute(ctx, job)
	if err != nil {
		return job, results, err
	}
	return job, results, nil
}
