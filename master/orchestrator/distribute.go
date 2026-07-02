package orchestrator

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
	"github.com/Vikaspal8923/Locdist/master/jobs"
	"github.com/Vikaspal8923/Locdist/master/packager"
	"github.com/Vikaspal8923/Locdist/master/workers"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const workspaceUploadChunkBytes = 1 << 20

type Distributor struct {
	workers *workers.Manager
	timeout time.Duration
}

type WorkspaceResult struct {
	WorkerID      string
	WorkspacePath string
}

func NewDistributor(workerManager *workers.Manager) *Distributor {
	return &Distributor{workers: workerManager, timeout: 30 * time.Minute}
}

func (d *Distributor) Distribute(ctx context.Context, job *jobs.JobState) ([]WorkspaceResult, error) {
	if job == nil {
		return nil, fmt.Errorf("job is required")
	}
	shards := make(map[string]jobs.ShardAssignment, len(job.Shards))
	for _, shard := range job.Shards {
		shards[shard.WorkerID] = shard
	}
	results := make([]WorkspaceResult, 0, len(job.Workers))
	for _, worker := range job.Workers {
		_, ok := shards[worker.WorkerID]
		if !ok {
			return nil, fmt.Errorf("worker %q has no dataset shard", worker.WorkerID)
		}
	}

	results = make([]WorkspaceResult, len(job.Workers))
	errors := make(chan error, len(job.Workers))
	var wait sync.WaitGroup
	for index, worker := range job.Workers {
		index := index
		worker := worker
		shard := shards[worker.WorkerID]
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := d.distributeWorker(ctx, job, worker, shard)
			if err != nil {
				errors <- err
				return
			}
			results[index] = result
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}

func (d *Distributor) distributeWorker(ctx context.Context, job *jobs.JobState, worker jobs.WorkerAssignment, shard jobs.ShardAssignment) (WorkspaceResult, error) {
	pairing, ok := d.workers.Pairing(worker.WorkerID)
	if !ok {
		return WorkspaceResult{}, fmt.Errorf("worker %q has no pairing credentials", worker.WorkerID)
	}
	packageFile, err := os.CreateTemp("", "ldgcc-workspace-*.zip")
	if err != nil {
		return WorkspaceResult{}, fmt.Errorf("create workspace package for worker %q: %w", worker.WorkerID, err)
	}
	packagePath := packageFile.Name()
	defer os.Remove(packagePath)
	request := packager.PackageRequest{
		ProjectRoot: job.ProjectRoot, JobID: job.JobID, WorkerID: worker.WorkerID,
		Entrypoint: job.Entrypoint, DatasetPath: job.DatasetPath, ShardPath: shard.Path, ShardKind: shard.Kind,
		Outputs:       job.Outputs,
		Communication: job.Communication,
	}
	if err := packager.Write(packageFile, request); err != nil {
		packageFile.Close()
		return WorkspaceResult{}, fmt.Errorf("package worker %q: %w", worker.WorkerID, err)
	}
	if err := packageFile.Close(); err != nil {
		return WorkspaceResult{}, fmt.Errorf("close package worker %q: %w", worker.WorkerID, err)
	}
	requestCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	address := net.JoinHostPort(worker.Host, worker.GRPCPort)
	connection, err := grpc.DialContext(requestCtx, address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		return WorkspaceResult{}, fmt.Errorf("connect to worker %q: %w", worker.WorkerID, err)
	}
	defer connection.Close()
	response, err := uploadWorkspace(requestCtx, gradient.NewWorkerBridgeClient(connection), packagePath, job, worker, pairing)
	if err != nil {
		return WorkspaceResult{}, fmt.Errorf("prepare worker %q: %w", worker.WorkerID, err)
	}
	if !response.GetPrepared() {
		return WorkspaceResult{}, fmt.Errorf("worker %q did not prepare its workspace", worker.WorkerID)
	}
	return WorkspaceResult{WorkerID: worker.WorkerID, WorkspacePath: response.GetWorkspacePath()}, nil
}

func uploadWorkspace(
	ctx context.Context,
	client gradient.WorkerBridgeClient,
	packagePath string,
	job *jobs.JobState,
	worker jobs.WorkerAssignment,
	pairing workers.Pairing,
) (*gradient.PrepareWorkspaceResponse, error) {
	archive, err := os.Open(packagePath)
	if err != nil {
		return nil, err
	}
	defer archive.Close()
	stream, err := client.UploadWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	buffer := make([]byte, workspaceUploadChunkBytes)
	var offset uint64
	for {
		count, readErr := archive.Read(buffer)
		if count > 0 {
			if err := stream.Send(&gradient.WorkspaceChunk{
				JobId:        job.JobID,
				WorkerId:     worker.WorkerID,
				MasterId:     pairing.MasterID,
				PairingToken: pairing.Token,
				Entrypoint:   job.Entrypoint,
				DatasetPath:  job.DatasetPath,
				Data:         buffer[:count],
				Offset:       offset,
			}); err != nil {
				return nil, err
			}
			offset += uint64(count)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	return stream.CloseAndRecv()
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
