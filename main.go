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

	// A command that already wrote its failure to the log says so. Printing it here too
	// would put the same failure in the same file twice, once with its fields and once as
	// a bare sentence.
	if !errors.Is(err, cli.ErrLogged) {
		fmt.Fprintln(os.Stderr, err)
	}

	os.Exit(1)
}
