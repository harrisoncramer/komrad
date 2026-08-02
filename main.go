// Command weave is the tmux panel and CLI for background Claude agents.
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
	// The root every command's own goroutine runs under. serve makes the service's logger the
	// default one before anything else, so a panic here is a record in the log file like
	// everything else the service says, and in any other command it is the line on stderr it
	// always was.
	defer logging.LogAndRepanic(context.Background())

	err := cli.NewRootCommand().Execute()
	if err == nil {
		return
	}

	// Both sentinels mean the failure has already been put in front of somebody, one in
	// the service log and one on the command's own output.
	if !errors.Is(err, cli.ErrLogged) && !errors.Is(err, cli.ErrReported) {
		fmt.Fprintln(os.Stderr, err)
	}

	os.Exit(1)
}
