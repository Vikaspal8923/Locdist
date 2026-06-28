package config

import (
	"encoding/json"
	"os"
)

type Config struct {
	MasterID    string `json:"master_id"`
	MasterName  string `json:"master_name"`
	Host        string `json:"host"`
	Port        string `json:"grpc_port"`
	AppPort     string `json:"app_port"`
	AppHost     string `json:"app_host"`
	PairingPath string `json:"pairing_path"`
}

func Default() Config {
	return Config{
		MasterID:    "master-a",
		MasterName:  "LDGCC Master",
		Host:        "127.0.0.1",
		Port:        "60051",
		AppPort:     "6060",
		AppHost:     "127.0.0.1",
		PairingPath: "master_pairings.json",
	}
}

// TODO(Master Infrastructure Phase):
//
// Current Phase 1 loads master_config.json from the
// current working directory for simplicity.
//
// Future Master architecture may move configuration
// into:
//
//	configs/master_config.json
//
// or another installation-managed location.
//
// This allows Studio, Orchestrator, and deployment
// tooling to manage Master configuration independently
// of the process working directory.
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
