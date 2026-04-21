package dsc

import (
	"testing"
)

var sampleDSC = []byte(`[
{
  "name": "pcap_stats",
  "start_time": 1774377600,
  "stop_time": 1774377900,
  "dimensions": [ "ifname", "pcap_stat" ],
  "data": [
  ]
},
{
  "name": "client_subnet",
  "start_time": 1774377600,
  "stop_time": 1774377900,
  "dimensions": [ "All", "ClientSubnet" ],
  "data": [
    {
      "All": "ALL",
      "ClientSubnet": [
        { "val": "194.103.53.0", "count": 59 },
        { "val": "2a10:ba00:53::", "count": 59 }
      ]
    }
  ]
},
{
  "name": "opcode",
  "start_time": 1774377600,
  "stop_time": 1774377900,
  "dimensions": [ "All", "Opcode" ],
  "data": [
    {
      "All": "ALL",
      "Opcode": [
        { "val": "0", "count": 118 }
      ]
    }
  ]
},
{
  "name": "rcode",
  "start_time": 1774377600,
  "stop_time": 1774377900,
  "dimensions": [ "All", "Rcode" ],
  "data": [
    {
      "All": "ALL",
      "Rcode": [
        { "val": "0", "count": 118 }
      ]
    }
  ]
},
{
  "name": "qtype",
  "start_time": 1774377600,
  "stop_time": 1774377900,
  "dimensions": [ "All", "Qtype" ],
  "data": [
    {
      "All": "ALL",
      "Qtype": [
        { "val": "6", "count": 118 }
      ]
    }
  ]
}
]`)

func TestParseJSON(t *testing.T) {
	obs, err := ParseJSON(sampleDSC, "test-provider", "test-site", "testhost")
	if err != nil {
		t.Fatalf("ParseJSON failed: %v", err)
	}

	// pcap_stats has empty data, so 0 observations from that.
	// client_subnet: 2 entries, opcode: 1, rcode: 1, qtype: 1 → 5 total.
	if len(obs) != 5 {
		t.Fatalf("expected 5 observations, got %d", len(obs))
	}

	for _, o := range obs {
		if o.Provider != "test-provider" {
			t.Errorf("expected provider test-provider, got %s", o.Provider)
		}
		if o.Site != "test-site" {
			t.Errorf("expected site test-site, got %s", o.Site)
		}
		if o.Hostname != "testhost" {
			t.Errorf("expected hostname testhost, got %s", o.Hostname)
		}
		if o.Timestamp.Unix() != 1774377600 {
			t.Errorf("expected timestamp 1774377600, got %d", o.Timestamp.Unix())
		}
	}

	datasets := map[string]int{}
	for _, o := range obs {
		datasets[o.Dataset]++
	}
	if datasets["client_subnet"] != 2 {
		t.Errorf("expected 2 client_subnet obs, got %d", datasets["client_subnet"])
	}
	if datasets["qtype"] != 1 {
		t.Errorf("expected 1 qtype obs, got %d", datasets["qtype"])
	}

	for _, o := range obs {
		if o.Dataset == "client_subnet" && o.Key2 == "194.103.53.0" {
			if o.Key1 != "ALL" {
				t.Errorf("expected key1=ALL, got %s", o.Key1)
			}
			if o.Value != 59 {
				t.Errorf("expected value=59, got %d", o.Value)
			}
			return
		}
	}
	t.Error("did not find expected client_subnet observation for 194.103.53.0")
}
