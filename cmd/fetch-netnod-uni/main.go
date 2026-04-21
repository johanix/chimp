package main

import (
	"os"

	"github.com/johanix/dns-anycast-testbed/chimporter/internal/netnod"
)

func main() {
	cmd := netnod.NewRootCmd(netnod.Defaults{
		Use:        "fetch-netnod-uni",
		Short:      "Fetch DSC data from the Netnod Unicast API (v2) into S3 and/or a local directory",
		ConfigPath: "fetch-netnod-uni.yaml",
		BaseURL:    "https://ndsapi.netnod.se/preview/dsc/v2",
		Provider:   "netnod-uni",
	})
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
