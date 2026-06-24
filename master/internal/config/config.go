package config

import (
	"encoding/json"
	"os"
)

type Config struct {
	Port string `json:"grpc_port"`
}

func Default() Config {
	return Config{
		Port: "60051",
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
