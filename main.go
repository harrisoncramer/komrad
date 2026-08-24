package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"weave/internal/cli"
	"weave/internal/logging"
)

func main() {
	defer logging.LogAndRepanic(context.Background())

	err := cli.NewRootCommand().Execute()
	if err == nil {
		return
	}

	if !errors.Is(err, cli.ErrLogged) && !errors.Is(err, cli.ErrReported) {
		fmt.Fprintln(os.Stderr, err)
	}

	os.Exit(1)
}
