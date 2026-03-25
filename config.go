package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	S3           S3Config `yaml:"s3"`
	ClickHouse   CHConfig `yaml:"clickhouse"`
	PollInterval int      `yaml:"poll_interval"` // seconds
}

type S3Config struct {
	Bucket   string `yaml:"bucket"`
	Prefix   string `yaml:"prefix"`
	Region   string `yaml:"region"`
	Endpoint string `yaml:"endpoint"` // for non-AWS S3-compatible stores
	Profile  string `yaml:"profile"`  // AWS profile name
}

type CHConfig struct {
	Address  string `yaml:"address"`
	Database string `yaml:"database"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	cfg := &Config{
		PollInterval: 60,
		ClickHouse: CHConfig{
			Address:  "localhost:9000",
			Database: "dsc",
		},
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return cfg, nil
}
