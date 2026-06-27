package workers

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type PairingStore interface {
	Load() (map[string]Pairing, error)
	Save(pairings map[string]Pairing) error
}

type FilePairingStore struct {
	path string
}

func NewFilePairingStore(path string) *FilePairingStore {
	return &FilePairingStore{path: path}
}

func (s *FilePairingStore) Load() (map[string]Pairing, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]Pairing), nil
	}
	if err != nil {
		return nil, err
	}

	pairings := make(map[string]Pairing)
	if err := json.Unmarshal(data, &pairings); err != nil {
		return nil, err
	}
	return pairings, nil
}

func (s *FilePairingStore) Save(pairings map[string]Pairing) error {
	data, err := json.MarshalIndent(pairings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}

	temporary, err := os.CreateTemp(directory, ".master-pairings-*.json")
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
