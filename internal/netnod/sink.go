package netnod

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/johanix/dns-anycast-testbed/chimporter/internal/s3io"
)

// Sink accepts DSC files under a relative Hive-style key. The sink decides
// how that key maps onto storage (e.g. S3 bucket path, filesystem path).
type Sink interface {
	Exists(ctx context.Context, relKey string) (bool, error)
	Put(ctx context.Context, relKey string, data []byte, contentType string) error
	// Locator returns a human-readable description of where a given key
	// lives (for log messages).
	Locator(relKey string) string
}

// S3Sink writes objects to S3 under an optional prefix.
type S3Sink struct {
	client *s3io.Client
	prefix string
}

func NewS3Sink(client *s3io.Client, prefix string) *S3Sink {
	return &S3Sink{client: client, prefix: prefix}
}

func (s *S3Sink) resolve(relKey string) string {
	if s.prefix == "" {
		return relKey
	}
	return strings.TrimRight(s.prefix, "/") + "/" + relKey
}

func (s *S3Sink) Exists(ctx context.Context, relKey string) (bool, error) {
	return s.client.Exists(ctx, s.resolve(relKey))
}

func (s *S3Sink) Put(ctx context.Context, relKey string, data []byte, contentType string) error {
	return s.client.Put(ctx, s.resolve(relKey), data, contentType)
}

func (s *S3Sink) Locator(relKey string) string {
	return fmt.Sprintf("s3://%s/%s", s.client.Bucket(), s.resolve(relKey))
}

// LocalSink writes objects to a local directory tree mirroring the Hive layout.
type LocalSink struct {
	root string
}

func NewLocalSink(root string) *LocalSink {
	return &LocalSink{root: root}
}

func (l *LocalSink) path(relKey string) string {
	return filepath.Join(l.root, filepath.FromSlash(relKey))
}

func (l *LocalSink) Exists(ctx context.Context, relKey string) (bool, error) {
	_, err := os.Stat(l.path(relKey))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (l *LocalSink) Put(ctx context.Context, relKey string, data []byte, _ string) error {
	p := l.path(relKey)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("creating dir for %s: %w", p, err)
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming %s → %s: %w", tmp, p, err)
	}
	return nil
}

func (l *LocalSink) Locator(relKey string) string {
	return l.path(relKey)
}

// BuildSinks constructs the sinks listed in cfg.Output. cfg is assumed valid
// (LoadConfig already rejects unknown sink names and empty required fields).
func BuildSinks(ctx context.Context, cfg *Config) ([]Sink, error) {
	sinks := make([]Sink, 0, len(cfg.Output))
	for _, name := range cfg.Output {
		switch name {
		case SinkS3:
			c, err := s3io.NewClient(ctx, cfg.S3)
			if err != nil {
				return nil, fmt.Errorf("creating S3 client: %w", err)
			}
			sinks = append(sinks, NewS3Sink(c, cfg.S3.Prefix))
		case SinkLocal:
			if err := os.MkdirAll(cfg.Local.Path, 0o755); err != nil {
				return nil, fmt.Errorf("creating local root %s: %w", cfg.Local.Path, err)
			}
			sinks = append(sinks, NewLocalSink(cfg.Local.Path))
		}
	}
	return sinks, nil
}
