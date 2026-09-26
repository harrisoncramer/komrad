package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"komrad/internal/cli"
	"komrad/internal/logging"
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

	var exited *cli.ExitError
	if errors.As(err, &exited) {
		return exited.Code
	}

	if !cli.IsQuiet(err) {
		fmt.Fprintln(os.Stderr, err)
	}

	return 1
}
