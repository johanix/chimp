package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/johanix/chimp/internal/chdb"
	"github.com/johanix/chimp/internal/dsc"
	"github.com/johanix/chimp/internal/s3io"
)

var configPath string
var s3prefix string
var fromHour, toHour string

func main() {
	rootCmd := &cobra.Command{
		Use:   "chimporter",
		Short: "Import DSC data from S3 into ClickHouse",
	}
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "chimporter.yaml", "path to config file")
	rootCmd.PersistentFlags().StringVarP(&s3prefix, "prefix", "p", "",
		"S3 time prefix (e.g. year=2026/month=03/day=25/hour=17/); mutually exclusive with --from/--to")
	rootCmd.PersistentFlags().StringVar(&fromHour, "from", "",
		"UTC start hour, inclusive (YYYYMMDDHH); requires --to")
	rootCmd.PersistentFlags().StringVar(&toHour, "to", "",
		"UTC end hour, exclusive (YYYYMMDDHH); requires --from")

	rootCmd.AddCommand(listCmd())
	rootCmd.AddCommand(parseCmd())
	rootCmd.AddCommand(importCmd())
	rootCmd.AddCommand(daemonCmd())
	rootCmd.AddCommand(syncSitesCmd())

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
			prefixes, err := resolvePrefixes()
			if err != nil {
				return err
			}
			s3c, err := s3io.NewClient(ctx, cfg.S3)
			if err != nil {
				return err
			}
			runDryMode(ctx, s3c, false, prefixes)
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
			prefixes, err := resolvePrefixes()
			if err != nil {
				return err
			}
			s3c, err := s3io.NewClient(ctx, cfg.S3)
			if err != nil {
				return err
			}
			runDryMode(ctx, s3c, true, prefixes)
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
			prefixes, err := resolvePrefixes()
			if err != nil {
				return err
			}
			s3c, err := s3io.NewClient(ctx, cfg.S3)
			if err != nil {
				return err
			}
			ch, err := chdb.NewClient(ctx, cfg.ClickHouse)
			if err != nil {
				return err
			}
			if err := ch.EnsureSchema(ctx); err != nil {
				return err
			}
			return pollAndImport(ctx, s3c, ch, prefixes)
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
			s3c, err := s3io.NewClient(ctx, cfg.S3)
			if err != nil {
				return err
			}
			ch, err := chdb.NewClient(ctx, cfg.ClickHouse)
			if err != nil {
				return err
			}
			if err := ch.EnsureSchema(ctx); err != nil {
				return err
			}

			log.Printf("Starting chimporter daemon: polling every %ds", cfg.PollInterval)
			ticker := time.NewTicker(time.Duration(cfg.PollInterval) * time.Second)
			defer ticker.Stop()

			if err := pollAndImport(ctx, s3c, ch, defaultDayPrefixes()); err != nil {
				log.Printf("Import error: %v", err)
			}

			for {
				select {
				case <-ctx.Done():
					log.Println("Shutting down")
					return nil
				case <-ticker.C:
					if err := pollAndImport(ctx, s3c, ch, defaultDayPrefixes()); err != nil {
						log.Printf("Import error: %v", err)
					}
				}
			}
		},
	}
}

func syncSitesCmd() *cobra.Command {
	var sitesPath string
	cmd := &cobra.Command{
		Use:   "sync-sites",
		Short: "Load sites.yaml and replace dsc.sites with its contents",
		Long: "Reads the sites YAML file and TRUNCATE+INSERTs into dsc.sites. " +
			"Run after editing sites.yaml to publish updates to dashboards.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, ctx, cancel := setup()
			defer cancel()

			path := sitesPath
			if path == "" {
				path = cfg.SitesFile
			}
			if path == "" {
				return fmt.Errorf("no sites file specified (set sites_file in config or pass --file)")
			}

			sites, err := LoadSitesYAML(path)
			if err != nil {
				return err
			}
			log.Printf("loaded %d sites from %s", len(sites), path)

			ch, err := chdb.NewClient(ctx, cfg.ClickHouse)
			if err != nil {
				return err
			}
			if err := ch.EnsureSchema(ctx); err != nil {
				return err
			}
			if err := ch.SyncSites(ctx, sites); err != nil {
				return err
			}
			log.Printf("synced %d sites into dsc.sites", len(sites))
			return nil
		},
	}
	cmd.Flags().StringVarP(&sitesPath, "file", "f", "",
		"path to sites.yaml (overrides config.sites_file)")
	return cmd
}

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

func runDryMode(ctx context.Context, s3c *s3io.Client, doParse bool, prefixes []string) {
	for _, pfx := range prefixes {
		files, err := s3c.List(ctx, pfx)
		if err != nil {
			log.Fatalf("Failed to list files for %s: %v", pfx, err)
		}

		log.Printf("Prefix %s: %d files", pfx, len(files))

		for _, f := range files {
			if !doParse {
				fmt.Printf("  %s  provider=%s  site=%s  host=%s  time=%s\n",
					f.Key, f.Provider, f.Site, f.Hostname, f.Timestamp.Format(time.RFC3339))
				continue
			}

			if !isParseable(f.Filename) {
				fmt.Printf("  %s  [skipped: no parser for %s]\n", f.Key, f.Filename)
				continue
			}

			data, err := s3c.Fetch(ctx, f.Key)
			if err != nil {
				log.Printf("  FETCH ERROR %s: %v", f.Key, err)
				continue
			}

			obs, err := parseDSC(data, f)
			if err != nil {
				log.Printf("  PARSE ERROR %s: %v", f.Key, err)
				continue
			}

			fmt.Printf("  %s  provider=%s  site=%s  host=%s  time=%s  observations=%d\n",
				f.Key, f.Provider, f.Site, f.Hostname, f.Timestamp.Format(time.RFC3339), len(obs))

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

// resolvePrefixes turns the user-supplied flags (--prefix / --from+--to) or
// the default (today + yesterday) into a concrete list of S3 key prefixes
// to iterate. --prefix is exclusive with --from/--to; --from and --to must
// come together and span at least one hour.
func resolvePrefixes() ([]string, error) {
	hasRange := fromHour != "" || toHour != ""
	if s3prefix != "" && hasRange {
		return nil, fmt.Errorf("--prefix is mutually exclusive with --from/--to")
	}
	if (fromHour == "") != (toHour == "") {
		return nil, fmt.Errorf("--from and --to must be used together")
	}

	if s3prefix != "" {
		return []string{s3prefix}, nil
	}

	if hasRange {
		from, err := parseHour(fromHour)
		if err != nil {
			return nil, fmt.Errorf("--from: %w", err)
		}
		to, err := parseHour(toHour)
		if err != nil {
			return nil, fmt.Errorf("--to: %w", err)
		}
		if !to.After(from) {
			return nil, fmt.Errorf("--to must be strictly after --from")
		}
		var out []string
		for t := from; t.Before(to); t = t.Add(time.Hour) {
			out = append(out, fmt.Sprintf("year=%d/month=%02d/day=%02d/hour=%02d/",
				t.Year(), t.Month(), t.Day(), t.Hour()))
		}
		return out, nil
	}

	// Default: today + yesterday, day-level.
	now := time.Now().UTC()
	yesterday := now.AddDate(0, 0, -1)
	return []string{
		fmt.Sprintf("year=%d/month=%02d/day=%02d/", now.Year(), now.Month(), now.Day()),
		fmt.Sprintf("year=%d/month=%02d/day=%02d/", yesterday.Year(), yesterday.Month(), yesterday.Day()),
	}, nil
}

// defaultDayPrefixes returns the daemon's standing "recent window" —
// today + yesterday in day granularity. Daemon ignores --from/--to/--prefix.
func defaultDayPrefixes() []string {
	now := time.Now().UTC()
	yesterday := now.AddDate(0, 0, -1)
	return []string{
		fmt.Sprintf("year=%d/month=%02d/day=%02d/", now.Year(), now.Month(), now.Day()),
		fmt.Sprintf("year=%d/month=%02d/day=%02d/", yesterday.Year(), yesterday.Month(), yesterday.Day()),
	}
}

func parseHour(s string) (time.Time, error) {
	t, err := time.Parse("2006010215", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing hour %q: %w", s, err)
	}
	return t.UTC(), nil
}

// parseDSC dispatches to the right DSC parser based on filename.
// Returns (nil, nil) for filenames with no known parser — callers skip.
func parseDSC(data []byte, f s3io.File) ([]dsc.Observation, error) {
	lower := strings.ToLower(f.Filename)
	switch {
	case strings.HasSuffix(lower, ".xml.gz"):
		return dsc.ParseXMLGz(data, f.Provider, f.Site, f.Hostname)
	case strings.HasSuffix(lower, ".xml"):
		return dsc.ParseXML(data, f.Provider, f.Site, f.Hostname)
	case strings.HasSuffix(lower, ".json"), !strings.Contains(lower, "."):
		return dsc.ParseJSON(data, f.Provider, f.Site, f.Hostname)
	default:
		return nil, nil
	}
}

// isParseable reports whether any DSC parser claims the given filename.
func isParseable(filename string) bool {
	lower := strings.ToLower(filename)
	return strings.HasSuffix(lower, ".xml.gz") ||
		strings.HasSuffix(lower, ".xml") ||
		strings.HasSuffix(lower, ".json") ||
		!strings.Contains(lower, ".")
}

func pollAndImport(ctx context.Context, s3c *s3io.Client, ch *chdb.Client, prefixes []string) error {
	var totalImported int
	for _, prefix := range prefixes {
		files, err := s3c.List(ctx, prefix)
		if err != nil {
			return fmt.Errorf("listing files for %s: %w", prefix, err)
		}

		for _, f := range files {
			if !isParseable(f.Filename) {
				continue
			}
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

func importFile(ctx context.Context, s3c *s3io.Client, ch *chdb.Client, f s3io.File) error {
	data, err := s3c.Fetch(ctx, f.Key)
	if err != nil {
		return fmt.Errorf("fetching: %w", err)
	}

	obs, err := parseDSC(data, f)
	if err != nil {
		return fmt.Errorf("parsing: %w", err)
	}

	if err := ch.InsertObservations(ctx, obs); err != nil {
		return fmt.Errorf("inserting: %w", err)
	}

	if err := ch.MarkImported(ctx, f.Key, f.Provider, f.Site, f.Hostname, f.Timestamp); err != nil {
		return fmt.Errorf("marking imported: %w", err)
	}

	log.Printf("Imported %s: %d observations from provider=%s site=%s host=%s",
		f.Key, len(obs), f.Provider, f.Site, f.Hostname)
	return nil
}
