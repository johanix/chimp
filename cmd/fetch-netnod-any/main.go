package main

import (
	"os"

	"github.com/johanix/dns-anycast-testbed/chimporter/internal/netnod"
)

func main() {
	cmd := netnod.NewRootCmd(netnod.Defaults{
		Use:        "fetch-netnod-any",
		Short:      "Fetch DSC data from the Netnod Anycast API into S3 and/or a local directory",
		ConfigPath: "fetch-netnod-any.yaml",
		BaseURL:    "https://api.netnod.se/dsc/v1",
		Provider:   "netnod-any",
	})
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
