package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/johanix/chimp/internal/chdb"
	"github.com/johanix/chimp/internal/s3io"
)

type Config struct {
	S3           s3io.Config `yaml:"s3"`
	ClickHouse   chdb.Config `yaml:"clickhouse"`
	PollInterval int         `yaml:"poll_interval"` // seconds
	SitesFile    string      `yaml:"sites_file"`    // path to sites.yaml (for sync-sites)
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	cfg := &Config{
		PollInterval: 60,
		SitesFile:    "./sites.yaml",
		ClickHouse: chdb.Config{
			Address:  "localhost:9000",
			Database: "dsc",
		},
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return cfg, nil
}
