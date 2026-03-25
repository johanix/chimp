package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

var configPath string
var s3prefix string

func main() {
	rootCmd := &cobra.Command{
		Use:   "chimporter",
		Short: "Import DSC data from S3 into ClickHouse",
	}
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "chimporter.yaml", "path to config file")
	rootCmd.PersistentFlags().StringVarP(&s3prefix, "prefix", "p", "", "S3 time prefix (e.g. year=2026/month=03/day=25/); default: today+yesterday")

	rootCmd.AddCommand(listCmd())
	rootCmd.AddCommand(parseCmd())
	rootCmd.AddCommand(importCmd())
	rootCmd.AddCommand(daemonCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List S3 files (no ClickHouse needed)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, ctx, cancel := setup()
			defer cancel()
			s3c, err := NewS3Client(ctx, cfg.S3)
			if err != nil {
				return err
			}
			runDryMode(ctx, s3c, false, s3prefix)
			return nil
		},
	}
}

func parseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "parse",
		Short: "Fetch and parse S3 files (no ClickHouse needed)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, ctx, cancel := setup()
			defer cancel()
			s3c, err := NewS3Client(ctx, cfg.S3)
			if err != nil {
				return err
			}
			runDryMode(ctx, s3c, true, s3prefix)
			return nil
		},
	}
}

func importCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import",
		Short: "One-shot import from S3 into ClickHouse",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, ctx, cancel := setup()
			defer cancel()
			s3c, err := NewS3Client(ctx, cfg.S3)
			if err != nil {
				return err
			}
			ch, err := NewCHClient(ctx, cfg.ClickHouse)
			if err != nil {
				return err
			}
			if err := ch.EnsureSchema(ctx); err != nil {
				return err
			}
			return pollAndImport(ctx, s3c, ch, s3prefix)
		},
	}
}

func daemonCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Run as persistent importer, polling S3 continuously",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, ctx, cancel := setup()
			defer cancel()
			s3c, err := NewS3Client(ctx, cfg.S3)
			if err != nil {
				return err
			}
			ch, err := NewCHClient(ctx, cfg.ClickHouse)
			if err != nil {
				return err
			}
			if err := ch.EnsureSchema(ctx); err != nil {
				return err
			}

			log.Printf("Starting chimporter daemon: polling every %ds", cfg.PollInterval)
			ticker := time.NewTicker(time.Duration(cfg.PollInterval) * time.Second)
			defer ticker.Stop()

			if err := pollAndImport(ctx, s3c, ch, ""); err != nil {
				log.Printf("Import error: %v", err)
			}

			for {
				select {
				case <-ctx.Done():
					log.Println("Shutting down")
					return nil
				case <-ticker.C:
					if err := pollAndImport(ctx, s3c, ch, ""); err != nil {
						log.Printf("Import error: %v", err)
					}
				}
			}
		},
	}
}

// setup loads config and creates a cancellable context with signal handling.
func setup() (*Config, context.Context, context.CancelFunc) {
	cfg, err := LoadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("Received signal %v, shutting down", sig)
		cancel()
	}()

	return cfg, ctx, cancel
}

func runDryMode(ctx context.Context, s3c *S3Client, doParse bool, prefix string) {
	prefixes := timePrefixes(prefix)

	for _, pfx := range prefixes {
		files, err := s3c.ListFiles(ctx, pfx)
		if err != nil {
			log.Fatalf("Failed to list files for %s: %v", pfx, err)
		}

		log.Printf("Prefix %s: %d files", pfx, len(files))

		for _, f := range files {
			if !doParse {
				fmt.Printf("  %s  host=%s  time=%s\n", f.Key, f.Hostname, f.Timestamp.Format(time.RFC3339))
				continue
			}

			data, err := s3c.FetchFile(ctx, f.Key)
			if err != nil {
				log.Printf("  FETCH ERROR %s: %v", f.Key, err)
				continue
			}

			obs, err := ParseDSCJSON(data, f.Hostname)
			if err != nil {
				log.Printf("  PARSE ERROR %s: %v", f.Key, err)
				continue
			}

			fmt.Printf("  %s  host=%s  time=%s  observations=%d\n",
				f.Key, f.Hostname, f.Timestamp.Format(time.RFC3339), len(obs))

			datasets := map[string]int{}
			for _, o := range obs {
				datasets[o.Dataset]++
			}
			for ds, count := range datasets {
				fmt.Printf("    dataset=%-20s rows=%d\n", ds, count)
			}
		}
	}
}

// timePrefixes returns the S3 time prefixes to scan.
func timePrefixes(explicit string) []string {
	if explicit != "" {
		return []string{explicit}
	}
	now := time.Now().UTC()
	yesterday := now.AddDate(0, 0, -1)
	return []string{
		fmt.Sprintf("year=%d/month=%02d/day=%02d/", now.Year(), now.Month(), now.Day()),
		fmt.Sprintf("year=%d/month=%02d/day=%02d/", yesterday.Year(), yesterday.Month(), yesterday.Day()),
	}
}

// pollAndImport lists recent S3 files and imports any that haven't
// been imported yet.
func pollAndImport(ctx context.Context, s3c *S3Client, ch *CHClient, explicitPrefix string) error {
	prefixes := timePrefixes(explicitPrefix)

	var totalImported int
	for _, prefix := range prefixes {
		files, err := s3c.ListFiles(ctx, prefix)
		if err != nil {
			return fmt.Errorf("listing files for %s: %w", prefix, err)
		}

		for _, f := range files {
			imported, err := ch.IsImported(ctx, f.Key)
			if err != nil {
				return fmt.Errorf("checking import status: %w", err)
			}
			if imported {
				continue
			}

			if err := importFile(ctx, s3c, ch, f); err != nil {
				log.Printf("Failed to import %s: %v", f.Key, err)
				continue
			}
			totalImported++
		}
	}

	if totalImported > 0 {
		log.Printf("Imported %d new files", totalImported)
	}
	return nil
}

func importFile(ctx context.Context, s3c *S3Client, ch *CHClient, f S3File) error {
	data, err := s3c.FetchFile(ctx, f.Key)
	if err != nil {
		return fmt.Errorf("fetching: %w", err)
	}

	obs, err := ParseDSCJSON(data, f.Hostname)
	if err != nil {
		return fmt.Errorf("parsing: %w", err)
	}

	if err := ch.InsertObservations(ctx, obs); err != nil {
		return fmt.Errorf("inserting: %w", err)
	}

	if err := ch.MarkImported(ctx, f.Key, f.Hostname, f.Timestamp); err != nil {
		return fmt.Errorf("marking imported: %w", err)
	}

	log.Printf("Imported %s: %d observations from %s", f.Key, len(obs), f.Hostname)
	return nil
}
