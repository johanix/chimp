package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type CHClient struct {
	conn     driver.Conn
	database string
}

func NewCHClient(ctx context.Context, cfg CHConfig) (*CHClient, error) {
	// Connect without specifying a database — it may not exist yet.
	// EnsureSchema will create it, and all queries use fully qualified
	// table names (database.table) so no USE is needed.
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
	return &CHClient{conn: conn, database: cfg.Database}, nil
}

// EnsureSchema creates the database and tables if they don't exist.
func (ch *CHClient) EnsureSchema(ctx context.Context) error {
	queries := []string{
		fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", ch.database),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.observations (
			timestamp   DateTime,
			hostname    LowCardinality(String),
			dataset     LowCardinality(String),
			key1        LowCardinality(String),
			key2        LowCardinality(String),
			value       UInt64
		) ENGINE = MergeTree()
		PARTITION BY toYYYYMM(timestamp)
		ORDER BY (dataset, hostname, timestamp, key1, key2)`, ch.database),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.imported_files (
			s3_key      String,
			hostname    String,
			file_time   DateTime,
			imported_at DateTime DEFAULT now()
		) ENGINE = MergeTree()
		ORDER BY (s3_key)`, ch.database),
	}

	for _, q := range queries {
		if err := ch.conn.Exec(ctx, q); err != nil {
			return fmt.Errorf("executing schema query: %w", err)
		}
	}
	log.Println("ClickHouse schema ensured")
	return nil
}

// IsImported checks if an S3 key has already been imported.
func (ch *CHClient) IsImported(ctx context.Context, s3Key string) (bool, error) {
	var count uint64
	err := ch.conn.QueryRow(ctx,
		fmt.Sprintf("SELECT count() FROM %s.imported_files WHERE s3_key = ?", ch.database),
		s3Key).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("checking imported status: %w", err)
	}
	return count > 0, nil
}

// MarkImported records that an S3 key has been imported.
func (ch *CHClient) MarkImported(ctx context.Context, s3Key string, hostname string, fileTime time.Time) error {
	return ch.conn.Exec(ctx,
		fmt.Sprintf("INSERT INTO %s.imported_files (s3_key, hostname, file_time) VALUES (?, ?, ?)", ch.database),
		s3Key, hostname, fileTime)
}

// InsertObservations inserts a batch of observations.
func (ch *CHClient) InsertObservations(ctx context.Context, obs []Observation) error {
	if len(obs) == 0 {
		return nil
	}

	batch, err := ch.conn.PrepareBatch(ctx,
		fmt.Sprintf("INSERT INTO %s.observations", ch.database))
	if err != nil {
		return fmt.Errorf("preparing batch: %w", err)
	}

	for _, o := range obs {
		if err := batch.Append(o.Timestamp, o.Hostname, o.Dataset, o.Key1, o.Key2, o.Value); err != nil {
			return fmt.Errorf("appending to batch: %w", err)
		}
	}
	return batch.Send()
}
