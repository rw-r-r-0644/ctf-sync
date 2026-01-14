package main

import (
	"encoding/json"
	"os"
)

type Config struct {
	Backend string            `json:"backend"`
	Config  map[string]string `json:"config"`
}

func loadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{Config: make(map[string]string)}, nil
		}
		return nil, err
	}
	defer f.Close()

	var cfg Config
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.Config == nil {
		cfg.Config = make(map[string]string)
	}
	return &cfg, nil
}
