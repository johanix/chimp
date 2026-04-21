package netnod

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
)

// Defaults are per-binary values baked into the cobra tree so one root command
// can serve multiple fetch-netnod-* binaries that differ only in defaults.
type Defaults struct {
	Use        string // binary name, e.g. "fetch-netnod-any"
	Short      string // one-line description for --help
	ConfigPath string // default --config path
	BaseURL    string // default netnod.base_url
	Provider   string // default provider name (used for storage keys)
}

// NewRootCmd builds the cobra command tree. Every subcommand closes over
// configPath (set by --config) and d; no package-level state.
func NewRootCmd(d Defaults) *cobra.Command {
	var configPath string

	rootCmd := &cobra.Command{
		Use:   d.Use,
		Short: d.Short,
	}
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c",
		d.ConfigPath, "path to config file")

	setup := func() (*Config, context.Context, context.CancelFunc) {
		cfg, err := LoadConfig(configPath, d)
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

	rootCmd.AddCommand(&cobra.Command{
		Use:   "sites",
		Short: "List configured sites",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, ctx, cancel := setup()
			defer cancel()
			nc := NewClient(cfg.Netnod)
			sites, err := nc.SitesConfigured(ctx)
			if err != nil {
				return err
			}
			for _, s := range sites {
				fmt.Println(s)
			}
			return nil
		},
	})

	var availableTime string
	availCmd := &cobra.Command{
		Use:   "available",
		Short: "List sites with statistics at a specific UTC minute (YYYYMMDDHHMM)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, ctx, cancel := setup()
			defer cancel()
			t, err := parseMinute(availableTime)
			if err != nil {
				return err
			}
			nc := NewClient(cfg.Netnod)
			sites, err := nc.SitesAvailable(ctx, t)
			if err != nil {
				return err
			}
			for _, s := range sites {
				fmt.Println(s)
			}
			return nil
		},
	}
	availCmd.Flags().StringVarP(&availableTime, "time", "t", "", "UTC minute (YYYYMMDDHHMM); default: now - lag")
	rootCmd.AddCommand(availCmd)

	var fetchTime string
	fCmd := &cobra.Command{
		Use:   "fetch",
		Short: "One-shot fetch for a specific UTC minute (YYYYMMDDHHMM)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, ctx, cancel := setup()
			defer cancel()
			t, err := parseMinute(fetchTime)
			if err != nil {
				return err
			}
			nc := NewClient(cfg.Netnod)
			sinks, err := BuildSinks(ctx, cfg)
			if err != nil {
				return err
			}
			FetchMinute(ctx, cfg, nc, sinks, t)
			return nil
		},
	}
	fCmd.Flags().StringVarP(&fetchTime, "time", "t", "", "UTC minute (YYYYMMDDHHMM); default: now - lag")
	rootCmd.AddCommand(fCmd)

	var fromStr, toStr, sitesStr string
	bfCmd := &cobra.Command{
		Use:   "backfill",
		Short: "Fetch historical DSC data by hour (efficient for multi-day/month backfill)",
		Long: "Fetch DSC data for a range of UTC hours using the hourly API endpoint. " +
			"Each hour produces one blob containing 60 minutes of arrays. " +
			"Already-fetched hours (present in every configured sink) are skipped.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, ctx, cancel := setup()
			defer cancel()

			from, err := parseHour(fromStr)
			if err != nil {
				return fmt.Errorf("--from: %w", err)
			}
			to, err := parseHour(toStr)
			if err != nil {
				return fmt.Errorf("--to: %w", err)
			}
			if !to.After(from) {
				return fmt.Errorf("--to must be strictly after --from")
			}

			nc := NewClient(cfg.Netnod)
			sinks, err := BuildSinks(ctx, cfg)
			if err != nil {
				return err
			}

			sites, err := resolveBackfillSites(ctx, cfg, nc, sitesStr)
			if err != nil {
				return fmt.Errorf("resolving sites: %w", err)
			}

			hours := int(to.Sub(from) / time.Hour)
			log.Printf("Backfilling %s → %s (%d hours) across %d sites: %v",
				from.Format(time.RFC3339), to.Format(time.RFC3339), hours, len(sites), sites)

			for t := from; t.Before(to); t = t.Add(time.Hour) {
				select {
				case <-ctx.Done():
					log.Println("Backfill interrupted")
					return nil
				default:
				}
				FetchHour(ctx, cfg, nc, sinks, sites, t)
			}
			return nil
		},
	}
	bfCmd.Flags().StringVar(&fromStr, "from", "", "UTC start hour, inclusive (YYYYMMDDHH)")
	bfCmd.Flags().StringVar(&toStr, "to", "", "UTC end hour, exclusive (YYYYMMDDHH)")
	bfCmd.Flags().StringVar(&sitesStr, "sites", "",
		"comma-separated site list (overrides config); default: config.netnod.sites, or /sites/configured")
	bfCmd.MarkFlagRequired("from")
	bfCmd.MarkFlagRequired("to")
	rootCmd.AddCommand(bfCmd)

	rootCmd.AddCommand(&cobra.Command{
		Use:   "daemon",
		Short: "Run continuous fetch loop",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, ctx, cancel := setup()
			defer cancel()

			nc := NewClient(cfg.Netnod)
			sinks, err := BuildSinks(ctx, cfg)
			if err != nil {
				return err
			}

			log.Printf("Starting %s: provider=%s poll=%ds lag=%dmin catchup=%dmin sinks=%d",
				d.Use, cfg.Provider, cfg.PollInterval, cfg.LagMinutes, cfg.CatchupMinutes, len(sinks))

			ticker := time.NewTicker(time.Duration(cfg.PollInterval) * time.Second)
			defer ticker.Stop()

			doCycle := func() {
				end := time.Now().UTC().
					Add(-time.Duration(cfg.LagMinutes) * time.Minute).
					Truncate(time.Minute)
				for i := cfg.CatchupMinutes; i >= 0; i-- {
					t := end.Add(-time.Duration(i) * time.Minute)
					FetchMinute(ctx, cfg, nc, sinks, t)
				}
			}

			doCycle()
			for {
				select {
				case <-ctx.Done():
					log.Println("Shutting down")
					return nil
				case <-ticker.C:
					doCycle()
				}
			}
		},
	})

	return rootCmd
}

func resolveBackfillSites(ctx context.Context, cfg *Config, nc *Client, override string) ([]string, error) {
	if override != "" {
		parts := strings.Split(override, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts, nil
	}
	if len(cfg.Netnod.Sites) > 0 {
		return cfg.Netnod.Sites, nil
	}
	return nc.SitesConfigured(ctx)
}

func parseMinute(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("--time is required (format: YYYYMMDDHHMM)")
	}
	t, err := time.Parse("200601021504", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing time %q: %w", s, err)
	}
	return t.UTC(), nil
}

func parseHour(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("hour required (YYYYMMDDHH)")
	}
	t, err := time.Parse("2006010215", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing hour %q: %w", s, err)
	}
	return t.UTC(), nil
}
