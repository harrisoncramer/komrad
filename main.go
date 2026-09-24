package main

import (
	"context"
	"fmt"
	"os"

	"komrade/internal/cli"
	"komrade/internal/logging"
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

	if !cli.IsQuiet(err) {
		fmt.Fprintln(os.Stderr, err)
	}

	return 1
}
