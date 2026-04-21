package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/johanix/dns-anycast-testbed/chimporter/internal/chdb"
)

// siteEntry mirrors one entry under `sites:` in sites.yaml.
type siteEntry struct {
	Provider   string   `yaml:"provider"`
	Site       string   `yaml:"site"`
	Service    string   `yaml:"service"`
	City       string   `yaml:"city"`
	Country    string   `yaml:"country"`
	CountryISO string   `yaml:"country_iso"`
	Tags       []string `yaml:"tags"`
	Lat        float64  `yaml:"lat"`
	Lon        float64  `yaml:"lon"`
	Notes      string   `yaml:"notes"`
}

type sitesFile struct {
	Version int         `yaml:"version"`
	Sites   []siteEntry `yaml:"sites"`
}

// LoadSitesYAML parses sites.yaml into a list of chdb.Site.
func LoadSitesYAML(path string) ([]chdb.Site, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var sf sitesFile
	if err := yaml.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	seen := make(map[string]bool, len(sf.Sites))
	out := make([]chdb.Site, 0, len(sf.Sites))
	for i, e := range sf.Sites {
		if e.Provider == "" || e.Site == "" {
			return nil, fmt.Errorf("entry %d: provider and site are required", i)
		}
		if e.Service != "anycast" && e.Service != "unicast" {
			return nil, fmt.Errorf("entry %d (%s/%s): service must be %q or %q, got %q",
				i, e.Provider, e.Site, "anycast", "unicast", e.Service)
		}
		key := e.Provider + "/" + e.Site
		if seen[key] {
			return nil, fmt.Errorf("duplicate (provider, site) entry: %s", key)
		}
		seen[key] = true
		out = append(out, chdb.Site{
			Provider:   e.Provider,
			Site:       e.Site,
			Service:    e.Service,
			City:       e.City,
			Country:    e.Country,
			CountryISO: e.CountryISO,
			Tags:       e.Tags,
			Lat:        e.Lat,
			Lon:        e.Lon,
			Notes:      e.Notes,
		})
	}
	return out, nil
}
