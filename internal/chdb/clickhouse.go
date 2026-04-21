// Package chdb provides the ClickHouse client used by chimporter.
package chdb

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/johanix/dns-anycast-testbed/chimporter/internal/dsc"
)

type Config struct {
	Address  string `yaml:"address"`
	Database string `yaml:"database"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type Client struct {
	conn     driver.Conn
	database string
}

func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{cfg.Address},
		Auth: clickhouse.Auth{
			Username: cfg.Username,
			Password: cfg.Password,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("connecting to ClickHouse: %w", err)
	}
	if err := conn.Ping(ctx); err != nil {
		return nil, fmt.Errorf("pinging ClickHouse: %w", err)
	}
	return &Client{conn: conn, database: cfg.Database}, nil
}

// EnsureSchema creates the database and tables if they don't exist.
func (c *Client) EnsureSchema(ctx context.Context) error {
	queries := []string{
		fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", c.database),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.observations (
			timestamp   DateTime,
			provider    LowCardinality(String),
			site        LowCardinality(String),
			hostname    LowCardinality(String),
			dataset     LowCardinality(String),
			key1        LowCardinality(String),
			key2        LowCardinality(String),
			value       UInt64
		) ENGINE = MergeTree()
		PARTITION BY toYYYYMM(timestamp)
		ORDER BY (dataset, provider, site, hostname, timestamp, key1, key2)`, c.database),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.imported_files (
			s3_key      String,
			provider    LowCardinality(String),
			site        LowCardinality(String),
			hostname    String,
			file_time   DateTime,
			imported_at DateTime DEFAULT now()
		) ENGINE = MergeTree()
		ORDER BY (s3_key)`, c.database),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.sites (
			provider    LowCardinality(String),
			site        String,
			service     LowCardinality(String),
			city        String,
			country     String,
			country_iso LowCardinality(String),
			tags        Array(LowCardinality(String)),
			lat         Float64,
			lon         Float64,
			notes       String,
			updated_at  DateTime DEFAULT now()
		) ENGINE = ReplacingMergeTree(updated_at)
		ORDER BY (provider, site)`, c.database),
	}

	for _, q := range queries {
		if err := c.conn.Exec(ctx, q); err != nil {
			return fmt.Errorf("executing schema query: %w", err)
		}
	}
	log.Println("ClickHouse schema ensured")
	return nil
}

func (c *Client) IsImported(ctx context.Context, s3Key string) (bool, error) {
	var count uint64
	err := c.conn.QueryRow(ctx,
		fmt.Sprintf("SELECT count() FROM %s.imported_files WHERE s3_key = ?", c.database),
		s3Key).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("checking imported status: %w", err)
	}
	return count > 0, nil
}

func (c *Client) MarkImported(ctx context.Context, s3Key, provider, site, hostname string, fileTime time.Time) error {
	return c.conn.Exec(ctx,
		fmt.Sprintf("INSERT INTO %s.imported_files (s3_key, provider, site, hostname, file_time) VALUES (?, ?, ?, ?, ?)", c.database),
		s3Key, provider, site, hostname, fileTime)
}

// Site represents a row in the dsc.sites lookup table.
type Site struct {
	Provider   string
	Site       string
	Service    string // "anycast" or "unicast"
	City       string
	Country    string
	CountryISO string
	Tags       []string
	Lat        float64
	Lon        float64
	Notes      string
}

// SyncSites truncates dsc.sites and inserts the given rows in a single
// batch. The table is small (one row per provider/site, ~100 rows per
// provider), so a full replace is simpler than a diff-based merge.
func (c *Client) SyncSites(ctx context.Context, sites []Site) error {
	if err := c.conn.Exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s.sites", c.database)); err != nil {
		return fmt.Errorf("truncating sites: %w", err)
	}

	if len(sites) == 0 {
		return nil
	}

	batch, err := c.conn.PrepareBatch(ctx,
		fmt.Sprintf("INSERT INTO %s.sites", c.database))
	if err != nil {
		return fmt.Errorf("preparing sites batch: %w", err)
	}

	now := time.Now().UTC()
	for _, s := range sites {
		if err := batch.Append(
			s.Provider, s.Site, s.Service, s.City, s.Country, s.CountryISO,
			s.Tags, s.Lat, s.Lon, s.Notes, now,
		); err != nil {
			return fmt.Errorf("appending site %s/%s: %w", s.Provider, s.Site, err)
		}
	}
	return batch.Send()
}

func (c *Client) InsertObservations(ctx context.Context, obs []dsc.Observation) error {
	if len(obs) == 0 {
		return nil
	}

	batch, err := c.conn.PrepareBatch(ctx,
		fmt.Sprintf("INSERT INTO %s.observations", c.database))
	if err != nil {
		return fmt.Errorf("preparing batch: %w", err)
	}

	for _, o := range obs {
		if err := batch.Append(o.Timestamp, o.Provider, o.Site, o.Hostname, o.Dataset, o.Key1, o.Key2, o.Value); err != nil {
			return fmt.Errorf("appending to batch: %w", err)
		}
	}
	return batch.Send()
}
