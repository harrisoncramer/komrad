// Command weave is the tmux panel and CLI for background Claude agents.
package main

import (
	"errors"
	"fmt"
	"os"

	"weave/internal/cli"
)

func main() {
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
