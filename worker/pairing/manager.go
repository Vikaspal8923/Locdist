package pairing

import (
	"context"
	"fmt"
	"sync"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
)

type pendingRequest struct {
	request  *gradient.PairWorkerRequest
	decision chan *gradient.PairWorkerResponse
}

type Manager struct {
	mu      sync.RWMutex
	store   Store
	record  *Record
	pending *pendingRequest
}

func NewManager(store Store) (*Manager, error) {
	record, err := store.Load()
	if err != nil {
		return nil, fmt.Errorf("load pairing: %w", err)
	}

	return &Manager{
		store:  store,
		record: record,
	}, nil
}

func (m *Manager) Request(
	ctx context.Context,
	request *gradient.PairWorkerRequest,
) (*gradient.PairWorkerResponse, error) {
	if err := validateRequest(request); err != nil {
		return nil, err
	}

	m.mu.Lock()
	if m.record != nil {
		m.mu.Unlock()
		return rejected(
			request.GetRequestId(),
			"Worker is already paired",
		), nil
	}
	if m.pending != nil {
		m.mu.Unlock()
		return rejected(
			request.GetRequestId(),
			"another pairing request is already pending",
		), nil
	}

	pending := &pendingRequest{
		request:  request,
		decision: make(chan *gradient.PairWorkerResponse, 1),
	}
	m.pending = pending
	m.mu.Unlock()

	select {
	case response := <-pending.decision:
		return response, nil
	case <-ctx.Done():
		m.mu.Lock()
		if m.pending == pending {
			m.pending = nil
		}
		m.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (m *Manager) Accept() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.pending == nil {
		return fmt.Errorf("no pairing request is pending")
	}

	request := m.pending.request
	record := Record{
		WorkerID:     request.GetWorkerId(),
		MasterID:     request.GetMasterId(),
		MasterName:   request.GetMasterName(),
		MasterHost:   request.GetMasterHost(),
		MasterPort:   request.GetMasterGrpcPort(),
		PairingToken: request.GetPairingToken(),
	}
	if err := m.store.Save(record); err != nil {
		return fmt.Errorf("save pairing: %w", err)
	}

	pending := m.pending
	m.record = &record
	m.pending = nil
	pending.decision <- &gradient.PairWorkerResponse{
		RequestId: request.GetRequestId(),
		Decision: gradient.
			PairingDecision_PAIRING_DECISION_ACCEPTED,
		Message: "pairing accepted",
	}
	return nil
}

func (m *Manager) Reject() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.pending == nil {
		return fmt.Errorf("no pairing request is pending")
	}

	pending := m.pending
	m.pending = nil
	pending.decision <- rejected(
		pending.request.GetRequestId(),
		"pairing rejected by Worker owner",
	)
	return nil
}

func (m *Manager) Reset() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.pending != nil {
		return fmt.Errorf("cannot reset while pairing is pending")
	}
	if err := m.store.Delete(); err != nil {
		return fmt.Errorf("delete pairing: %w", err)
	}
	m.record = nil
	return nil
}

func (m *Manager) Record() (*Record, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.record == nil {
		return nil, false
	}
	record := *m.record
	return &record, true
}

func (m *Manager) Pending() (*gradient.PairWorkerRequest, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.pending == nil {
		return nil, false
	}
	return m.pending.request, true
}

func rejected(
	requestID string,
	message string,
) *gradient.PairWorkerResponse {
	return &gradient.PairWorkerResponse{
		RequestId: requestID,
		Decision: gradient.
			PairingDecision_PAIRING_DECISION_REJECTED,
		Message: message,
	}
}

func validateRequest(request *gradient.PairWorkerRequest) error {
	if request.GetRequestId() == "" ||
		request.GetMasterId() == "" ||
		request.GetMasterName() == "" ||
		request.GetMasterHost() == "" ||
		request.GetMasterGrpcPort() == "" ||
		request.GetWorkerId() == "" ||
		request.GetPairingToken() == "" {
		return fmt.Errorf("pairing request is incomplete")
	}
	return nil
}
