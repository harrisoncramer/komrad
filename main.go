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

	if !errors.Is(err, cli.ErrLogged) {
		fmt.Fprintln(os.Stderr, err)
	}

	os.Exit(1)
}
