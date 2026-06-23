package config

import (
	"encoding/json"
	"os"
)

type Config struct {
	GRPCPort int `json:"grpc_port"`
}

func Default() Config {
	return Config{
		GRPCPort: 50051,
	}
}

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