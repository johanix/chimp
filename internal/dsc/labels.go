package dsc

import "strings"

// Labelize converts a raw DSC dimension value to a human-readable label
// when the dimension type has a well-known code→name mapping. Applied at
// parse time so observations store e.g. "AAAA" rather than "28" in key1/
// key2, which makes ClickHouse WHERE clauses trivial (index-friendly) and
// Grafana dashboards readable without query-time multiIf() gymnastics.
//
// Unknown numeric codes fall back to a typed prefix ("TYPE123", "RCODE99")
// so the value is still distinguishable from neighbouring buckets. All
// other dimension types (TLD, ClientAddr, QnameLen, Direction, ...) return
// their value unchanged.
func Labelize(dimType, val string) string {
	switch dimType {
	case "Qtype":
		if s, ok := qtypeLabels[val]; ok {
			return s
		}
		return "TYPE" + val
	case "Rcode":
		if s, ok := rcodeLabels[val]; ok {
			return s
		}
		return "RCODE" + val
	case "Opcode":
		if s, ok := opcodeLabels[val]; ok {
			return s
		}
		return "OPCODE" + val
	case "IPProto":
		// Netnod DSC emits the mnemonic ("udp"/"tcp"); other collectors
		// may emit the numeric proto code. Handle both; if the value is
		// already an alphabetic mnemonic we don't know, pass it through
		// uppercased for readability.
		if s, ok := ipprotoLabels[val]; ok {
			return s
		}
		if len(val) > 0 && (val[0] < '0' || val[0] > '9') {
			return strings.ToUpper(val)
		}
		return "PROTO" + val
	default:
		return val
	}
}

// IANA DNS parameter registries (abridged to the codes commonly seen in
// DSC output; unknown values pass through the "TYPE<n>" fallback).
var qtypeLabels = map[string]string{
	"1":     "A",
	"2":     "NS",
	"5":     "CNAME",
	"6":     "SOA",
	"10":    "NULL",
	"12":    "PTR",
	"13":    "HINFO",
	"15":    "MX",
	"16":    "TXT",
	"17":    "RP",
	"18":    "AFSDB",
	"24":    "SIG",
	"25":    "KEY",
	"28":    "AAAA",
	"29":    "LOC",
	"33":    "SRV",
	"35":    "NAPTR",
	"36":    "KX",
	"37":    "CERT",
	"39":    "DNAME",
	"41":    "OPT",
	"43":    "DS",
	"44":    "SSHFP",
	"45":    "IPSECKEY",
	"46":    "RRSIG",
	"47":    "NSEC",
	"48":    "DNSKEY",
	"49":    "DHCID",
	"50":    "NSEC3",
	"51":    "NSEC3PARAM",
	"52":    "TLSA",
	"53":    "SMIMEA",
	"55":    "HIP",
	"57":    "RKEY",
	"59":    "CDS",
	"60":    "CDNSKEY",
	"61":    "OPENPGPKEY",
	"62":    "CSYNC",
	"63":    "ZONEMD",
	"64":    "SVCB",
	"65":    "HTTPS",
	"99":    "SPF",
	"100":   "UINFO",
	"108":   "EUI48",
	"109":   "EUI64",
	"249":   "TKEY",
	"250":   "TSIG",
	"251":   "IXFR",
	"252":   "AXFR",
	"253":   "MAILB",
	"254":   "MAILA",
	"255":   "ANY",
	"256":   "URI",
	"257":   "CAA",
	"258":   "AVC",
	"259":   "DOA",
	"260":   "AMTRELAY",
	"261":   "RESINFO",
	"262":   "WALLET",
	"263":   "CLA",
	"264":   "IPN",
	"32768": "TA",
	"32769": "DLV",
}

var rcodeLabels = map[string]string{
	"0":  "NOERROR",
	"1":  "FORMERR",
	"2":  "SERVFAIL",
	"3":  "NXDOMAIN",
	"4":  "NOTIMP",
	"5":  "REFUSED",
	"6":  "YXDOMAIN",
	"7":  "YXRRSET",
	"8":  "NXRRSET",
	"9":  "NOTAUTH",
	"10": "NOTZONE",
	"11": "DSOTYPENI",
	"16": "BADVERS",
	"17": "BADKEY",
	"18": "BADTIME",
	"19": "BADMODE",
	"20": "BADNAME",
	"21": "BADALG",
	"22": "BADTRUNC",
	"23": "BADCOOKIE",
}

var opcodeLabels = map[string]string{
	"0": "QUERY",
	"1": "IQUERY",
	"2": "STATUS",
	"3": "RESERVED3",
	"4": "NOTIFY",
	"5": "UPDATE",
	"6": "DSO",
}

var ipprotoLabels = map[string]string{
	"1":   "ICMP",
	"6":   "TCP",
	"17":  "UDP",
	"41":  "IPv6",
	"47":  "GRE",
	"50":  "ESP",
	"58":  "ICMPv6",
	"132": "SCTP",
}
