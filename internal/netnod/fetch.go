package netnod

import (
	"context"
	"log"
	"time"

	"github.com/johanix/chimp/internal/s3io"
)

// FetchMinute fetches DSC for every site with data at minute t and writes
// to all configured sinks. Sites already present in every sink are skipped.
func FetchMinute(ctx context.Context, cfg *Config, nc *Client, sinks []Sink, t time.Time) {
	sites := cfg.Netnod.Sites
	if len(sites) == 0 {
		avail, err := nc.SitesAvailable(ctx, t)
		if err != nil {
			log.Printf("SitesAvailable(%s): %v", t.UTC().Format(time.RFC3339), err)
			return
		}
		sites = avail
	}

	for _, site := range sites {
		// Hostname = site for Netnod: the API exposes only site-level data,
		// not individual servers behind a site.
		relKey := s3io.BuildKey("", cfg.Provider, site, site, cfg.Provider, "dsc.xml.gz", t)

		missing := sinksMissingKey(ctx, sinks, relKey)
		if len(missing) == 0 {
			continue
		}

		r, err := nc.FetchDSC(ctx, site, t)
		if err != nil {
			log.Printf("FetchDSC %s %s: %v", site, t.UTC().Format(time.RFC3339), err)
			continue
		}
		if r.NoData {
			// 204: data may arrive later; outer loop's catch-up window re-checks.
			continue
		}

		for _, s := range missing {
			if err := s.Put(ctx, relKey, r.Data, r.ContentType); err != nil {
				log.Printf("Put %s: %v", s.Locator(relKey), err)
				continue
			}
			log.Printf("Wrote %s (%d bytes)", s.Locator(relKey), len(r.Data))
		}
	}
}

// FetchHour fetches an hour's worth of DSC data per site via the hourly
// API endpoint and writes the raw gzipped XML blob to all configured sinks
// under a "dsc-hour.xml.gz" filename keyed at minute=00/second=00.
func FetchHour(ctx context.Context, cfg *Config, nc *Client, sinks []Sink, sites []string, hour time.Time) {
	for _, site := range sites {
		relKey := s3io.BuildKey("", cfg.Provider, site, site, cfg.Provider, "dsc-hour.xml.gz", hour)

		missing := sinksMissingKey(ctx, sinks, relKey)
		if len(missing) == 0 {
			continue
		}

		r, err := nc.FetchDSCHour(ctx, site, hour)
		if err != nil {
			log.Printf("FetchDSCHour %s %s: %v", site, hour.UTC().Format("2006-01-02T15"), err)
			continue
		}
		if r.NoData {
			continue
		}

		for _, s := range missing {
			if err := s.Put(ctx, relKey, r.Data, r.ContentType); err != nil {
				log.Printf("Put %s: %v", s.Locator(relKey), err)
				continue
			}
			log.Printf("Wrote %s (%d bytes)", s.Locator(relKey), len(r.Data))
		}
	}
}

// sinksMissingKey returns the subset of sinks that don't yet have the key.
// A sink whose Exists check errors is treated as missing (best-effort write).
func sinksMissingKey(ctx context.Context, sinks []Sink, relKey string) []Sink {
	var missing []Sink
	for _, s := range sinks {
		ok, err := s.Exists(ctx, relKey)
		if err != nil {
			log.Printf("Exists %s: %v (will attempt write)", s.Locator(relKey), err)
			missing = append(missing, s)
			continue
		}
		if !ok {
			missing = append(missing, s)
		}
	}
	return missing
}
