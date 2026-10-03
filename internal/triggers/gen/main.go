//go:build ignore

package main

import (
	"fmt"
	"os"

	"komrad/internal/triggers"
)

func main() {
	if err := triggers.Sync("db/triggers"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
