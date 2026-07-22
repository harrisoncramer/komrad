// Command gen-docs writes the claude-agents command reference as one markdown
// file per command into the target directory (docs/reference by default). It is
// the source of truth for command usage and flags: run `mise run gen-docs` after
// changing any command so the checked-in reference stays in sync with the tree.
//
// It lives in its own module (claude-agents/docs) so cobra's markdown/doc
// dependencies never enter the root binary's module graph.
package main

import (
	"fmt"
	"os"

	"claude-agents/internal/cli"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

func main() {
	dir := "reference"
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
	disableAutoGenTag(root)
	if err := doc.GenMarkdownTree(root, dir); err != nil {
		return fmt.Errorf("generating markdown: %w", err)
	}
	return nil
}

// disableAutoGenTag turns off cobra's "Auto generated ... on <date>" footer for
// every command, so the output is stable across runs and only real changes show
// up in a diff.
func disableAutoGenTag(cmd *cobra.Command) {
	cmd.DisableAutoGenTag = true
	for _, c := range cmd.Commands() {
		disableAutoGenTag(c)
	}
}
