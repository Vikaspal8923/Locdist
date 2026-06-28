package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	WorkerID   string `json:"worker_id"`
	WorkerName string `json:"worker_name"`

	Port          string `json:"grpc_port"`
	AppPort       string `json:"app_port"`
	PairingPath   string `json:"pairing_path"`
	WorkspaceRoot string `json:"workspace_root"`

	Host       string `json:"host"`
	MasterHost string `json:"master_host"`
	MasterPort string `json:"master_port"`
}

func Default() Config {
	return Config{
		WorkerName:    "LDGCC Worker",
		Port:          "50051",
		AppPort:       "5050",
		PairingPath:   "pairing.json",
		WorkspaceRoot: "workspaces",
		Host:          "127.0.0.1",

		MasterHost: "127.0.0.1",
		MasterPort: "60051",
	}
}

// TODO(Worker Infrastructure Phase):
//
// Current Phase 1 loads worker_config.json from the current
// working directory for simplicity.
//
// Future Worker architecture should move configuration to a
// dedicated location such as:
//
//	configs/worker_config.json
//
// or
//
//	~/.ldgcc/worker_config.json
//
// so configuration becomes independent of the process
// working directory and can be managed by the Worker
// installation, Tray App, or Master-generated setup
func Load(path string) (Config, error) {

	cfg := Default()

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, nil
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func Save(path string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}

	temporary, err := os.CreateTemp(directory, ".worker-config-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(0o600); err != nil {
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
	return os.Rename(temporaryPath, path)
}
