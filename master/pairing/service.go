package pairing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/Vikaspal8923/Locdist/master/discovery"
	gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"
	"github.com/Vikaspal8923/Locdist/master/internal/config"
	"github.com/Vikaspal8923/Locdist/master/workers"
	grpcclient "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Record struct {
	WorkerID string
	Instance string
	Token    string
}

type Service struct {
	mu             sync.RWMutex
	config         config.Config
	discovered     *discovery.Registry
	workerManager  *workers.Manager
	records        map[string]Record
	requestTimeout time.Duration
}

func New(
	cfg config.Config,
	discovered *discovery.Registry,
	workerManager *workers.Manager,
) *Service {
	return &Service{
		config:         cfg,
		discovered:     discovered,
		workerManager:  workerManager,
		records:        make(map[string]Record),
		requestTimeout: 5 * time.Minute,
	}
}

func (s *Service) Pair(
	ctx context.Context,
	id string,
) (Record, error) {
	worker, ok := s.discovered.Worker(id)
	if !ok {
		return Record{}, fmt.Errorf("discovered Worker %q was not found", id)
	}
	if worker.PairingStatus != "unpaired" {
		return Record{}, fmt.Errorf("Worker %q is already paired", worker.Instance)
	}

	requestID, err := randomID("pair")
	if err != nil {
		return Record{}, err
	}
	workerID, err := randomID("worker")
	if err != nil {
		return Record{}, err
	}
	token, err := randomSecret()
	if err != nil {
		return Record{}, err
	}

	if err := s.workerManager.ReservePairing(
		workerID,
		s.config.MasterID,
		token,
	); err != nil {
		return Record{}, err
	}

	record := Record{
		WorkerID: workerID,
		Instance: worker.Instance,
		Token:    token,
	}

	response, err := s.sendRequest(
		ctx,
		worker,
		&gradient.PairWorkerRequest{
			RequestId:      requestID,
			MasterId:       s.config.MasterID,
			MasterName:     s.config.MasterName,
			MasterHost:     reachableHost(s.config.Host),
			MasterGrpcPort: s.config.Port,
			WorkerId:       workerID,
			PairingToken:   token,
		},
	)
	if err != nil {
		s.workerManager.RevokePairing(workerID)
		return Record{}, err
	}
	if response.GetDecision() !=
		gradient.PairingDecision_PAIRING_DECISION_ACCEPTED {
		s.workerManager.RevokePairing(workerID)
		return Record{}, fmt.Errorf(
			"Worker rejected pairing: %s",
			response.GetMessage(),
		)
	}

	s.mu.Lock()
	s.records[workerID] = record
	s.mu.Unlock()

	return record, nil
}

func (s *Service) Records() []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()

	records := make([]Record, 0, len(s.records))
	for _, record := range s.records {
		records = append(records, record)
	}
	return records
}

func (s *Service) sendRequest(
	parent context.Context,
	worker discovery.Worker,
	request *gradient.PairWorkerRequest,
) (*gradient.PairWorkerResponse, error) {
	ctx, cancel := context.WithTimeout(parent, s.requestTimeout)
	defer cancel()

	conn, err := grpcclient.NewClient(
		discovery.Address(worker),
		grpcclient.WithTransportCredentials(
			insecure.NewCredentials(),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to Worker: %w", err)
	}
	defer conn.Close()

	response, err := gradient.NewWorkerBridgeClient(conn).PairWorker(
		ctx,
		request,
	)
	if err != nil {
		return nil, fmt.Errorf("send pairing request: %w", err)
	}
	return response, nil
}

func randomID(prefix string) (string, error) {
	value, err := randomBytes(8)
	if err != nil {
		return "", err
	}
	return prefix + "-" + hex.EncodeToString(value), nil
}

func randomSecret() (string, error) {
	value, err := randomBytes(32)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func randomBytes(size int) ([]byte, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return nil, fmt.Errorf("generate pairing credential: %w", err)
	}
	return value, nil
}
