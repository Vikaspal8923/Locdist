package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
	"github.com/Vikaspal8923/Locdist/master/jobs"
	"github.com/Vikaspal8923/Locdist/master/workers"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	maxResultFileBytes   = 64 << 20
	maxWorkerResultBytes = 256 << 20
)

type ResultCollector struct {
	workers *workers.Manager
	root    string
	dial    func(context.Context, string) (*grpc.ClientConn, error)
}

func NewResultCollector(workerManager *workers.Manager, root string) *ResultCollector {
	return &ResultCollector{workers: workerManager, root: root, dial: func(ctx context.Context, address string) (*grpc.ClientConn, error) {
		return grpc.DialContext(ctx, address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	}}
}

func (c *ResultCollector) Collect(ctx context.Context, job *jobs.JobState, strict bool) error {
	if job == nil {
		return fmt.Errorf("job is required")
	}
	if err := safeRoot(c.root); err != nil {
		return err
	}
	if err := os.MkdirAll(c.root, 0o700); err != nil {
		return err
	}
	temporary, err := os.MkdirTemp(c.root, ".result-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	var failures []string
	for _, assignment := range job.Workers {
		if err := c.collectWorker(ctx, job.JobID, assignment, temporary, strict); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", assignment.WorkerID, err))
			if strict {
				return fmt.Errorf("collect results: %s", strings.Join(failures, "; "))
			}
		}
	}
	destination := filepath.Join(c.root, job.JobID)
	if _, err := os.Stat(destination); err == nil {
		return fmt.Errorf("result directory for job already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(temporary, destination); err != nil {
		return err
	}
	return nil
}

func (c *ResultCollector) collectWorker(ctx context.Context, jobID string, assignment jobs.WorkerAssignment, destination string, strict bool) error {
	pairing, ok := c.workers.Pairing(assignment.WorkerID)
	if !ok {
		return fmt.Errorf("pairing credentials are missing")
	}
	worker, ok := c.workers.Worker(assignment.WorkerID)
	if !ok || worker.Availability != workers.AvailabilityOnline {
		return fmt.Errorf("Worker is offline")
	}
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	connection, err := c.dial(requestCtx, net.JoinHostPort(assignment.Host, assignment.GRPCPort))
	if err != nil {
		return err
	}
	defer connection.Close()
	client := gradient.NewWorkerBridgeClient(connection)
	auth := &gradient.JobCommandRequest{JobId: jobID, WorkerId: assignment.WorkerID, MasterId: pairing.MasterID, PairingToken: pairing.Token}
	manifest, err := client.GetResultManifest(requestCtx, auth)
	if err != nil {
		return err
	}
	var missingError error
	if strict && len(manifest.GetMissingOutputs()) > 0 {
		missingError = fmt.Errorf("configured outputs are missing: %s", strings.Join(manifest.GetMissingOutputs(), ", "))
	}
	if strict && len(manifest.GetCollectionErrors()) > 0 {
		missingError = fmt.Errorf("configured outputs are invalid: %s", strings.Join(manifest.GetCollectionErrors(), "; "))
	}
	var total uint64
	for _, file := range manifest.GetFiles() {
		if !safeResultPath(file.GetPath()) {
			return fmt.Errorf("unsafe result path %q", file.GetPath())
		}
		if file.GetSize() > maxResultFileBytes {
			return fmt.Errorf("result %q exceeds file size limit", file.GetPath())
		}
		total += file.GetSize()
		if total > maxWorkerResultBytes {
			return fmt.Errorf("Worker results exceed total size limit")
		}
		if !safeWorkerID(assignment.WorkerID) {
			return fmt.Errorf("unsafe worker_id")
		}
		target := resultTarget(destination, assignment.WorkerID, file)
		if err := downloadFile(requestCtx, client, pairing, jobID, assignment.WorkerID, file, target); err != nil {
			return err
		}
	}
	return missingError
}

func downloadFile(ctx context.Context, client gradient.WorkerBridgeClient, pairing workers.Pairing, jobID, workerID string, expected *gradient.ResultFile, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(target)
		}
	}()
	hash := sha256.New()
	stream, err := client.DownloadResult(ctx, &gradient.DownloadResultRequest{JobId: jobID, WorkerId: workerID, MasterId: pairing.MasterID, PairingToken: pairing.Token, Path: expected.GetPath()})
	if err != nil {
		output.Close()
		return err
	}
	var size uint64
	for {
		chunk, receiveErr := stream.Recv()
		if receiveErr == io.EOF {
			break
		}
		if receiveErr != nil {
			output.Close()
			return receiveErr
		}
		size += uint64(len(chunk.GetData()))
		if size > expected.GetSize() || size > maxResultFileBytes {
			output.Close()
			return fmt.Errorf("result %q exceeded declared size", expected.GetPath())
		}
		if _, err := output.Write(chunk.GetData()); err != nil {
			output.Close()
			return err
		}
		_, _ = hash.Write(chunk.GetData())
	}
	if err := output.Close(); err != nil {
		return err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if size != expected.GetSize() || digest != expected.GetSha256() {
		return fmt.Errorf("result %q failed checksum verification", expected.GetPath())
	}
	complete = true
	return nil
}

func (c *ResultCollector) WriteSummary(summary jobs.Summary) error {
	if err := safeRoot(c.root); err != nil {
		return err
	}
	directory := filepath.Join(c.root, summary.JobID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".summary-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	target := filepath.Join(directory, "summary.json")
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(temporaryPath, target)
}

func resultTarget(root, workerID string, file *gradient.ResultFile) string {
	if file.GetLogFile() {
		return filepath.Join(root, "logs", workerID, filepath.Base(file.GetPath()))
	}
	return filepath.Join(root, "workers", workerID, "outputs", filepath.FromSlash(file.GetPath()))
}

func safeRoot(root string) error {
	clean := filepath.Clean(root)
	if clean == "." || clean == string(filepath.Separator) {
		return fmt.Errorf("results root is unsafe")
	}
	return nil
}

func safeResultPath(value string) bool {
	if value == "" || filepath.IsAbs(value) || strings.Contains(value, "\\") {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(value))
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func safeWorkerID(value string) bool {
	return value != "" && filepath.Base(value) == value && !strings.Contains(value, "\\")
}
