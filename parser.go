package main

import (
	"encoding/json"
	"fmt"
	"time"
)

// Observation is a single row destined for the observations table.
type Observation struct {
	Timestamp time.Time
	Hostname  string
	Dataset   string
	Key1      string
	Key2      string
	Value     uint64
}

// DSCDataset represents a single dataset in the DSC JSON output.
// The file is a JSON array of these.
type DSCDataset struct {
	Name       string          `json:"name"`
	StartTime  int64           `json:"start_time"`
	StopTime   int64           `json:"stop_time"`
	Dimensions []string        `json:"dimensions"`
	Data       json.RawMessage `json:"data"`
}

// DSCCountPair is a {val, count} entry in the inner dimension.
type DSCCountPair struct {
	Val   string `json:"val"`
	Count uint64 `json:"count"`
}

// ParseDSCJSON parses a DSC JSON file into observations.
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
//	      {
//	        "All": "ALL",
//	        "Qtype": [{"val": "6", "count": 118}]
//	      }
//	    ]
//	  }
//	]
func ParseDSCJSON(data []byte, hostname string) ([]Observation, error) {
	var datasets []DSCDataset
	if err := json.Unmarshal(data, &datasets); err != nil {
		return nil, fmt.Errorf("unmarshaling DSC JSON: %w", err)
	}

	var obs []Observation
	for _, ds := range datasets {
		parsed, err := parseDataset(ds, hostname)
		if err != nil {
			return nil, fmt.Errorf("parsing dataset %s: %w", ds.Name, err)
		}
		obs = append(obs, parsed...)
	}
	return obs, nil
}

// parseDataset handles a single DSC dataset.
//
// Each data entry is a JSON object whose keys match the dimension names.
// The first dimension is typically a grouping key (string value),
// and the last dimension is an array of {val, count} pairs.
//
// For 2D datasets like:
//
//	{"All": "ALL", "Qtype": [{"val": "6", "count": 118}]}
//
// key1 = the first dimension's value ("ALL")
// key2 = the val from the count pairs ("6")
//
// For datasets with empty data arrays (like pcap_stats), we
// produce no observations.
func parseDataset(ds DSCDataset, hostname string) ([]Observation, error) {
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
			parsed, err := parse2DEntry(entry, dim1Name, dim2Name, ts, hostname, ds.Name)
			if err != nil {
				return nil, err
			}
			obs = append(obs, parsed...)
		}
	} else if len(ds.Dimensions) == 1 {
		for _, entry := range entries {
			parsed, err := parse1DEntry(entry, dim1Name, ts, hostname, ds.Name)
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

// parse2DEntry parses an entry like:
//
//	{"All": "ALL", "Qtype": [{"val": "6", "count": 118}]}
func parse2DEntry(entry json.RawMessage, dim1Name, dim2Name string, ts time.Time, hostname, dataset string) ([]Observation, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(entry, &raw); err != nil {
		return nil, fmt.Errorf("parsing data entry: %w", err)
	}

	// First dimension: string value
	var key1 string
	if v, ok := raw[dim1Name]; ok {
		if err := json.Unmarshal(v, &key1); err != nil {
			return nil, fmt.Errorf("parsing dimension %s: %w", dim1Name, err)
		}
	}

	// Second dimension: array of {val, count}
	var pairs []DSCCountPair
	if v, ok := raw[dim2Name]; ok {
		if err := json.Unmarshal(v, &pairs); err != nil {
			return nil, fmt.Errorf("parsing dimension %s: %w", dim2Name, err)
		}
	}

	var obs []Observation
	for _, p := range pairs {
		obs = append(obs, Observation{
			Timestamp: ts,
			Hostname:  hostname,
			Dataset:   dataset,
			Key1:      key1,
			Key2:      p.Val,
			Value:     p.Count,
		})
	}
	return obs, nil
}

// parse1DEntry parses a 1D entry with a single dimension of {val, count} pairs.
func parse1DEntry(entry json.RawMessage, dimName string, ts time.Time, hostname, dataset string) ([]Observation, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(entry, &raw); err != nil {
		return nil, fmt.Errorf("parsing data entry: %w", err)
	}

	var pairs []DSCCountPair
	if v, ok := raw[dimName]; ok {
		if err := json.Unmarshal(v, &pairs); err != nil {
			return nil, fmt.Errorf("parsing dimension %s: %w", dimName, err)
		}
	}

	var obs []Observation
	for _, p := range pairs {
		obs = append(obs, Observation{
			Timestamp: ts,
			Hostname:  hostname,
			Dataset:   dataset,
			Key1:      p.Val,
			Key2:      "",
			Value:     p.Count,
		})
	}
	return obs, nil
}
