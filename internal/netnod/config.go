package netnod

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/johanix/dns-anycast-testbed/chimporter/internal/s3io"
)

type NetnodConfig struct {
	BaseURL string   `yaml:"base_url"`
	Token   string   `yaml:"token"`
	Sites   []string `yaml:"sites"` // optional; empty → use /sites/available
}

type LocalConfig struct {
	Path string `yaml:"path"`
}

// Sink names recognized by Config.Output.
const (
	SinkS3    = "s3"
	SinkLocal = "local"
)

type Config struct {
	Netnod   NetnodConfig `yaml:"netnod"`
	Provider string       `yaml:"provider"`

	// Output names which sinks to write to. Required, non-empty.
	// Each named sink's config section below must be populated.
	// Valid values: "s3", "local". Listing both mirrors writes.
	Output []string `yaml:"output"`

	S3    s3io.Config `yaml:"s3"`
	Local LocalConfig `yaml:"local"`

	PollInterval   int `yaml:"poll_interval"`   // seconds between cycles
	LagMinutes     int `yaml:"lag_minutes"`     // wall-clock lag before fetching a minute
	CatchupMinutes int `yaml:"catchup_minutes"` // minutes to re-scan each cycle
}

// LoadConfig reads path and applies d's BaseURL/Provider as defaults before
// unmarshalling, so per-binary defaults live at the call site.
func LoadConfig(path string, d Defaults) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	cfg := &Config{
		Provider:       d.Provider,
		PollInterval:   60,
		LagMinutes:     2,
		CatchupMinutes: 5,
		Netnod: NetnodConfig{
			BaseURL: d.BaseURL,
		},
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if cfg.Netnod.Token == "" {
		return nil, fmt.Errorf("netnod.token is required")
	}
	if cfg.Provider == "" {
		return nil, fmt.Errorf("provider is required")
	}
	if len(cfg.Output) == 0 {
		return nil, fmt.Errorf("output is required: list the sinks to write to, e.g. output: [s3] or output: [local] or output: [s3, local]")
	}
	seen := make(map[string]bool, len(cfg.Output))
	for _, name := range cfg.Output {
		if seen[name] {
			return nil, fmt.Errorf("output sink %q listed more than once", name)
		}
		seen[name] = true
		switch name {
		case SinkS3:
			if cfg.S3.Bucket == "" {
				return nil, fmt.Errorf("output lists %q but s3.bucket is empty", name)
			}
		case SinkLocal:
			if cfg.Local.Path == "" {
				return nil, fmt.Errorf("output lists %q but local.path is empty", name)
			}
		default:
			return nil, fmt.Errorf("unknown output sink %q (valid: %q, %q)", name, SinkS3, SinkLocal)
		}
	}
	return cfg, nil
}
