package pairing

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type Record struct {
	WorkerID     string `json:"worker_id"`
	MasterID     string `json:"master_id"`
	MasterName   string `json:"master_name"`
	MasterHost   string `json:"master_host"`
	MasterPort   string `json:"master_grpc_port"`
	PairingToken string `json:"pairing_token"`
}

type Store interface {
	Load() (*Record, error)
	Save(record Record) error
	Delete() error
}

type FileStore struct {
	path string
}

func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

func (s *FileStore) Load() (*Record, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *FileStore) Save(record Record) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}

	temporary, err := os.CreateTemp(directory, ".pairing-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}

	return os.Rename(temporaryPath, s.path)
}

func (s *FileStore) Delete() error {
	err := os.Remove(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
