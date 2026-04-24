package dsc

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"fmt"
	"io"
	"time"
)

// xmlDSCData is the root element of the DSC XML response. We don't pin
// the root name: Netnod v1 emits <dscdata> (lowercase) while v2 emits
// <Dscdata> (capital D). XML element matching is case-sensitive, so we
// omit the XMLName tag and accept whatever root wraps the <array> children.
type xmlDSCData struct {
	Arrays []xmlArray `xml:"array"`
}

type xmlArray struct {
	Name       string         `xml:"name,attr"`
	Dimensions int            `xml:"dimensions,attr"`
	StartTime  int64          `xml:"start_time,attr"`
	StopTime   int64          `xml:"stop_time,attr"`
	DimDecls   []xmlDimension `xml:"dimension"`
	Data       xmlData        `xml:"data"`
}

type xmlDimension struct {
	Number int    `xml:"number,attr"`
	Type   string `xml:"type,attr"`
}

// xmlData holds the raw children of the <data> element. We parse them
// generically because the element names match the dimension types.
type xmlData struct {
	Inner []byte `xml:",innerxml"`
}

// xmlOuter represents a top-level data entry in a 2D array.
// The element's tag name equals the outer dimension's type attribute,
// e.g. <All val="ALL" count="N"> or <Rcode val="0" count="N">.
type xmlOuter struct {
	Val   string     `xml:"val,attr"`
	Count uint64     `xml:"count,attr"`
	Inner []xmlInner `xml:",any"`
}

// xmlInner is a leaf entry inside an outer element.
// The tag name equals the inner dimension's type attribute.
type xmlInner struct {
	Val   string `xml:"val,attr"`
	Count uint64 `xml:"count,attr"`
}

// ParseXMLGz gunzips data and parses it as DSC XML.
func ParseXMLGz(data []byte, provider, site, hostname string) ([]Observation, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gunzip: %w", err)
	}
	defer gr.Close()
	raw, err := io.ReadAll(gr)
	if err != nil {
		return nil, fmt.Errorf("reading gzip stream: %w", err)
	}
	return ParseXML(raw, provider, site, hostname)
}

// ParseXML parses uncompressed DSC XML into observations.
//
// The XML shape is:
//
//	<dscdata>
//	  <array name="DATASET" dimensions="2" start_time="T" stop_time="T+60">
//	    <dimension number="1" type="OUTER"/>
//	    <dimension number="2" type="INNER"/>
//	    <data>
//	      <OUTER val="X" count="N">
//	        <INNER val="Y" count="M"/>
//	        ...
//	      </OUTER>
//	      ...
//	    </data>
//	  </array>
//	  ...
//	</dscdata>
//
// One observation per inner leaf: key1=outer.val, key2=inner.val, value=inner.count.
// Outer aggregate counts are ignored (recoverable via SUM in SQL).
func ParseXML(data []byte, provider, site, hostname string) ([]Observation, error) {
	var root xmlDSCData
	if err := xml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("unmarshaling DSC XML: %w", err)
	}

	var obs []Observation
	for _, arr := range root.Arrays {
		parsed, err := parseXMLArray(arr, provider, site, hostname)
		if err != nil {
			return nil, fmt.Errorf("parsing array %s: %w", arr.Name, err)
		}
		obs = append(obs, parsed...)
	}
	return obs, nil
}

func parseXMLArray(arr xmlArray, provider, site, hostname string) ([]Observation, error) {
	ts := time.Unix(arr.StartTime, 0).UTC()

	// Pick out the dimension types so we can labelize well-known numeric
	// codes (Qtype/Rcode/Opcode/IPProto) at parse time.
	var dim1Type, dim2Type string
	for _, d := range arr.DimDecls {
		switch d.Number {
		case 1:
			dim1Type = d.Type
		case 2:
			dim2Type = d.Type
		}
	}

	// Re-parse <data>'s inner XML as a stream of outer elements. Using
	// innerxml + a fresh decoder keeps us agnostic to the outer element's
	// tag name (which varies by array per its `type` attribute).
	dec := xml.NewDecoder(bytes.NewReader(arr.Data.Inner))
	var obs []Observation

	switch arr.Dimensions {
	case 2:
		for {
			tok, err := dec.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("tokenizing data: %w", err)
			}
			start, ok := tok.(xml.StartElement)
			if !ok {
				continue
			}
			var outer xmlOuter
			if err := dec.DecodeElement(&outer, &start); err != nil {
				return nil, fmt.Errorf("decoding outer %s: %w", start.Name.Local, err)
			}
			for _, inner := range outer.Inner {
				obs = append(obs, Observation{
					Timestamp: ts,
					Provider:  provider,
					Site:      site,
					Hostname:  hostname,
					Dataset:   arr.Name,
					Key1:      Labelize(dim1Type, outer.Val),
					Key2:      Labelize(dim2Type, inner.Val),
					Value:     inner.Count,
				})
			}
		}

	case 1:
		// 1D arrays: flat list of leaf elements, no outer grouping.
		// Not observed in current Netnod output, but handled for completeness.
		for {
			tok, err := dec.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("tokenizing data: %w", err)
			}
			start, ok := tok.(xml.StartElement)
			if !ok {
				continue
			}
			var leaf xmlInner
			if err := dec.DecodeElement(&leaf, &start); err != nil {
				return nil, fmt.Errorf("decoding leaf %s: %w", start.Name.Local, err)
			}
			obs = append(obs, Observation{
				Timestamp: ts,
				Provider:  provider,
				Site:      site,
				Hostname:  hostname,
				Dataset:   arr.Name,
				Key1:      Labelize(dim1Type, leaf.Val),
				Key2:      "",
				Value:     leaf.Count,
			})
		}

	default:
		return nil, fmt.Errorf("array %s has dimensions=%d (expected 1 or 2)",
			arr.Name, arr.Dimensions)
	}

	return obs, nil
}
