// Command claude-agents is the tmux panel and CLI for background Claude agents.
package main

import (
	"fmt"
	"os"

	"claude-agents/internal/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
