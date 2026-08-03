// Command gen-docs writes the weave command reference as one markdown file per
// command into the target directory (docs/reference by default). It is the source
// of truth for usage lines and flags: run `mise run gen:docs` after changing any
// command, and `mise run gen:check` fails if the checked-in reference drifts from
// the command tree.
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

// prepare walks the tree turning off cobra's "Auto generated ... on <date>" footer,
// so the output is stable across runs and the drift check only fires on a real
// change, and clearing Hidden, since a command the panel invokes for itself still
// belongs in a reference even when it is kept out of `weave --help`.
func prepare(cmd *cobra.Command) {
	cmd.DisableAutoGenTag = true
	cmd.Hidden = false

	for _, c := range cmd.Commands() {
		prepare(c)
	}
}
