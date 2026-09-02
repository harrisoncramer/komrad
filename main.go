package main

import (
	"context"
	"fmt"
	"os"

	"weave/internal/cli"
	"weave/internal/logging"
)

func main() {
	os.Exit(run())
}

// run holds the deferred panic logger, which an os.Exit in the same frame would skip.
func run() int {
	defer logging.LogAndRepanic(context.Background())

	err := cli.NewRootCommand().Execute()
	if err == nil {
		return 0
	}

	if !cli.Quiet(err) {
		fmt.Fprintln(os.Stderr, err)
	}

	return 1
}
