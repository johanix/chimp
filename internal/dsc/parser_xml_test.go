package dsc

import (
	"os"
	"testing"
)

func TestParseXML_SampleFile(t *testing.T) {
	data, err := os.ReadFile("testdata/sample.xml")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	obs, err := ParseXML(data, "netnod-any", "STH", "STH")
	if err != nil {
		t.Fatalf("ParseXML: %v", err)
	}
	if len(obs) == 0 {
		t.Fatalf("expected non-empty observations")
	}

	datasets := map[string]int{}
	for _, o := range obs {
		datasets[o.Dataset]++
		if o.Provider != "netnod-any" {
			t.Errorf("provider mismatch: %s", o.Provider)
		}
		if o.Site != "STH" {
			t.Errorf("site mismatch: %s", o.Site)
		}
		if o.Hostname != "STH" {
			t.Errorf("hostname mismatch: %s", o.Hostname)
		}
		if o.Timestamp.Unix() != 1776016800 {
			t.Errorf("timestamp mismatch: %d", o.Timestamp.Unix())
		}
	}

	// Sample has 25 distinct arrays; each should contribute at least one observation.
	if got, want := len(datasets), 25; got != want {
		t.Errorf("expected %d datasets, got %d: %v", want, got, datasets)
	}

	// qtype: outer All=ALL, 17 inner Qtype entries → 17 obs.
	if datasets["qtype"] != 17 {
		t.Errorf("qtype: expected 17 obs, got %d", datasets["qtype"])
	}

	// opcode: outer All=ALL, 1 inner Opcode entry (val=0, count=11456).
	if datasets["opcode"] != 1 {
		t.Errorf("opcode: expected 1 obs, got %d", datasets["opcode"])
	}

	// Spot-check a known (key1, key2, value) triple. Opcode 0 labelizes to QUERY.
	var found bool
	for _, o := range obs {
		if o.Dataset == "opcode" && o.Key1 == "ALL" && o.Key2 == "QUERY" {
			if o.Value != 11456 {
				t.Errorf("opcode ALL/QUERY: expected value 11456, got %d", o.Value)
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("did not find opcode ALL/QUERY observation")
	}

	// client_addr_vs_rcode has a real 2D cross-tab: multiple outer Rcode values.
	// Labelize converts 0→NOERROR, 3→NXDOMAIN.
	var noerrorCount, nxdomainCount int
	for _, o := range obs {
		if o.Dataset != "client_addr_vs_rcode" {
			continue
		}
		switch o.Key1 {
		case "NOERROR":
			noerrorCount++
		case "NXDOMAIN":
			nxdomainCount++
		}
	}
	if noerrorCount == 0 || nxdomainCount == 0 {
		t.Errorf("client_addr_vs_rcode: expected entries for NOERROR and NXDOMAIN, got %d and %d",
			noerrorCount, nxdomainCount)
	}
}

func TestLabelize(t *testing.T) {
	cases := []struct {
		dimType, in, want string
	}{
		{"Qtype", "1", "A"},
		{"Qtype", "28", "AAAA"},
		{"Qtype", "99999", "TYPE99999"},
		{"Rcode", "0", "NOERROR"},
		{"Rcode", "3", "NXDOMAIN"},
		{"Rcode", "42", "RCODE42"},
		{"Opcode", "0", "QUERY"},
		{"Opcode", "5", "UPDATE"},
		{"IPProto", "17", "UDP"},
		{"IPProto", "6", "TCP"},
		{"IPProto", "udp", "UDP"},
		{"IPProto", "tcp", "TCP"},
		{"All", "ALL", "ALL"},
		{"TLD", "se.", "se."},
		{"", "foo", "foo"},
	}
	for _, c := range cases {
		if got := Labelize(c.dimType, c.in); got != c.want {
			t.Errorf("Labelize(%q, %q) = %q; want %q", c.dimType, c.in, got, c.want)
		}
	}
}

// TestParseXML_CapitalRootElement covers the Netnod v2 API, which emits
// <Dscdata> (capital D) instead of the v1 <dscdata>. Parser must accept both.
func TestParseXML_CapitalRootElement(t *testing.T) {
	const v2 = `<Dscdata>
<array name="opcode" dimensions="2" start_time="1776016800" stop_time="1776016860">
  <dimension number="1" type="All"/>
  <dimension number="2" type="Opcode"/>
  <data>
    <All val="ALL" count="42">
      <Opcode val="0" count="42"/>
    </All>
  </data>
</array>
</Dscdata>`
	obs, err := ParseXML([]byte(v2), "netnod-uni", "STO", "STO")
	if err != nil {
		t.Fatalf("ParseXML: %v", err)
	}
	if len(obs) != 1 {
		t.Fatalf("expected 1 observation, got %d", len(obs))
	}
	o := obs[0]
	if o.Dataset != "opcode" || o.Key1 != "ALL" || o.Key2 != "QUERY" || o.Value != 42 {
		t.Errorf("unexpected observation: %+v", o)
	}
}

func TestParseXMLGz_SampleFile(t *testing.T) {
	data, err := os.ReadFile("testdata/sample.xml.gz")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	obs, err := ParseXMLGz(data, "netnod-any", "STH", "STH")
	if err != nil {
		t.Fatalf("ParseXMLGz: %v", err)
	}
	if len(obs) == 0 {
		t.Fatalf("expected non-empty observations")
	}
}
