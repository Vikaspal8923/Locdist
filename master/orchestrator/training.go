package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
	"github.com/Vikaspal8923/Locdist/master/jobs"
	"github.com/Vikaspal8923/Locdist/master/workers"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type TrainingCoordinator struct {
	jobs    *jobs.Manager
	workers *workers.Manager
	timeout time.Duration
}

func NewTrainingCoordinator(jobManager *jobs.Manager, workerManager *workers.Manager) *TrainingCoordinator {
	return &TrainingCoordinator{jobs: jobManager, workers: workerManager, timeout: 30 * time.Second}
}

func (c *TrainingCoordinator) Start(ctx context.Context) error {
	job, err := c.jobs.CurrentJob()
	if err != nil {
		return err
	}
	if job.Status != jobs.StatusPrepared {
		return fmt.Errorf("job must be prepared before training")
	}
	if !c.jobs.AllWorkersReady(job.JobID) {
		return fmt.Errorf("all assigned Workers must be ready")
	}
	for _, assignment := range job.Workers {
		worker, ok := c.workers.Worker(assignment.WorkerID)
		if !ok || worker.Availability != workers.AvailabilityOnline {
			return fmt.Errorf("worker %q is not online", assignment.WorkerID)
		}
		if worker.Status == gradient.WorkerStatus_WORKER_STATUS_RUNNING {
			return fmt.Errorf("worker %q is already training", assignment.WorkerID)
		}
	}
	_ = c.jobs.SetStatus(job.JobID, jobs.StatusStarting)
	armed, failures := c.commandAll(ctx, job, job.Workers, commandArm)
	if len(failures) > 0 {
		c.stopWorkers(context.Background(), job, armed)
		_ = c.jobs.SetStatus(job.JobID, jobs.StatusFailed)
		return errors.Join(failures...)
	}
	_, failures = c.commandAll(ctx, job, job.Workers, commandRelease)
	if len(failures) > 0 {
		c.stopWorkers(context.Background(), job, job.Workers)
		_ = c.jobs.SetStatus(job.JobID, jobs.StatusFailed)
		return errors.Join(failures...)
	}
	_ = c.jobs.SetStatus(job.JobID, jobs.StatusRunning)
	return nil
}

func (c *TrainingCoordinator) Stop(ctx context.Context) error {
	job, err := c.jobs.CurrentJob()
	if err != nil {
		return err
	}
	failures := c.stopWorkers(ctx, job, job.Workers)
	_ = c.jobs.SetStatus(job.JobID, jobs.StatusCancelled)
	return errors.Join(failures...)
}

type commandKind int

const (
	commandArm commandKind = iota
	commandRelease
	commandStop
)

func (c *TrainingCoordinator) commandAll(ctx context.Context, job *jobs.JobState, selected []jobs.WorkerAssignment, kind commandKind) ([]jobs.WorkerAssignment, []error) {
	var wait sync.WaitGroup
	var mu sync.Mutex
	successful := make([]jobs.WorkerAssignment, 0, len(selected))
	failures := make([]error, 0)
	for _, worker := range selected {
		worker := worker
		wait.Add(1)
		go func() {
			defer wait.Done()
			response, err := c.command(ctx, job.JobID, worker, kind)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures = append(failures, fmt.Errorf("worker %q: %w", worker.WorkerID, err))
				return
			}
			_ = c.jobs.UpdateRun(job.JobID, worker.WorkerID, jobs.WorkerRun{Status: response.GetStatus(), ErrorMessage: response.GetErrorMessage(), LogPath: response.GetLogPath()})
			expected := gradient.JobRunStatus_JOB_RUN_STATUS_ARMED
			if kind == commandRelease {
				expected = gradient.JobRunStatus_JOB_RUN_STATUS_RUNNING
			}
			if kind == commandStop {
				expected = gradient.JobRunStatus_JOB_RUN_STATUS_CANCELLED
			}
			if response.GetStatus() != expected {
				failures = append(failures, fmt.Errorf("worker %q: %s", worker.WorkerID, response.GetErrorMessage()))
				return
			}
			successful = append(successful, worker)
		}()
	}
	wait.Wait()
	return successful, failures
}

func (c *TrainingCoordinator) stopWorkers(ctx context.Context, job *jobs.JobState, selected []jobs.WorkerAssignment) []error {
	_, failures := c.commandAll(ctx, job, selected, commandStop)
	return failures
}

func (c *TrainingCoordinator) command(ctx context.Context, jobID string, worker jobs.WorkerAssignment, kind commandKind) (*gradient.JobCommandResponse, error) {
	pairing, ok := c.workers.Pairing(worker.WorkerID)
	if !ok {
		return nil, fmt.Errorf("pairing credentials are missing")
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	connection, err := grpc.DialContext(requestCtx, net.JoinHostPort(worker.Host, worker.GRPCPort), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	client := gradient.NewWorkerBridgeClient(connection)
	request := &gradient.JobCommandRequest{JobId: jobID, WorkerId: worker.WorkerID, MasterId: pairing.MasterID, PairingToken: pairing.Token}
	switch kind {
	case commandArm:
		return client.ArmJob(requestCtx, request)
	case commandRelease:
		return client.ReleaseJob(requestCtx, request)
	default:
		return client.StopJob(requestCtx, request)
	}
}
