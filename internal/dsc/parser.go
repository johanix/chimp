// Package dsc provides DSC data types and parsers.
package dsc

import (
	"encoding/json"
	"fmt"
	"time"
)

// Observation is a single row destined for the observations table.
type Observation struct {
	Timestamp time.Time
	Provider  string
	Site      string
	Hostname  string
	Dataset   string
	Key1      string
	Key2      string
	Value     uint64
}

// Dataset represents a single dataset in the DSC JSON output.
// The file is a JSON array of these.
type Dataset struct {
	Name       string          `json:"name"`
	StartTime  int64           `json:"start_time"`
	StopTime   int64           `json:"stop_time"`
	Dimensions []string        `json:"dimensions"`
	Data       json.RawMessage `json:"data"`
}

// CountPair is a {val, count} entry in the inner dimension.
type CountPair struct {
	Val   string `json:"val"`
	Count uint64 `json:"count"`
}

// ParseJSON parses a DSC JSON file into observations.
//
// DSC JSON is an array of datasets:
//
//	[
//	  {
//	    "name": "qtype",
//	    "start_time": 1774377600,
//	    "stop_time": 1774377900,
//	    "dimensions": ["All", "Qtype"],
//	    "data": [
//	      {"All": "ALL", "Qtype": [{"val": "6", "count": 118}]}
//	    ]
//	  }
//	]
func ParseJSON(data []byte, provider, site, hostname string) ([]Observation, error) {
	var datasets []Dataset
	if err := json.Unmarshal(data, &datasets); err != nil {
		return nil, fmt.Errorf("unmarshaling DSC JSON: %w", err)
	}

	var obs []Observation
	for _, ds := range datasets {
		parsed, err := parseDataset(ds, provider, site, hostname)
		if err != nil {
			return nil, fmt.Errorf("parsing dataset %s: %w", ds.Name, err)
		}
		obs = append(obs, parsed...)
	}
	return obs, nil
}

func parseDataset(ds Dataset, provider, site, hostname string) ([]Observation, error) {
	ts := time.Unix(ds.StartTime, 0).UTC()

	if len(ds.Dimensions) < 1 {
		return nil, fmt.Errorf("dataset %s has no dimensions", ds.Name)
	}

	var entries []json.RawMessage
	if err := json.Unmarshal(ds.Data, &entries); err != nil {
		return nil, fmt.Errorf("parsing data array: %w", err)
	}
	if len(entries) == 0 {
		return nil, nil
	}

	dim1Name := ds.Dimensions[0]
	var obs []Observation

	if len(ds.Dimensions) == 2 {
		dim2Name := ds.Dimensions[1]
		for _, entry := range entries {
			parsed, err := parse2DEntry(entry, dim1Name, dim2Name, ts, provider, site, hostname, ds.Name)
			if err != nil {
				return nil, err
			}
			obs = append(obs, parsed...)
		}
	} else if len(ds.Dimensions) == 1 {
		for _, entry := range entries {
			parsed, err := parse1DEntry(entry, dim1Name, ts, provider, site, hostname, ds.Name)
			if err != nil {
				return nil, err
			}
			obs = append(obs, parsed...)
		}
	} else {
		return nil, fmt.Errorf("dataset %s has %d dimensions (expected 1 or 2)",
			ds.Name, len(ds.Dimensions))
	}

	return obs, nil
}

func parse2DEntry(entry json.RawMessage, dim1Name, dim2Name string, ts time.Time, provider, site, hostname, dataset string) ([]Observation, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(entry, &raw); err != nil {
		return nil, fmt.Errorf("parsing data entry: %w", err)
	}

	var key1 string
	if v, ok := raw[dim1Name]; ok {
		if err := json.Unmarshal(v, &key1); err != nil {
			return nil, fmt.Errorf("parsing dimension %s: %w", dim1Name, err)
		}
	}

	var pairs []CountPair
	if v, ok := raw[dim2Name]; ok {
		if err := json.Unmarshal(v, &pairs); err != nil {
			return nil, fmt.Errorf("parsing dimension %s: %w", dim2Name, err)
		}
	}

	var obs []Observation
	for _, p := range pairs {
		obs = append(obs, Observation{
			Timestamp: ts,
			Provider:  provider,
			Site:      site,
			Hostname:  hostname,
			Dataset:   dataset,
			Key1:      key1,
			Key2:      p.Val,
			Value:     p.Count,
		})
	}
	return obs, nil
}

func parse1DEntry(entry json.RawMessage, dimName string, ts time.Time, provider, site, hostname, dataset string) ([]Observation, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(entry, &raw); err != nil {
		return nil, fmt.Errorf("parsing data entry: %w", err)
	}

	var pairs []CountPair
	if v, ok := raw[dimName]; ok {
		if err := json.Unmarshal(v, &pairs); err != nil {
			return nil, fmt.Errorf("parsing dimension %s: %w", dimName, err)
		}
	}

	var obs []Observation
	for _, p := range pairs {
		obs = append(obs, Observation{
			Timestamp: ts,
			Provider:  provider,
			Site:      site,
			Hostname:  hostname,
			Dataset:   dataset,
			Key1:      p.Val,
			Key2:      "",
			Value:     p.Count,
		})
	}
	return obs, nil
}
