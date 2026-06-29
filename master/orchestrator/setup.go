package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
	"github.com/Vikaspal8923/Locdist/master/jobs"
	"github.com/Vikaspal8923/Locdist/master/workers"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type SetupCoordinator struct {
	jobs    *jobs.Manager
	workers *workers.Manager
	timeout time.Duration
}

func NewSetupCoordinator(jobManager *jobs.Manager, workerManager *workers.Manager) *SetupCoordinator {
	return &SetupCoordinator{jobs: jobManager, workers: workerManager, timeout: 15 * time.Minute}
}

func (s *SetupCoordinator) SetupAll(ctx context.Context) error {
	job, err := s.jobs.CurrentJob()
	if err != nil {
		return err
	}
	return s.setupWorkers(ctx, job, job.Workers, false)
}

func (s *SetupCoordinator) RetryFailed(ctx context.Context) error {
	job, err := s.jobs.CurrentJob()
	if err != nil {
		return err
	}
	selected := make([]jobs.WorkerAssignment, 0)
	for _, worker := range job.Workers {
		if job.Setup[worker.WorkerID].Status == gradient.JobSetupStatus_JOB_SETUP_STATUS_FAILED {
			selected = append(selected, worker)
		}
	}
	if len(selected) == 0 {
		return fmt.Errorf("no failed Worker setups to retry")
	}
	return s.setupWorkers(ctx, job, selected, true)
}

func (s *SetupCoordinator) RetryWorker(ctx context.Context, workerID string) error {
	job, err := s.jobs.CurrentJob()
	if err != nil {
		return err
	}
	if job.Setup[workerID].Status != gradient.JobSetupStatus_JOB_SETUP_STATUS_FAILED {
		return fmt.Errorf("worker %q is not setup_failed", workerID)
	}
	for _, worker := range job.Workers {
		if worker.WorkerID == workerID {
			return s.setupWorkers(ctx, job, []jobs.WorkerAssignment{worker}, true)
		}
	}
	return fmt.Errorf("worker %q is not assigned to job", workerID)
}

func (s *SetupCoordinator) AllReady() (bool, error) {
	job, err := s.jobs.CurrentJob()
	if err != nil {
		return false, err
	}
	return s.jobs.AllWorkersReady(job.JobID), nil
}

func (s *SetupCoordinator) setupWorkers(ctx context.Context, job *jobs.JobState, selected []jobs.WorkerAssignment, retry bool) error {
	log.Printf("starting setup for job %q on %d Worker(s)", job.JobID, len(selected))
	var wait sync.WaitGroup
	failureChannel := make(chan error, len(selected))
	for _, worker := range selected {
		worker := worker
		wait.Add(1)
		go func() {
			defer wait.Done()
			log.Printf("marking Worker %q setup as setting_up for job %q", worker.WorkerID, job.JobID)
			_ = s.jobs.UpdateSetup(job.JobID, worker.WorkerID, jobs.WorkerSetup{Status: gradient.JobSetupStatus_JOB_SETUP_STATUS_SETTING_UP})
			response, err := s.setupWorker(ctx, job.JobID, worker, retry)
			if err != nil {
				_ = s.jobs.UpdateSetup(job.JobID, worker.WorkerID, jobs.WorkerSetup{Status: gradient.JobSetupStatus_JOB_SETUP_STATUS_FAILED, ErrorMessage: err.Error()})
				failureChannel <- fmt.Errorf("setup worker %q: %w", worker.WorkerID, err)
				return
			}
			state := jobs.WorkerSetup{Status: response.GetStatus(), ErrorMessage: response.GetErrorMessage(), LogPath: response.GetLogPath()}
			_ = s.jobs.UpdateSetup(job.JobID, worker.WorkerID, state)
			if response.GetStatus() != gradient.JobSetupStatus_JOB_SETUP_STATUS_READY {
				failureChannel <- fmt.Errorf("setup worker %q: %s", worker.WorkerID, response.GetErrorMessage())
			}
		}()
	}
	wait.Wait()
	close(failureChannel)
	var failures []error
	for err := range failureChannel {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func (s *SetupCoordinator) setupWorker(ctx context.Context, jobID string, worker jobs.WorkerAssignment, retry bool) (*gradient.SetupJobResponse, error) {
	pairing, ok := s.workers.Pairing(worker.WorkerID)
	if !ok {
		return nil, fmt.Errorf("pairing credentials are missing")
	}
	requestCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	log.Printf("setting up Worker %q for job %q at %s", worker.WorkerID, jobID, net.JoinHostPort(worker.Host, worker.GRPCPort))
	connection, err := grpc.DialContext(requestCtx, net.JoinHostPort(worker.Host, worker.GRPCPort), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		log.Printf("setup dial failed for Worker %q job %q: %v", worker.WorkerID, jobID, err)
		return nil, err
	}
	defer connection.Close()
	response, err := gradient.NewWorkerBridgeClient(connection).SetupJob(requestCtx, &gradient.SetupJobRequest{
		JobId: jobID, WorkerId: worker.WorkerID, MasterId: pairing.MasterID, PairingToken: pairing.Token, Retry: retry,
	})
	if err != nil {
		log.Printf("setup RPC failed for Worker %q job %q: %v", worker.WorkerID, jobID, err)
		return nil, err
	}
	log.Printf("setup completed for Worker %q job %q with status %s", worker.WorkerID, jobID, response.GetStatus())
	return response, nil
}
