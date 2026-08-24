package main

import (
	"fmt"
	"os"

	"weave/internal/cli"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

func main() {
	dir := "docs/reference"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	if err := run(dir); err != nil {
		fmt.Fprintln(os.Stderr, "gen-docs:", err)
		os.Exit(1)
	}
}

func run(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	root := cli.NewRootCommand()
	prepare(root)

	if err := doc.GenMarkdownTree(root, dir); err != nil {
		return fmt.Errorf("generating markdown: %w", err)
	}

	return nil
}

func prepare(cmd *cobra.Command) {
	cmd.DisableAutoGenTag = true
	cmd.Hidden = false

	for _, c := range cmd.Commands() {
		prepare(c)
	}
}
