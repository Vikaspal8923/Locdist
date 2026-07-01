package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Vikaspal8923/Locdist/master/aggregator"
	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
	"github.com/Vikaspal8923/Locdist/master/jobs"
	"github.com/Vikaspal8923/Locdist/master/workers"
)

type LifecycleCoordinator struct {
	jobs         *jobs.Manager
	workers      *workers.Manager
	training     *TrainingCoordinator
	jobsRoot     string
	pollInterval time.Duration
	aggregator   *aggregator.Service
	results      *ResultCollector
}

func NewLifecycleCoordinator(jobManager *jobs.Manager, workerManager *workers.Manager, jobsRoot string, aggregatorService ...*aggregator.Service) *LifecycleCoordinator {
	resultsRoot := filepath.Join(filepath.Dir(filepath.Clean(jobsRoot)), "ldgcc_results")
	coordinator := &LifecycleCoordinator{jobs: jobManager, workers: workerManager, training: NewTrainingCoordinator(jobManager, workerManager), jobsRoot: jobsRoot, pollInterval: time.Second, results: NewResultCollector(workerManager, resultsRoot)}
	if len(aggregatorService) > 0 {
		coordinator.aggregator = aggregatorService[0]
	}
	return coordinator
}

func (c *LifecycleCoordinator) Monitor(ctx context.Context, timeout time.Duration) (*jobs.Summary, error) {
	job, err := c.jobs.CurrentJob()
	if err != nil {
		return nil, err
	}
	if job.Status != jobs.StatusRunning {
		return nil, fmt.Errorf("job is not running")
	}
	started := time.Now()
	for {
		if timeout > 0 && time.Since(started) >= timeout {
			return c.fail(ctx, job, "training timeout exceeded")
		}
		snapshots, reason, complete := c.inspect(ctx, job)
		if reason != "" {
			return c.finalize(ctx, job, jobs.StatusFailed, reason, snapshots, true)
		}
		if complete {
			return c.finalize(ctx, job, jobs.StatusFinished, "all Workers completed", snapshots, false)
		}
		select {
		case <-ctx.Done():
			return c.finalize(context.Background(), job, jobs.StatusCancelled, ctx.Err().Error(), snapshots, true)
		case <-time.After(c.pollInterval):
		}
	}
}

func (c *LifecycleCoordinator) Cancel(ctx context.Context) (*jobs.Summary, error) {
	job, err := c.jobs.CurrentJob()
	if err != nil {
		return nil, err
	}
	snapshots, _, _ := c.inspect(ctx, job)
	return c.finalize(ctx, job, jobs.StatusCancelled, "cancelled by user", snapshots, true)
}

func (c *LifecycleCoordinator) fail(ctx context.Context, job *jobs.JobState, reason string) (*jobs.Summary, error) {
	snapshots, _, _ := c.inspect(ctx, job)
	return c.finalize(ctx, job, jobs.StatusFailed, reason, snapshots, true)
}

func (c *LifecycleCoordinator) inspect(ctx context.Context, job *jobs.JobState) (map[string]*gradient.JobCommandResponse, string, bool) {
	responses := make(map[string]*gradient.JobCommandResponse, len(job.Workers))
	var mu sync.Mutex
	var wait sync.WaitGroup
	reason := ""
	complete := true
	for _, assignment := range job.Workers {
		assignment := assignment
		wait.Add(1)
		go func() {
			defer wait.Done()
			worker, ok := c.workers.Worker(assignment.WorkerID)
			if !ok || worker.Availability != workers.AvailabilityOnline {
				mu.Lock()
				if reason == "" {
					reason = fmt.Sprintf("worker %q disconnected", assignment.WorkerID)
				}
				mu.Unlock()
				return
			}
			response, err := c.training.command(ctx, job.JobID, assignment, commandStatus)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if reason == "" {
					reason = fmt.Sprintf("worker %q is unreachable: %v", assignment.WorkerID, err)
				}
				return
			}
			responses[assignment.WorkerID] = response
			_ = c.jobs.UpdateRun(job.JobID, assignment.WorkerID, jobs.WorkerRun{Status: response.GetStatus(), ErrorMessage: response.GetErrorMessage(), LogPath: response.GetLogPath()})
			switch response.GetStatus() {
			case gradient.JobRunStatus_JOB_RUN_STATUS_COMPLETED:
			case gradient.JobRunStatus_JOB_RUN_STATUS_FAILED, gradient.JobRunStatus_JOB_RUN_STATUS_CANCELLED, gradient.JobRunStatus_JOB_RUN_STATUS_UNKNOWN:
				if reason == "" {
					reason = fmt.Sprintf("worker %q ended with %s: %s", assignment.WorkerID, response.GetStatus(), response.GetErrorMessage())
				}
			default:
				complete = false
			}
		}()
	}
	wait.Wait()
	if len(responses) != len(job.Workers) {
		complete = false
	}
	return responses, reason, complete
}

func (c *LifecycleCoordinator) finalize(ctx context.Context, job *jobs.JobState, status jobs.Status, reason string, snapshots map[string]*gradient.JobCommandResponse, stop bool) (*jobs.Summary, error) {
	if c.aggregator != nil {
		c.aggregator.AbortJob(reason)
	}
	online := make([]jobs.WorkerAssignment, 0, len(job.Workers))
	for _, assignment := range job.Workers {
		worker, ok := c.workers.Worker(assignment.WorkerID)
		if ok && worker.Availability == workers.AvailabilityOnline {
			online = append(online, assignment)
		}
	}
	if stop {
		_ = c.training.stopWorkers(ctx, job, online)
	}
	if err := c.results.Collect(ctx, job, status == jobs.StatusFinished); err != nil && status == jobs.StatusFinished {
		status = jobs.StatusFailed
		reason = "result collection failed: " + err.Error()
		_ = c.results.Collect(ctx, job, false)
	}
	if status == jobs.StatusFailed && preservesRetryableWorkspace(snapshots) {
		summary := jobs.Summary{JobID: job.JobID, Status: status, Reason: reason, FinishedAt: time.Now(), Workers: finalResultsFromSnapshots(job, snapshots)}
		if err := c.jobs.ReturnToPrepared(job.JobID, summary); err != nil {
			return nil, err
		}
		if err := c.results.WriteSummary(summary); err != nil {
			return &summary, fmt.Errorf("write result summary: %w", err)
		}
		return &summary, nil
	}
	results := make(map[string]jobs.WorkerFinalResult, len(job.Workers))
	var cleanupErrors []error
	for _, assignment := range job.Workers {
		worker, reachable := c.workers.Worker(assignment.WorkerID)
		if !reachable || worker.Availability != workers.AvailabilityOnline {
			results[assignment.WorkerID] = jobs.WorkerFinalResult{Status: gradient.JobRunStatus_JOB_RUN_STATUS_FAILED, ErrorMessage: "Worker disconnected before cleanup", ExitCode: -1}
			continue
		}
		response, err := c.training.command(ctx, job.JobID, assignment, commandCleanup)
		if err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("cleanup worker %q: %w", assignment.WorkerID, err))
			response = snapshots[assignment.WorkerID]
		}
		if response != nil {
			results[assignment.WorkerID] = jobs.WorkerFinalResult{Status: response.GetStatus(), ErrorMessage: response.GetErrorMessage(), ExitCode: response.GetExitCode(), LogTail: string(response.GetLogTail())}
		}
	}
	cleanRoot := filepath.Clean(c.jobsRoot)
	jobRoot := filepath.Join(cleanRoot, job.JobID)
	if cleanRoot == "." || cleanRoot == string(filepath.Separator) || filepath.Base(jobRoot) != job.JobID {
		cleanupErrors = append(cleanupErrors, fmt.Errorf("unsafe Master job path"))
	} else if err := os.RemoveAll(jobRoot); err != nil {
		cleanupErrors = append(cleanupErrors, err)
	}
	summary := jobs.Summary{JobID: job.JobID, Status: status, Reason: reason, FinishedAt: time.Now(), Workers: results}
	if err := c.jobs.ArchiveAndReset(summary); err != nil {
		return nil, err
	}
	if err := c.results.WriteSummary(summary); err != nil {
		cleanupErrors = append(cleanupErrors, fmt.Errorf("write result summary: %w", err))
	}
	if len(cleanupErrors) > 0 {
		return &summary, fmt.Errorf("job finalized with cleanup errors: %v", cleanupErrors)
	}
	return &summary, nil
}

func preservesRetryableWorkspace(snapshots map[string]*gradient.JobCommandResponse) bool {
	for _, response := range snapshots {
		if response.GetStatus() == gradient.JobRunStatus_JOB_RUN_STATUS_FAILED {
			return true
		}
	}
	return false
}

func finalResultsFromSnapshots(job *jobs.JobState, snapshots map[string]*gradient.JobCommandResponse) map[string]jobs.WorkerFinalResult {
	results := make(map[string]jobs.WorkerFinalResult, len(job.Workers))
	for _, assignment := range job.Workers {
		response := snapshots[assignment.WorkerID]
		if response == nil {
			results[assignment.WorkerID] = jobs.WorkerFinalResult{Status: gradient.JobRunStatus_JOB_RUN_STATUS_FAILED, ErrorMessage: "Worker did not return final training status", ExitCode: -1}
			continue
		}
		results[assignment.WorkerID] = jobs.WorkerFinalResult{Status: response.GetStatus(), ErrorMessage: response.GetErrorMessage(), ExitCode: response.GetExitCode(), LogTail: string(response.GetLogTail())}
	}
	return results
}
