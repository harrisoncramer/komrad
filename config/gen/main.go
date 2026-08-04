//go:build ignore

// Command gen writes config.toml from the Config struct in config.
// It runs from that package's directory via go:generate, so the source it parses
// for doc comments is "." and the file it writes is one level up.
package main

import (
	"fmt"
	"os"

	"weave/config"
)

const outputPath = "../config.toml"

func main() {
	rendered, err := config.RenderConfigTOML(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to render config: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(outputPath, rendered, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write %s: %v\n", outputPath, err)
		os.Exit(1)
	}

	fmt.Printf("wrote %s\n", outputPath)
}
