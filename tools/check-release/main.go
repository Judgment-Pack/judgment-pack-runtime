// Command check-release enforces release-only tag and artifact provenance rules.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/releasecheck"
)

func main() {
	tag := flag.String("tag", "", "exact CLI release tag")
	flag.Parse()
	if *tag == "" {
		fmt.Fprintln(os.Stderr, "release check failed: --tag is required")
		os.Exit(1)
	}
	if err := releasecheck.CheckEmbedded(*tag); err != nil {
		fmt.Fprintln(os.Stderr, "release check failed:", err)
		os.Exit(1)
	}
	fmt.Printf("Release inputs verified for %s.\n", *tag)
}
