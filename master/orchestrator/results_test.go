package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"testing"

	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
	"github.com/Vikaspal8923/Locdist/master/jobs"
	"github.com/Vikaspal8923/Locdist/master/workers"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type resultWorkerServer struct {
	gradient.UnimplementedWorkerBridgeServer
	files map[string][]byte
}

func (s *resultWorkerServer) GetResultManifest(context.Context, *gradient.JobCommandRequest) (*gradient.ResultManifestResponse, error) {
	files := make([]*gradient.ResultFile, 0, len(s.files))
	for path, content := range s.files {
		digest := sha256.Sum256(content)
		files = append(files, &gradient.ResultFile{Path: path, Size: uint64(len(content)), Sha256: hex.EncodeToString(digest[:]), LogFile: path == "logs/training.log"})
	}
	return &gradient.ResultManifestResponse{JobId: "job-1", WorkerId: "worker-1", Files: files}, nil
}

func (s *resultWorkerServer) DownloadResult(request *gradient.DownloadResultRequest, stream gradient.WorkerBridge_DownloadResultServer) error {
	return stream.Send(&gradient.ResultChunk{Data: s.files[request.GetPath()]})
}

func TestResultCollectorStreamsAndVerifiesWorkerFiles(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	gradient.RegisterWorkerBridgeServer(server, &resultWorkerServer{files: map[string][]byte{"model/model.pt": []byte("model"), "logs/training.log": []byte("trained")}})
	go server.Serve(listener)
	defer server.Stop()

	workerManager := workers.New()
	if err := workerManager.ReservePairing("worker-1", "master-1", "secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := workerManager.Register(&gradient.RegisterWorkerRequest{WorkerId: "worker-1", Host: "bufnet", GrpcPort: "1", MasterId: "master-1", PairingToken: "secret"}); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "ldgcc_results")
	collector := NewResultCollector(workerManager, root)
	collector.dial = func(ctx context.Context, _ string) (*grpc.ClientConn, error) {
		return grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	job := &jobs.JobState{JobID: "job-1", Workers: []jobs.WorkerAssignment{{WorkerID: "worker-1", Host: "bufnet", GRPCPort: "1"}}}
	if err := collector.Collect(context.Background(), job, true); err != nil {
		t.Fatal(err)
	}
	model, err := os.ReadFile(filepath.Join(root, "job-1", "workers", "worker-1", "outputs", "model", "model.pt"))
	if err != nil || string(model) != "model" {
		t.Fatalf("model=%q err=%v", model, err)
	}
	log, err := os.ReadFile(filepath.Join(root, "job-1", "logs", "worker-1", "training.log"))
	if err != nil || string(log) != "trained" {
		t.Fatalf("log=%q err=%v", log, err)
	}
}
